package control

import (
	"encoding/json"
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) tokenOwner(r *http.Request) string {
	if id := r.URL.Query().Get("node_id"); id != "" {
		return id
	}
	return s.node.ID
}
func (s *Server) ListAPITokens(w http.ResponseWriter, r *http.Request, _ httpapi.ListAPITokensParams) {
	items := []httpapi.APIToken{}
	e := s.node.Call(r.Context(), s.tokenOwner(r), "api_token.list", cluster.TokenCommand{}, &items)
	s.resultError(w, r, 200, httpapi.APITokenList{Items: items}, e)
}
func (s *Server) tokenInput(w http.ResponseWriter, r *http.Request) (cluster.TokenCommand, bool) {
	var input httpapi.APITokenInput
	var command cluster.TokenCommand
	if !s.body(w, r, &input) {
		return command, false
	}
	b, _ := json.Marshal(input)
	if e := json.Unmarshal(b, &command.Input); e != nil {
		s.fail(w, r, 422, "invalid_request", "令牌请求无效")
		return command, false
	}
	return command, true
}
func (s *Server) CreateAPIToken(w http.ResponseWriter, r *http.Request, _ httpapi.CreateAPITokenParams) {
	command, ok := s.tokenInput(w, r)
	if !ok {
		return
	}
	var out httpapi.APITokenCreated
	e := s.node.Call(r.Context(), s.tokenOwner(r), "api_token.create", command, &out)
	s.resultError(w, r, 201, out, e)
}
func (s *Server) UpdateAPIToken(w http.ResponseWriter, r *http.Request, id string, _ httpapi.UpdateAPITokenParams) {
	command, ok := s.tokenInput(w, r)
	if !ok {
		return
	}
	command.ID = id
	var out httpapi.APIToken
	e := s.node.Call(r.Context(), s.tokenOwner(r), "api_token.update", command, &out)
	s.resultError(w, r, 200, out, e)
}
func (s *Server) DeleteAPIToken(w http.ResponseWriter, r *http.Request, id string, p httpapi.DeleteAPITokenParams) {
	command := cluster.TokenCommand{ID: id}
	if p.Revision < 1 {
		s.fail(w, r, 422, "invalid_request", "缺少令牌版本")
		return
	}
	command.Input.Revision = uint64(p.Revision)
	if e := s.node.Call(r.Context(), s.tokenOwner(r), "api_token.delete", command, nil); e != nil {
		s.operationError(w, r, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) RegenerateAPIToken(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RegenerateAPITokenParams) {
	var input httpapi.TokenRevision
	if !s.body(w, r, &input) {
		return
	}
	command := cluster.TokenCommand{ID: id}
	if input.Revision < 1 {
		s.fail(w, r, 422, "invalid_request", "缺少令牌版本")
		return
	}
	command.Input.Revision = uint64(input.Revision)
	var out httpapi.APITokenCreated
	e := s.node.Call(r.Context(), s.tokenOwner(r), "api_token.regenerate", command, &out)
	s.resultError(w, r, 200, out, e)
}
func (s *Server) VerifyAPIToken(w http.ResponseWriter, r *http.Request) {
	if a := access(r.Context()); a != nil {
		write(w, 200, a.Token)
		return
	}
	s.fail(w, r, 401, "unauthenticated", "请提供 Bearer 令牌")
}
