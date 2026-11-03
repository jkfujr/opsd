package control

import (
	"encoding/json"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"time"

	"github.com/jkfujr/opsd/internal/account"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
)

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body httpapi.LoginRequest
	if !s.body(w, r, &body) {
		return
	}
	if body.Password == nil || body.Username == "" || len(*body.Password) > 1024 {
		s.fail(w, r, 422, "invalid_request", "账号和密码不能为空且长度须符合要求")
		return
	}
	var username, hash, entrance string
	var revision int
	if err := s.db.QueryRowContext(r.Context(), "SELECT username,password_hash,entrance,revision FROM account WHERE id=1").Scan(&username, &hash, &entrance, &revision); err != nil {
		s.fail(w, r, 500, "internal_error", "登录暂不可用")
		return
	}
	if body.Entrance != entrance {
		s.fail(w, r, 404, "not_found", "页面不存在")
		return
	}
	if !sameOrigin(r) {
		s.fail(w, r, 403, "csrf_rejected", "请求来源无效")
		return
	}
	ip := remoteIP(r)
	s.mu.Lock()
	cutoff := s.now().Add(-time.Minute)
	for source, failures := range s.failures {
		first := 0
		for first < len(failures) && !failures[first].After(cutoff) {
			first++
		}
		if first == len(failures) {
			delete(s.failures, source)
		} else {
			s.failures[source] = failures[first:]
		}
	}
	blocked := len(s.failures[ip])+s.inFlight[ip] >= 5
	if !blocked {
		select {
		case s.passwordSlots <- struct{}{}:
			s.inFlight[ip]++
		default:
			blocked = true
		}
	}
	s.mu.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, 429, "rate_limited", "登录尝试过于频繁，请稍后重试")
		return
	}
	valid := account.Verify(hash, *body.Password) && body.Username == username
	s.mu.Lock()
	s.inFlight[ip]--
	if s.inFlight[ip] == 0 {
		delete(s.inFlight, ip)
	}
	if !valid {
		s.failures[ip] = append(s.failures[ip], s.now())
	}
	<-s.passwordSlots
	s.mu.Unlock()
	if !valid {
		s.fail(w, r, 401, "unauthenticated", "账号或密码错误")
		return
	}
	token, id, csrf := identity.Random(), identity.Random(), identity.Random()
	now := s.now().UTC().Truncate(time.Second)
	expires := now.Add(7 * 24 * time.Hour)
	// 派生密码期间凭据可能已变更，旧结果不能再创建有效会话。
	result, err := s.db.ExecContext(r.Context(), `INSERT INTO sessions(id,token_hash,csrf_token,created_at,expires_at,last_seen_at,remote_address) SELECT ?,?,?,?,?,?,? FROM account WHERE id=1 AND revision=?`, id, digest(token), csrf, now.Unix(), expires.Unix(), now.Unix(), ip, revision)
	if err != nil {
		s.fail(w, r, 500, "internal_error", "登录失败，请重试")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		s.fail(w, r, 409, "conflict", "凭据已变更，请重新登录")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 60 * 60, Expires: expires})
	write(w, 200, httpapi.CurrentSession{LocalNodeId: s.node.ID, CsrfToken: csrf, LoginAddress: entrance, Session: httpapi.Session{Id: id, Username: username, CreatedAt: now, ExpiresAt: expires, LastSeenAt: now, RemoteAddress: ip, Current: true}})
}

func (s *Server) GetSession(w http.ResponseWriter, r *http.Request) {
	value := current(r)
	write(w, 200, httpapi.CurrentSession{LocalNodeId: s.node.ID, CsrfToken: value.csrf, Session: value.session, LoginAddress: value.entrance})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request, _ httpapi.LogoutParams) {
	id := current(r).session.Id
	if _, err := s.db.ExecContext(r.Context(), "UPDATE sessions SET revoked_at=? WHERE id=?", s.now().Unix(), id); err != nil {
		s.fail(w, r, 500, "internal_error", "退出失败，请重试")
		return
	}
	s.revokeStreams(id)
	s.clearCookie(w)
	w.WriteHeader(204)
}

func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request) {
	value := current(r)
	rows, err := s.db.QueryContext(r.Context(), "SELECT id,created_at,expires_at,last_seen_at,remote_address FROM sessions WHERE revoked_at IS NULL AND expires_at>? ORDER BY created_at DESC,id", s.now().Unix())
	if err != nil {
		s.fail(w, r, 500, "internal_error", "读取会话失败")
		return
	}
	defer rows.Close()
	items := []httpapi.Session{}
	for rows.Next() {
		var item httpapi.Session
		var created, expires, last int64
		if err := rows.Scan(&item.Id, &created, &expires, &last, &item.RemoteAddress); err != nil {
			s.fail(w, r, 500, "internal_error", "读取会话失败")
			return
		}
		item.Username = value.session.Username
		item.Current = item.Id == value.session.Id
		item.CreatedAt = time.Unix(created, 0).UTC()
		item.ExpiresAt = time.Unix(expires, 0).UTC()
		item.LastSeenAt = time.Unix(last, 0).UTC()
		items = append(items, item)
	}
	s.result(w, r, 200, httpapi.SessionList{Items: items}, rows.Err())
}

func (s *Server) RevokeSession(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevokeSessionParams) {
	result, err := s.db.ExecContext(r.Context(), "UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL", s.now().Unix(), id)
	if err != nil {
		s.fail(w, r, 500, "internal_error", "退出会话失败")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		s.fail(w, r, 500, "internal_error", "退出会话失败")
		return
	}
	if affected == 0 {
		s.fail(w, r, 404, "not_found", "会话不存在")
		return
	}
	s.revokeStreams(id)
	if current(r).session.Id == id {
		s.clearCookie(w)
	}
	w.WriteHeader(204)
}

func (s *Server) GetAccount(w http.ResponseWriter, r *http.Request) {
	write(w, 200, httpapi.Account{Username: current(r).session.Username})
}

func (s *Server) ChangeAccount(w http.ResponseWriter, r *http.Request, _ httpapi.ChangeAccountParams) {
	var body httpapi.AccountChange
	if !s.body(w, r, &body) {
		return
	}
	if body.CurrentPassword == nil {
		s.fail(w, r, 422, "invalid_request", "请输入当前密码")
		return
	}
	var username, hash, entrance string
	var revision int
	if err := s.db.QueryRowContext(r.Context(), "SELECT username,password_hash,entrance,revision FROM account WHERE id=1").Scan(&username, &hash, &entrance, &revision); err != nil {
		s.fail(w, r, 500, "internal_error", "读取账号失败")
		return
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		s.fail(w, r, 503, "dependency_unavailable", "密码校验繁忙，请稍后重试")
		return
	}
	if !account.Verify(hash, *body.CurrentPassword) {
		s.fail(w, r, 403, "forbidden", "当前密码错误")
		return
	}
	if body.Username != nil {
		username = *body.Username
	}
	if body.Entrance != nil {
		entrance = *body.Entrance
	}
	if err := account.Validate(username, entrance); err != nil {
		s.fail(w, r, 422, "invalid_request", err.Error())
		return
	}
	change := cluster.AccountChange{Revision: revision, Username: username, Entrance: entrance}
	if body.Password != nil {
		change.Password = *body.Password
	}
	payload, _ := json.Marshal(change)
	op, err := s.node.Submit(r.Context(), s.node.ID, cluster.Request{ID: cluster.NewOperationID(s.node.ID), Kind: "account", Payload: payload})
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		op, err = s.node.GetOperation(r.Context(), op.ID)
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		if operations.Terminal(op.State) {
			if op.State != protocol.Succeeded {
				s.fail(w, r, 409, "conflict", "账号修改未完成，请查看操作记录")
				return
			}
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
	s.revokeStreams("")
	s.clearCookie(w)
	write(w, 200, httpapi.AccountChanged{LoginAddress: entrance})
}

func (s *Server) GetVersion(w http.ResponseWriter, r *http.Request) {
	write(w, 200, httpapi.VersionInfo{NodeVersion: s.options.NodeVersion, FrontendVersion: s.options.FrontendVersion})
}
