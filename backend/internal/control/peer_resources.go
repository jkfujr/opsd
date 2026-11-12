package control

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) nodes(ctx context.Context) ([]httpapi.NodeStatus, error) {
	d, e := s.node.Directory(ctx)
	out := []httpapi.NodeStatus{}
	if e != nil {
		return out, e
	}
	revoked := map[string]bool{}
	for _, r := range d.Revocations {
		revoked[r.ID] = true
	}
	for _, m := range d.Members {
		if a := access(ctx); a != nil && !a.Token.Allows(a.Module, "read", m.ID, m.Region) {
			continue
		}
		n := httpapi.NodeStatus{Id: m.ID, Name: m.Name, PublicKey: m.Public, Local: m.ID == s.node.ID, Connected: s.node.Connected(m.ID), Writable: !revoked[m.ID]}
		n.Writable = n.Writable && n.Connected
		n.Region = &m.Region
		n.Address = &m.Address
		n.StorageAddress = &m.Storage
		if revoked[m.ID] {
			now := s.now()
			n.RevokedAt = &now
			n.Connected = false
			n.Writable = false
		}
		out = append(out, n)
	}
	return out, nil
}
func (s *Server) findNode(ctx context.Context, id string) (httpapi.NodeStatus, error) {
	items, e := s.nodes(ctx)
	if e != nil {
		return httpapi.NodeStatus{}, e
	}
	for _, m := range items {
		if m.Id == id {
			return m, nil
		}
	}
	return httpapi.NodeStatus{}, &cluster.Fault{Code: "not_found", Message: "节点不存在"}
}
func (s *Server) GetNodeSettings(w http.ResponseWriter, r *http.Request, id string) {
	c, e := s.node.ReadConfig(r.Context(), id)
	if e == nil {
		s.setETag(w, r, c.Revision)
	}
	s.resultError(w, r, 200, c, e)
}
func (s *Server) ChangeNodeSettings(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeNodeSettingsParams) {
	base, e := s.node.ReadConfig(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	var c cluster.Config
	if !s.mergeBody(w, r, base, &c) {
		return
	}
	c.Revision = expectedRevision(r)
	c.Cached = false
	c.LastSyncedAt = nil
	c.NetworkError = nil
	s.submit(w, r, id, "config", c, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) GetNodeAccount(w http.ResponseWriter, r *http.Request, id string) {
	a, e := s.node.ReadAccount(r.Context(), id)
	if e == nil {
		s.setETag(w, r, a.Revision)
	}
	out := map[string]any{"username": a.Username, "entrance": a.Entrance, "revision": a.Revision, "cached": a.Cached}
	if a.LastSyncedAt != nil {
		out["last_synced_at"] = a.LastSyncedAt
	}
	s.resultError(w, r, 200, out, e)
}
func (s *Server) ChangeNodeAccount(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeNodeAccountParams) {
	base, e := s.node.ReadAccount(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	var a cluster.AccountChange
	if !s.mergeBody(w, r, map[string]any{"username": base.Username, "entrance": base.Entrance}, &a) {
		return
	}
	a.Revision = int(expectedRevision(r))
	s.submit(w, r, id, "account", a, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) RevealNodePassword(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevealNodePasswordParams) {
	var input httpapi.PasswordAction
	if !s.body(w, r, &input) {
		return
	}
	a, e := s.node.Password(r.Context(), id, uint64(input.ExpectedRevision))
	s.resultError(w, r, 200, a, e)
}
func (s *Server) GetStorageStatus(w http.ResponseWriter, r *http.Request, id string) {
	var status cluster.StorageStatus
	e := s.node.Call(r.Context(), id, "storage.status", nil, &status)
	s.resultError(w, r, 200, status, e)
}
func (s *Server) submit(w http.ResponseWriter, r *http.Request, target, kind string, payload any, key, id string) {
	b, e := json.Marshal(payload)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if key == "" && id == "" {
		s.fail(w, r, 422, "invalid_request", "缺少请求标识")
		return
	}
	if actor(r) != "" {
		id = ""
		key = r.Header.Get("Idempotency-Key")
		if key == "" {
			s.fail(w, r, 422, "invalid_request", "缺少请求标识")
			return
		}
	}
	var expected *uint64
	if state := apiRequestOf(r.Context()); state != nil {
		expected = state.expected
	}
	op, e := s.node.Submit(r.Context(), target, cluster.Request{ExpectedRevision: expected, InputDigest: requestFingerprint(r), Actor: actor(r), ID: id, Kind: kind, Payload: b, Key: key})
	if e == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, 202, op, e)
}
func (s *Server) resultError(w http.ResponseWriter, r *http.Request, status int, value any, e error) {
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	write(w, status, value)
}
func digest(value string) []byte { return cluster.Digest(value) }
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	session := current(r)
	sub := &subscriber{session: session.session.Id, events: make(chan string, 1), done: make(chan struct{})}
	s.mu.Lock()
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, sub); s.mu.Unlock() }()
	updates, unsubscribe := s.node.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	event := "event: ready\ndata: {}\n\n"
	for {
		if _, e := s.authenticate(r); e != nil {
			return
		}
		controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, e := w.Write([]byte(event)); e != nil {
			return
		}
		if controller.Flush() != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case change := <-updates:
			changes := map[string]cluster.Change{change.Resource + "/" + change.NodeID: change}
			timer := time.NewTimer(100 * time.Millisecond)
		collect:
			for {
				select {
				case next := <-updates:
					changes[next.Resource+"/"+next.NodeID] = next
				case <-timer.C:
					break collect
				case <-r.Context().Done():
					timer.Stop()
					return
				case <-sub.done:
					timer.Stop()
					return
				}
			}
			items := make([]cluster.Change, 0, len(changes))
			for _, v := range changes {
				items = append(items, v)
			}
			data, _ := json.Marshal(map[string]any{"changes": items})
			event = "event: invalidate\ndata: " + string(data) + "\n\n"
		case resource := <-sub.events:
			data, _ := json.Marshal(map[string]string{"resource": resource})
			event = "event: invalidate\ndata: " + string(data) + "\n\n"
		case <-ticker.C:
			event = ": keepalive\n\n"
		}
	}
}
