package control

import (
	"database/sql"
	"fmt"
	"github.com/jkfujr/opsd/internal/cluster"
	"net/http"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
)

func (s *Server) ListNodes(w http.ResponseWriter, r *http.Request) {
	items, err := s.nodes(r.Context())
	s.result(w, r, 200, httpapi.NodeList{Items: items}, err)
}

func (s *Server) GetNode(w http.ResponseWriter, r *http.Request, id string) {
	node, err := s.findNode(r.Context(), id)
	s.result(w, r, 200, node, err)
}

func (s *Server) RenameNode(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RenameNodeParams) {
	var body httpapi.NodePatch
	if !s.body(w, r, &body) {
		return
	}
	s.submit(w, r, id, "name", strings.TrimSpace(body.Name), "", cluster.NewOperationID(id))
}
func (s *Server) RevokeNode(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeNodeParams) {
	if e := s.node.Revoke(r.Context(), id); e != nil {
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
	token, id, expires, e := s.node.Token(r.Context())
	s.result(w, r, 201, httpapi.EnrollmentTokenCreated{Token: token, Version: s.options.NodeVersion, InstallerUrl: fmt.Sprintf("https://github.com/jkfujr/opsd/releases/download/v%s/install.sh", s.options.NodeVersion), PeerAddress: c.Address, PeerPublicKey: identity.Encode(s.node.Key.Public), Record: httpapi.EnrollmentToken{Id: id, CreatedAt: s.now().UTC().Truncate(time.Second), ExpiresAt: expires}}, e)
}

func (s *Server) ListEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
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
		if consumed.Valid {
			value := time.Unix(consumed.Int64, 0).UTC()
			item.ConsumedAt = &value
		}
		if revoked.Valid {
			value := time.Unix(revoked.Int64, 0).UTC()
			item.RevokedAt = &value
		}
		items = append(items, item)
	}
	s.result(w, r, 200, httpapi.EnrollmentTokenList{Items: items}, rows.Err())
}

func (s *Server) RevokeEnrollmentToken(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeEnrollmentTokenParams) {
	result, err := s.db.ExecContext(r.Context(), "UPDATE enrollment_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE id=?", s.now().Unix(), id)
	if err != nil {
		s.fail(w, r, 500, "internal_error", "撤销接入码失败")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		s.fail(w, r, 404, "not_found", "接入码不存在")
		return
	}
	w.WriteHeader(204)
}
