package control

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

func (s *Server) GetSharedStorage(w http.ResponseWriter, r *http.Request) {
	directory, err := s.node.Directory(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	out := httpapi.SharedStorageOverview{Local: convertHTTP[httpapi.SharedStorageStatus](s.node.SharedStatus(r.Context())), Nodes: []httpapi.SharedStorageNode{}}
	for _, member := range directory.Members {
		if s.node.Active(r.Context(), member.ID) {
			out.Nodes = append(out.Nodes, httpapi.SharedStorageNode{NodeId: member.ID, Name: member.Name})
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i := range out.Nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var status cluster.SharedStatus
			var err error
			select {
			case slots <- struct{}{}:
				err = s.node.Call(ctx, out.Nodes[i].NodeId, "shared.status", nil, &status)
				<-slots
			case <-ctx.Done():
				err = ctx.Err()
			}
			if err != nil {
				status = cluster.SharedStatus{State: "unavailable", Phase: "unknown", Error: &protocol.Error{Code: "node_unavailable", Message: "节点暂不可用"}}
			}
			out.Nodes[i].Status = convertHTTP[httpapi.SharedStorageStatus](status)
		}()
	}
	wg.Wait()
	write(w, http.StatusOK, out)
}

func (s *Server) GetSharedConsoles(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, convertHTTP[httpapi.SharedConsoles](s.node.SharedConsoles(r.Context())))
}

func (s *Server) SaveSharedConsoles(w http.ResponseWriter, r *http.Request, _ httpapi.SaveSharedConsolesParams) {
	var input httpapi.SharedConsoles
	if !s.body(w, r, &input) {
		return
	}
	value, err := s.node.SaveSharedConsoles(requeststate.With(r.Context(), s.writeIdentity(r)), convertHTTP[cluster.SharedConsoles](input))
	s.resultError(w, r, http.StatusOK, convertHTTP[httpapi.SharedConsoles](value), err)
}

func (s *Server) InitializeSharedStorage(w http.ResponseWriter, r *http.Request, _ httpapi.InitializeSharedStorageParams) {
	var input httpapi.SharedStorageInitialize
	if !s.body(w, r, &input) {
		return
	}
	op, err := s.node.StartSharedInitialization(requeststate.With(r.Context(), s.writeIdentity(r)), convertHTTP[cluster.SharedInitialize](input), r.Header.Get("Idempotency-Key"))
	if err == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, http.StatusAccepted, convertHTTP[httpapi.Operation](op), err)
}

func (s *Server) GetNodeSharedStorage(w http.ResponseWriter, r *http.Request, id string) {
	var status cluster.SharedStatus
	err := s.node.Call(r.Context(), id, "shared.status", nil, &status)
	s.resultError(w, r, http.StatusOK, convertHTTP[httpapi.SharedStorageStatus](status), err)
}

func (s *Server) RecoverNodeSharedStorage(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RecoverNodeSharedStorageParams) {
	var status cluster.SharedStatus
	err := s.node.Call(r.Context(), id, "shared.recover", nil, &status)
	s.resultError(w, r, http.StatusOK, convertHTTP[httpapi.SharedStorageStatus](status), err)
}

func (s *Server) ChangeDatabaseCoordinator(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeDatabaseCoordinatorParams) {
	var input httpapi.DatabaseCoordinatorChange
	if !s.body(w, r, &input) {
		return
	}
	c, err := s.node.TransferDatabaseCoordinator(requeststate.With(r.Context(), s.writeIdentity(r)), id, input.Coordinator, uint64(input.ExpectedRevision))
	s.resultError(w, r, http.StatusOK, convertHTTP[httpapi.ManagedDatabaseCluster](c), err)
}
