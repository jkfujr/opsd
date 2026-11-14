package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) ScanDatabaseClusters(w http.ResponseWriter, r *http.Request, _ httpapi.ScanDatabaseClustersParams) {
	var input httpapi.DatabaseScanInput
	if !s.body(w, r, &input) {
		return
	}
	result, err := s.node.ScanDatabases(r.Context(), convertHTTP[cluster.DatabaseScanInput](input))
	s.resultError(w, r, 200, convertHTTP[httpapi.DatabaseScanResult](result), err)
}

func (s *Server) PreviewDatabaseDeployment(w http.ResponseWriter, r *http.Request, _ httpapi.PreviewDatabaseDeploymentParams) {
	var input httpapi.DatabaseDeploymentInput
	if !s.body(w, r, &input) {
		return
	}
	result, err := s.node.PreviewDatabaseDeployment(r.Context(), convertHTTP[cluster.DatabaseDeploymentInput](input), actor(r))
	s.resultError(w, r, 200, convertHTTP[httpapi.DatabaseDeploymentPreview](result), err)
}

func (s *Server) submitDatabaseDeployment(w http.ResponseWriter, r *http.Request, id, mode string) {
	var input httpapi.DatabaseDeploymentCommit
	if !s.body(w, r, &input) {
		return
	}
	request := convertHTTP[cluster.DatabaseDeploymentCommit](input)
	request.Actor, request.Key, request.Digest = actor(r), r.Header.Get("Idempotency-Key"), requestFingerprint(r)
	request.ExpectedCluster, request.ExpectedMode = id, mode
	op, err := s.node.SubmitDatabaseDeployment(r.Context(), request)
	if err == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, 202, convertHTTP[httpapi.Operation](op), err)
}

func (s *Server) CreateDatabaseDeployment(w http.ResponseWriter, r *http.Request, _ httpapi.CreateDatabaseDeploymentParams) {
	s.submitDatabaseDeployment(w, r, "", "create_or_import")
}
func (s *Server) AddDatabaseMembers(w http.ResponseWriter, r *http.Request, id string, _ httpapi.AddDatabaseMembersParams) {
	s.submitDatabaseDeployment(w, r, id, "members")
}
func (s *Server) AddDatabaseProxy(w http.ResponseWriter, r *http.Request, id string, _ httpapi.AddDatabaseProxyParams) {
	s.submitDatabaseDeployment(w, r, id, "proxy")
}
