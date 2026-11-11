package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/requeststate"
)

type TokenCommand struct {
	ID       string              `json:"id,omitempty"`
	Input    apiauth.Input       `json:"input"`
	Raw      string              `json:"raw,omitempty"`
	SourceIP string              `json:"source_ip,omitempty"`
	Write    *requeststate.Write `json:"write,omitempty"`
}

type createdToken struct {
	Token apiauth.Token `json:"token"`
	Value string        `json:"value"`
}

func (n *Node) tokenDispatch(ctx context.Context, method string, body json.RawMessage) (any, error) {
	var command TokenCommand
	if e := json.Unmarshal(body, &command); e != nil {
		return nil, fault("invalid_request", "令牌请求无效")
	}
	store := apiauth.Store{DB: n.DB, Owner: n.ID, Now: n.Now}
	var tx *sql.Tx
	if command.Write != nil && command.Write.Key != "" {
		var e error
		tx, e = n.DB.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		store.DB = tx
		prior, e := requeststate.Lookup(ctx, tx, *command.Write)
		if e == nil {
			if prior.Secret {
				return nil, fault("secret_unavailable", "原请求已完成，秘密值仅在首次生成时展示")
			}
			var value any
			_ = json.Unmarshal(prior.Result, &value)
			return value, nil
		}
		if errors.Is(e, requeststate.ErrConflict) {
			return nil, fault("conflict", e.Error())
		}
		if e != sql.ErrNoRows {
			return nil, e
		}
	}
	var result any
	var e error
	switch method {
	case "api_token.verify":
		result, e = store.Verify(ctx, command.Raw, command.SourceIP)
	case "api_token.list":
		result, e = store.List(ctx)
	case "api_token.get":
		result, e = store.Get(ctx, command.ID)
	case "api_token.create", "api_token.regenerate":
		var token apiauth.Token
		var raw string
		if method == "api_token.create" {
			token, raw, e = store.Create(ctx, command.Input)
		} else {
			token, raw, e = store.Regenerate(ctx, command.ID, command.Input.Revision)
		}
		result = createdToken{token, raw}
	case "api_token.update":
		result, e = store.Update(ctx, command.ID, command.Input)
	case "api_token.delete":
		e = store.Delete(ctx, command.ID, command.Input.Revision)
	default:
		e = fault("invalid_request", "未知令牌操作")
	}
	if errors.Is(e, apiauth.ErrUnauthorized) {
		e = fault("unauthenticated", e.Error())
	}
	if errors.Is(e, apiauth.ErrInvalid) {
		e = fault("invalid_request", e.Error())
	}
	if errors.Is(e, apiauth.ErrConflict) {
		e = fault("precondition_failed", e.Error())
	}
	if e == nil && tx != nil {
		resource, secret := command.ID, false
		value := result
		if created, ok := result.(createdToken); ok {
			resource, secret, value = created.Token.ID, true, created.Token
		}
		data, _ := json.Marshal(value)
		e = requeststate.Save(ctx, tx, requeststate.Receipt{Write: *command.Write, Kind: "api-token", ResourceID: resource, Result: data, Secret: secret, CreatedAt: n.Now().UTC()})
		if e == nil {
			e = tx.Commit()
		}
	}
	return result, e
}
func (n *Node) VerifyAPIToken(ctx context.Context, raw, sourceIP string) (apiauth.Token, error) {
	var t apiauth.Token
	owner := apiauth.Owner(raw)
	if owner == "" || !n.Active(ctx, owner) {
		return t, fault("unauthenticated", "令牌无效")
	}
	e := n.Call(ctx, owner, "api_token.verify", TokenCommand{Raw: raw, SourceIP: sourceIP}, &t)
	return t, e
}
