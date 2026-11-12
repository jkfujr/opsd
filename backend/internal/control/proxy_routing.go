package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) GetProxyRouting(w http.ResponseWriter, r *http.Request, id string) {
	c, err := s.node.DatabaseCluster(r.Context(), id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	status, err := s.node.GetProxyRouting(r.Context(), c)
	s.resultError(w, r, 200, convertHTTP[httpapi.ProxyRoutingStatus](status), err)
}

func (s *Server) PreviewProxyRouting(w http.ResponseWriter, r *http.Request, id string, _ httpapi.PreviewProxyRoutingParams) {
	var input httpapi.ProxyRoutingPreviewInput
	if !s.body(w, r, &input) {
		return
	}
	c, err := s.node.DatabaseCluster(r.Context(), id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	preview, err := s.node.PreviewProxyRouting(r.Context(), c, convertHTTP[cluster.ProxyRoutingPreviewInput](input))
	s.resultError(w, r, 200, convertHTTP[httpapi.ProxyRoutingPreview](preview), err)
}

func (s *Server) ApplyProxyRouting(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ApplyProxyRoutingParams) {
	var input httpapi.ProxyRoutingPreview
	if !s.body(w, r, &input) {
		return
	}
	c, err := s.node.DatabaseCluster(r.Context(), id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	op, err := s.node.ApplyProxyRouting(r.Context(), c, convertHTTP[cluster.ProxyRoutingPreview](input), actor(r), r.Header.Get("Idempotency-Key"), requestFingerprint(r))
	if err == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, 202, convertHTTP[httpapi.Operation](op), err)
}
