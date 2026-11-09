package control

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Server) ListContainers(w http.ResponseWriter, r *http.Request, _ httpapi.ListContainersParams) {
	ctx := r.Context()
	nodes, err := s.nodes(ctx)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	target, stateFilter, search := r.URL.Query().Get("node_id"), r.URL.Query().Get("state"), strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	out := httpapi.ContainerList{Items: []httpapi.ContainerListItem{}, Nodes: []httpapi.DockerNodeState{}}
	for _, node := range nodes {
		if node.RevokedAt != nil || target != "" && node.Id != target {
			continue
		}
		func() {
			snapshot := protocol.DockerState{State: "offline", Containers: []protocol.ContainerSummary{}, CheckedAt: s.now().UTC()}
			read := s.node.CurrentRead(ctx, node.Id, "containers.read", nil, &snapshot, 3*time.Second)
			if read.State != "ready" {
				snapshot.State = read.State
				snapshot.Error = read.Error
			}
			status := httpapi.DockerNodeState{NodeId: node.Id, NodeName: node.Name, EngineId: snapshot.EngineID, Version: snapshot.Version, State: httpapi.DockerNodeStateState(snapshot.State), CheckedAt: snapshot.CheckedAt}
			status.Read = convertHTTP[httpapi.ReadStatus](read)
			if snapshot.Error != nil {
				status.Error = &httpapi.NetworkError{Code: snapshot.Error.Code, Message: snapshot.Error.Message}
			}
			items := []httpapi.ContainerListItem{}
			if read.DataAt != nil {
				for _, container := range snapshot.Containers {
					matches := stateFilter == "" || container.State == stateFilter || stateFilter == "stopped" && (container.State == "created" || container.State == "exited" || container.State == "dead")
					if !matches || search != "" && !strings.Contains(strings.ToLower(container.Name), search) {
						continue
					}
					var item httpapi.ContainerListItem
					body, _ := json.Marshal(container)
					if json.Unmarshal(body, &item) != nil {
						continue
					}
					item.NodeId, item.NodeName = node.Id, node.Name
					items = append(items, item)
				}
			}
			out.Nodes = append(out.Nodes, status)
			out.Items = append(out.Items, items...)
		}()
	}
	if target != "" && len(out.Nodes) == 0 {
		s.operationError(w, r, &cluster.Fault{Code: "not_found", Message: "节点不存在或已撤销"})
		return
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].NodeName < out.Nodes[j].NodeName })
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.NodeName != b.NodeName {
			return a.NodeName < b.NodeName
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ContainerId < b.ContainerId
	})
	s.resultError(w, r, 200, out, nil)
}
func (s *Server) GetContainer(w http.ResponseWriter, r *http.Request, id, containerID string, params httpapi.GetContainerParams) {
	ref := protocol.ContainerRef{EngineID: params.EngineId, ContainerID: containerID}
	if err := ref.Validate(); err != nil {
		s.fail(w, r, 422, "invalid_request", err.Error())
		return
	}
	var result protocol.ContainerDetail
	err := s.node.Call(r.Context(), id, "container.inspect", &protocol.ContainerInspect{ContainerRef: ref}, &result)
	s.resultError(w, r, 200, result, err)
}
func (s *Server) GetContainerLogs(w http.ResponseWriter, r *http.Request, id, containerID string, params httpapi.GetContainerLogsParams) {
	request := protocol.ContainerLogsRead{ContainerRef: protocol.ContainerRef{EngineID: params.EngineId, ContainerID: containerID}, Tail: 200}
	if params.Tail != nil {
		request.Tail = *params.Tail
	}
	if err := request.ContainerRef.Validate(); err != nil {
		s.fail(w, r, 422, "invalid_request", err.Error())
		return
	}
	if request.Tail < 1 || request.Tail > 1000 {
		s.fail(w, r, 422, "invalid_request", "日志行数须为 1 至 1000")
		return
	}
	var result protocol.ContainerLogs
	err := s.node.Call(r.Context(), id, "container.logs", &request, &result)
	s.resultError(w, r, 200, result, err)
}
func (s *Server) ChangeContainer(w http.ResponseWriter, r *http.Request, id, containerID string, _ httpapi.ChangeContainerParams) {
	var body httpapi.ContainerActionRequest
	if !s.body(w, r, &body) {
		return
	}
	action := protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: body.EngineId, ContainerID: containerID}, Action: string(body.Action)}
	if err := action.Validate(); err != nil {
		s.fail(w, r, 422, "invalid_request", err.Error())
		return
	}
	s.submit(w, r, id, "container", action, r.Header.Get("Idempotency-Key"), "")
}
