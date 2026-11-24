package cluster

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type SharedConsoles struct {
	Revision uint64 `json:"revision"`
	Primary  string `json:"primary"`
	Backup   string `json:"backup"`
	Cached   bool   `json:"cached,omitempty"`
}

func (n *Node) SharedConsoles(ctx context.Context) SharedConsoles {
	state := n.sharedState()
	if state == nil {
		return SharedConsoles{}
	}
	store, err := n.sharedStore(ctx)
	if err == nil {
		object, e := store.Object(ctx, "console", "management")
		if e == sql.ErrNoRows {
			return SharedConsoles{}
		}
		var value SharedConsoles
		if e == nil && json.Unmarshal(object.Body, &value) == nil {
			return value
		}
	}
	value := state.Consoles
	value.Cached = true
	return value
}

func (n *Node) SaveSharedConsoles(ctx context.Context, input SharedConsoles) (SharedConsoles, error) {
	store, err := n.sharedStore(ctx)
	if err != nil {
		return input, err
	}
	var result SharedConsoles
	err = store.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if w := requeststate.Current(ctx); w != nil {
			prior, e := t.Receipt(ctx, sharedsql.Receipt{Origin: w.Origin, Actor: w.Actor, Key: w.Key, Digest: w.Digest})
			if e == nil {
				return json.Unmarshal(prior.Result, &result)
			}
			if e != sql.ErrNoRows {
				return e
			}
		}
		if input.Primary == "" || input.Primary == input.Backup {
			return fault("invalid_request", "请选择不同的主用与备用管理节点")
		}
		for _, id := range []string{input.Primary, input.Backup} {
			if id == "" {
				continue
			}
			candidate, e := t.Object(ctx, "candidate", id)
			if e != nil || string(candidate.Body) != "true" {
				return fault("invalid_request", "管理节点尚未登记为协调候选")
			}
			if _, e = t.Object(ctx, "revocation", id); e == nil {
				return fault("forbidden", "管理节点已撤销")
			} else if e != sql.ErrNoRows {
				return e
			}
		}
		old, e := t.Object(ctx, "console", "management")
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if old.Revision != input.Revision {
			return sharedsql.ErrConflict
		}
		result = SharedConsoles{Revision: input.Revision + 1, Primary: input.Primary, Backup: input.Backup}
		object := sharedObject("console", "management", result.Revision, result)
		if e = t.Put(ctx, object, old.Revision); e != nil {
			return e
		}
		if w := requeststate.Current(ctx); w != nil {
			return t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "consoles", ResourceID: "management", Result: object.Body})
		}
		return nil
	})
	return result, sharedFault(err)
}
