package control

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"time"
)

func (s *Server) ListComposeProjects(w http.ResponseWriter, r *http.Request, _ httpapi.ListComposeProjectsParams) {
	nodes, err := s.nodes(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	out := httpapi.ComposeProjectList{Items: []httpapi.ComposeProjectItem{}, Nodes: []httpapi.ResourceNodeRead{}}
	for _, node := range nodes {
		if node.RevokedAt != nil || r.URL.Query().Get("node_id") != "" && r.URL.Query().Get("node_id") != node.Id {
			continue
		}
		var projects []protocol.ComposeProject
		read := s.node.CurrentRead(r.Context(), node.Id, "compose.list", nil, &projects, 5*time.Second)
		out.Nodes = append(out.Nodes, httpapi.ResourceNodeRead{NodeId: node.Id, NodeName: node.Name, Read: convertHTTP[httpapi.ReadStatus](read)})
		for _, project := range projects {
			item := convertHTTP[httpapi.ComposeProjectItem](project)
			item.NodeId = node.Id
			item.NodeName = node.Name
			out.Items = append(out.Items, item)
		}
	}
	write(w, 200, out)
}
func (s *Server) RegisterComposeProject(w http.ResponseWriter, r *http.Request, id, engineID, projectId string, _ httpapi.RegisterComposeProjectParams) {
	state := apiRequestOf(r.Context())
	s.submit(w, r, id, "compose_register", cluster.ComposeSelection{ID: projectId, EngineID: engineID, Create: state.create, ExpectedRevision: state.expectedValue}, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) GetComposeRegistration(w http.ResponseWriter, r *http.Request, node, engine, id string) {
	var ref protocol.ComposeRef
	err := s.node.Call(r.Context(), node, "compose.registration", cluster.ComposeSelection{ID: id, EngineID: engine}, &ref)
	if err == nil {
		s.setETag(w, r, protocol.ResourceVersion(ref))
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.ComposeRegistration](ref), err)
}
func (s *Server) ChangeComposeProject(w http.ResponseWriter, r *http.Request, id, engineID, projectId string, _ httpapi.ChangeComposeProjectParams) {
	var body httpapi.ComposeActionRequest
	if !s.body(w, r, &body) {
		return
	}
	s.submit(w, r, id, "compose", cluster.ComposeSelection{ID: projectId, EngineID: engineID, Action: string(body.Action), ExpectedRevision: body.ExpectedRevision}, r.Header.Get("Idempotency-Key"), "")
}
