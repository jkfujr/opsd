package control

import (
	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"net/http"
)

func (s *Server) ListAPITokens(w http.ResponseWriter, r *http.Request, node string, _ httpapi.ListAPITokensParams) {
	items := []httpapi.APIToken{}
	e := s.node.Call(r.Context(), node, "api_token.list", cluster.TokenCommand{}, &items)
	s.resultError(w, r, 200, httpapi.APITokenList{Items: items}, e)
}
func (s *Server) GetAPIToken(w http.ResponseWriter, r *http.Request, node, id string) {
	var item httpapi.APIToken
	e := s.node.Call(r.Context(), node, "api_token.get", cluster.TokenCommand{ID: id}, &item)
	if e == nil {
		s.setETag(w, r, uint64(item.Revision))
	}
	s.resultError(w, r, 200, item, e)
}
func (s *Server) CreateAPIToken(w http.ResponseWriter, r *http.Request, node string, _ httpapi.CreateAPITokenParams) {
	var input httpapi.APITokenInput
	if !s.body(w, r, &input) {
		return
	}
	var out httpapi.APITokenCreated
	e := s.node.Call(r.Context(), node, "api_token.create", cluster.TokenCommand{Write: writeIdentity(r), Input: convertHTTP[apiauth.Input](input)}, &out)
	if e == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/nodes/"+node+"/api-tokens/"+out.Token.Id)
	}
	s.resultError(w, r, 201, out, e)
}
func (s *Server) UpdateAPIToken(w http.ResponseWriter, r *http.Request, node, id string, _ httpapi.UpdateAPITokenParams) {
	var current httpapi.APIToken
	e := s.node.Call(r.Context(), node, "api_token.get", cluster.TokenCommand{ID: id}, &current)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	base := convertHTTP[httpapi.APITokenInput](current)
	var input httpapi.APITokenInput
	if !s.mergeBody(w, r, base, &input) {
		return
	}
	domain := convertHTTP[apiauth.Input](input)
	domain.Revision = expectedRevision(r)
	var item httpapi.APIToken
	e = s.node.Call(r.Context(), node, "api_token.update", cluster.TokenCommand{ID: id, Input: domain}, &item)
	s.resultError(w, r, 200, item, e)
}
func (s *Server) DeleteAPIToken(w http.ResponseWriter, r *http.Request, node, id string, _ httpapi.DeleteAPITokenParams) {
	e := s.node.Call(r.Context(), node, "api_token.delete", cluster.TokenCommand{ID: id, Input: apiauth.Input{Revision: expectedRevision(r)}}, nil)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) RegenerateAPIToken(w http.ResponseWriter, r *http.Request, node, id string, _ httpapi.RegenerateAPITokenParams) {
	var input httpapi.RegenerateAPITokenJSONRequestBody
	if !s.body(w, r, &input) {
		return
	}
	var out httpapi.APITokenCreated
	e := s.node.Call(r.Context(), node, "api_token.regenerate", cluster.TokenCommand{Write: writeIdentity(r), ID: id, Input: apiauth.Input{Revision: uint64(input.ExpectedRevision)}}, &out)
	s.resultError(w, r, 200, out, e)
}
