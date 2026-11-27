package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Server) PreviewStoragePool(w http.ResponseWriter, r *http.Request, _ httpapi.PreviewStoragePoolParams) {
	var input protocol.PoolInput
	if !s.body(w, r, &input) {
		return
	}
	plan, err := s.node.PreviewPool(r.Context(), input)
	s.resultError(w, r, http.StatusOK, plan, err)
}

func (s *Server) CreateStoragePool(w http.ResponseWriter, r *http.Request, _ httpapi.CreateStoragePoolParams) {
	var input protocol.PoolCreate
	if !s.body(w, r, &input) {
		return
	}
	owner := cluster.OperationOwner(input.PlanID)
	if owner == "" || !operations.ValidID(input.PlanID) {
		s.fail(w, r, http.StatusUnprocessableEntity, "invalid_request", "存储池计划标识无效")
		return
	}
	s.submit(w, r, owner, "storage_pool_create", input, r.Header.Get("Idempotency-Key"), "")
}

func (s *Server) ListStoragePools(w http.ResponseWriter, r *http.Request, p httpapi.ListStoragePoolsParams) {
	limit, cursor := 50, ""
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	if limit < 1 || limit > 100 || cursor != "" && (cluster.OperationOwner(cursor) == "" || !operations.ValidID(cursor)) {
		s.fail(w, r, http.StatusBadRequest, "malformed_request", "存储池游标或分页范围无效")
		return
	}
	out, err := s.node.Pools(r.Context(), cursor, limit)
	s.resultError(w, r, http.StatusOK, out, err)
}

func (s *Server) GetStoragePool(w http.ResponseWriter, r *http.Request, id string) {
	out, err := s.node.Pool(r.Context(), id)
	if err == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, http.StatusOK, out, err)
}

func (s *Server) SaveStoragePoolEC(w http.ResponseWriter, r *http.Request, id string, _ httpapi.SaveStoragePoolECParams) {
	var input protocol.PoolECSettings
	if !s.body(w, r, &input) {
		return
	}
	owner := cluster.OperationOwner(id)
	if owner == "" || !operations.ValidID(id) {
		s.fail(w, r, http.StatusUnprocessableEntity, "invalid_request", "存储池标识无效")
		return
	}
	input.ID = id
	input.Revision = expectedRevision(r)
	s.submit(w, r, owner, "storage_pool_ec", input, r.Header.Get("Idempotency-Key"), "")
}
