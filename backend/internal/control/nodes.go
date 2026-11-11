package control

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/requeststate"
)

func (s *Server) ListNodes(w http.ResponseWriter, r *http.Request, _ httpapi.ListNodesParams) {
	items, err := s.nodes(r.Context())
	s.result(w, r, 200, httpapi.NodeList{Items: items}, err)
}

func (s *Server) GetNode(w http.ResponseWriter, r *http.Request, id string) {
	member, e := s.node.Member(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	s.setETag(w, r, member.ConfigurationRevision)
	s.result(w, r, 200, httpapi.Node{Id: member.ID, Name: member.Name, PublicKey: member.Public, Local: member.ID == s.node.ID, Region: &member.Region, Address: &member.Address, Revision: int64(member.ConfigurationRevision)}, nil)
}

func (s *Server) RenameNode(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RenameNodeParams) {
	var body httpapi.NodePatch
	if !s.body(w, r, &body) {
		return
	}
	s.submit(w, r, id, "name", strings.TrimSpace(body.Name), r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) RevokeNode(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeNodeParams) {
	var input httpapi.NodeAction
	if !s.body(w, r, &input) {
		return
	}
	if e := s.node.RevokeRevision(requeststate.With(r.Context(), writeIdentity(r)), id, uint64(input.ExpectedRevision)); e != nil {
		s.operationError(w, r, e)
		return
	}
	s.GetNode(w, r, id)
}

func (s *Server) CreateEnrollmentToken(w http.ResponseWriter, r *http.Request, _ httpapi.CreateEnrollmentTokenParams) {
	c, e := s.node.Config(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if c.AddressMode == "auto" {
		addresses, err := s.node.RefreshPublic(r.Context())
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		if len(addresses.IPv4.Addresses) == 0 && len(addresses.IPv6.Addresses) == 0 {
			s.fail(w, r, 409, "conflict", "公网地址尚未确认，请在节点详情设置地址")
			return
		}
		c, e = s.node.Config(r.Context())
		if e != nil {
			s.operationError(w, r, e)
			return
		}
	}
	token, id, expires, e := s.node.Token(requeststate.With(r.Context(), writeIdentity(r)))
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	w.Header().Set("Location", httpapi.APIBasePath+"/enrollment-tokens/"+id)
	s.result(w, r, 201, httpapi.EnrollmentTokenCreated{Token: token, Version: s.options.NodeVersion, InstallerUrl: fmt.Sprintf("https://github.com/jkfujr/opsd/releases/download/v%s/install.sh", s.options.NodeVersion), PeerAddress: c.Address, PeerPublicKey: identity.Encode(s.node.Key.Public), Record: httpapi.EnrollmentToken{Id: id, Revision: 1, CreatedAt: s.now().UTC().Truncate(time.Second), ExpiresAt: expires}}, nil)
}

func (s *Server) ListEnrollmentTokens(w http.ResponseWriter, r *http.Request, _ httpapi.ListEnrollmentTokensParams) {
	rows, err := s.db.QueryContext(r.Context(), "SELECT id,created_at,expires_at,consumed_at,revoked_at FROM enrollment_tokens ORDER BY created_at DESC,id")
	if err != nil {
		s.fail(w, r, 500, "internal_error", "读取接入码失败")
		return
	}
	defer rows.Close()
	items := []httpapi.EnrollmentToken{}
	for rows.Next() {
		var item httpapi.EnrollmentToken
		var created, expires int64
		var consumed, revoked sql.NullInt64
		if err := rows.Scan(&item.Id, &created, &expires, &consumed, &revoked); err != nil {
			s.fail(w, r, 500, "internal_error", "读取接入码失败")
			return
		}
		item.CreatedAt = time.Unix(created, 0).UTC()
		item.ExpiresAt = time.Unix(expires, 0).UTC()
		item.Revision = 1
		if consumed.Valid {
			item.Revision++
			value := time.Unix(consumed.Int64, 0).UTC()
			item.ConsumedAt = &value
		}
		if revoked.Valid {
			item.Revision += 2
			value := time.Unix(revoked.Int64, 0).UTC()
			item.RevokedAt = &value
		}
		items = append(items, item)
	}
	s.result(w, r, 200, httpapi.EnrollmentTokenList{Items: items}, rows.Err())
}

func (s *Server) GetEnrollmentToken(w http.ResponseWriter, r *http.Request, id string) {
	item, err := enrollmentToken(r.Context(), s.db, id)
	if err == nil {
		s.setETag(w, r, item.Revision)
	}
	s.resultError(w, r, 200, item, err)
}
func enrollmentToken(ctx context.Context, q requeststate.Querier, id string) (httpapi.EnrollmentToken, error) {
	item := httpapi.EnrollmentToken{Id: id, Revision: 1}
	var created, expires int64
	var consumed, revoked sql.NullInt64
	err := q.QueryRowContext(ctx, "SELECT created_at,expires_at,consumed_at,revoked_at FROM enrollment_tokens WHERE id=?", id).Scan(&created, &expires, &consumed, &revoked)
	item.CreatedAt, item.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	if consumed.Valid {
		v := time.Unix(consumed.Int64, 0).UTC()
		item.ConsumedAt = &v
		item.Revision++
	}
	if revoked.Valid {
		v := time.Unix(revoked.Int64, 0).UTC()
		item.RevokedAt = &v
		item.Revision += 2
	}
	return item, err
}

func (s *Server) RevokeEnrollmentToken(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeEnrollmentTokenParams) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	defer tx.Rollback()
	item, err := enrollmentToken(r.Context(), tx, id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	if !s.checkRevision(w, r, uint64(item.Revision)) {
		return
	}
	result, err := tx.ExecContext(r.Context(), "UPDATE enrollment_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE id=?", s.now().Unix(), id)
	if err != nil {
		s.fail(w, r, 500, "internal_error", "撤销接入码失败")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		s.fail(w, r, 404, "not_found", "接入码不存在")
		return
	}
	if err = tx.Commit(); err != nil {
		s.operationError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
