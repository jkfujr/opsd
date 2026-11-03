package control

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) nodes(ctx context.Context) ([]httpapi.Node, error) {
	d, e := s.node.Directory(ctx)
	out := []httpapi.Node{}
	if e != nil {
		return out, e
	}
	revoked := map[string]bool{}
	for _, r := range d.Revocations {
		revoked[r.ID] = true
	}
	for _, m := range d.Members {
		n := httpapi.Node{Id: m.ID, Name: m.Name, PublicKey: m.Public, Local: m.ID == s.node.ID, Connected: s.node.Connected(m.ID), Writable: !revoked[m.ID]}
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
func (s *Server) findNode(ctx context.Context, id string) (httpapi.Node, error) {
	items, e := s.nodes(ctx)
	if e != nil {
		return httpapi.Node{}, e
	}
	for _, m := range items {
		if m.Id == id {
			return m, nil
		}
	}
	return httpapi.Node{}, &cluster.Fault{Code: "not_found", Message: "节点不存在"}
}
func (s *Server) GetNodeSettings(w http.ResponseWriter, r *http.Request, id string) {
	c, e := s.node.ReadConfig(r.Context(), id)
	s.resultError(w, r, 200, c, e)
}
func (s *Server) ChangeNodeSettings(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeNodeSettingsParams) {
	var c cluster.Config
	if !s.body(w, r, &c) {
		return
	}
	s.submit(w, r, id, "config", c, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) GetNodeAccount(w http.ResponseWriter, r *http.Request, id string) {
	a, e := s.node.ReadAccount(r.Context(), id)
	out := map[string]any{"username": a.Username, "entrance": a.Entrance, "revision": a.Revision, "cached": a.Cached}
	if a.LastSyncedAt != nil {
		out["last_synced_at"] = a.LastSyncedAt
	}
	s.resultError(w, r, 200, out, e)
}
func (s *Server) ChangeNodeAccount(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeNodeAccountParams) {
	var a cluster.AccountChange
	if !s.body(w, r, &a) {
		return
	}
	s.submit(w, r, id, "account", a, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) RevealNodePassword(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RevealNodePasswordParams) {
	a, e := s.node.Password(r.Context(), id)
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
	op, e := s.node.Submit(r.Context(), target, cluster.Request{ID: id, Kind: kind, Payload: b, Key: key})
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
