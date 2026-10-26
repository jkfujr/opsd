package control

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
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
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 255 {
		s.fail(w, r, 422, "invalid_request", "名称须为 1 至 255 字节")
		return
	}
	result, err := s.db.ExecContext(r.Context(), "UPDATE nodes SET name=? WHERE id=?", name, id)
	if err != nil {
		s.fail(w, r, 500, "internal_error", "修改名称失败")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		s.fail(w, r, 404, "not_found", "节点不存在")
		return
	}
	s.notify("nodes")
	s.GetNode(w, r, id)
}

func (s *Server) RevokeNode(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeNodeParams) {
	s.lifecycle.Lock()
	result, err := s.db.ExecContext(r.Context(), "UPDATE nodes SET revoked_at=COALESCE(revoked_at,?) WHERE id=?", s.now().Unix(), id)
	if err == nil {
		var affected int64
		affected, err = result.RowsAffected()
		if affected == 0 {
			err = sql.ErrNoRows
		}
	}
	if err == nil {
		s.mu.Lock()
		if conn := s.connections[id]; conn != nil {
			delete(s.connections, id)
			conn.channel.Close()
		}
		s.mu.Unlock()
	}
	s.lifecycle.Unlock()
	if err != nil {
		s.result(w, r, 200, nil, err)
		return
	}
	s.notify("nodes")
	s.GetNode(w, r, id)
}

func (s *Server) CreateEnrollmentToken(w http.ResponseWriter, r *http.Request, _ httpapi.CreateEnrollmentTokenParams) {
	token, id := identity.Random(), identity.Random()
	now := s.now().UTC().Truncate(time.Second)
	expires := now.Add(24 * time.Hour)
	_, err := s.db.ExecContext(r.Context(), "INSERT INTO enrollment_tokens(id,token_hash,created_at,expires_at) VALUES(?,?,?,?)", id, digest(token), now.Unix(), expires.Unix())
	address := s.options.ControlAddress
	if address == "" {
		address = "http://" + r.Host
	}
	s.result(w, r, 201, httpapi.EnrollmentTokenCreated{Token: token, ControlAddress: address, ControlPublicKey: identity.Encode(s.key.Public), Record: httpapi.EnrollmentToken{Id: id, CreatedAt: now, ExpiresAt: expires}}, err)
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
