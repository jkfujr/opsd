package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Server) GetStorageRoot(w http.ResponseWriter, r *http.Request, id string) {
	var out protocol.StorageRootView
	err := s.node.Call(r.Context(), id, "storage.root", nil, &out)
	if err == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, 200, out, err)
}

func (s *Server) SaveStorageRoot(w http.ResponseWriter, r *http.Request, id string, _ httpapi.SaveStorageRootParams) {
	var body httpapi.StorageRootInput
	if !s.body(w, r, &body) {
		return
	}
	in := convertHTTP[protocol.StorageRootInput](body)
	in.ExpectedRevision = expectedRevision(r)
	s.submit(w, r, id, "storage_root", in, r.Header.Get("Idempotency-Key"), "")
}
