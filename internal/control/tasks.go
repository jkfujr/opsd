package control

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"time"
)

func (s *Server) ListScheduledTasks(w http.ResponseWriter, r *http.Request, _ httpapi.ListScheduledTasksParams) {
	nodes, e := s.nodes(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	out := httpapi.ScheduledTaskList{Items: []httpapi.ScheduledTaskItem{}, Unavailable: []httpapi.UnavailableNode{}, Nodes: []httpapi.ResourceNodeRead{}}
	for _, node := range nodes {
		if node.RevokedAt != nil || r.URL.Query().Get("node_id") != "" && r.URL.Query().Get("node_id") != node.Id {
			continue
		}
		var tasks []cluster.ScheduledTask
		read := s.node.CurrentRead(r.Context(), node.Id, "tasks.list", nil, &tasks, 5*time.Second)
		out.Nodes = append(out.Nodes, httpapi.ResourceNodeRead{NodeId: node.Id, NodeName: node.Name, Read: convertHTTP[httpapi.ReadStatus](read)})
		if read.Error != nil {
			out.Unavailable = append(out.Unavailable, httpapi.UnavailableNode{NodeId: node.Id, NodeName: node.Name, Message: read.Error.Message})
		}
		for _, task := range tasks {
			item := convertHTTP[httpapi.ScheduledTaskItem](task)
			item.NodeId = node.Id
			item.NodeName = node.Name
			out.Items = append(out.Items, item)
		}
	}
	write(w, 200, out)
}
func (s *Server) SaveScheduledTask(w http.ResponseWriter, r *http.Request, id string, _ httpapi.SaveScheduledTaskParams) {
	var body httpapi.ScheduledTask
	if !s.body(w, r, &body) {
		return
	}
	s.submit(w, r, id, "task_save", convertHTTP[cluster.ScheduledTask](body), r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) ExecuteScheduledTask(w http.ResponseWriter, r *http.Request, id, taskId string, _ httpapi.ExecuteScheduledTaskParams) {
	input := struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}{taskId, r.Header.Get("Idempotency-Key")}
	var op cluster.Operation
	e := s.node.Call(r.Context(), id, "task.run", input, &op)
	s.resultError(w, r, 202, op, e)
}
func (s *Server) ListScheduledTaskRuns(w http.ResponseWriter, r *http.Request, id, taskId string) {
	var items []cluster.Operation
	e := s.node.Call(r.Context(), id, "task.runs", taskId, &items)
	s.resultError(w, r, 200, httpapi.ScheduledTaskRuns{Items: convertHTTP[[]httpapi.Operation](items)}, e)
}
func (s *Server) ReadExecutionLogs(w http.ResponseWriter, r *http.Request, id, operationId string, p httpapi.ReadExecutionLogsParams) {
	input := protocol.ExecutionLogsRead{OperationID: operationId, Limit: 65536}
	if p.Offset != nil {
		input.Offset = int64(*p.Offset)
	}
	if p.Limit != nil {
		input.Limit = *p.Limit
	}
	if input.Offset < 0 || input.Limit < 1 || input.Limit > 65536 {
		s.fail(w, r, 422, "invalid_request", "日志范围无效")
		return
	}
	var logs protocol.ExecutionLogs
	e := s.node.Call(r.Context(), id, "execution.logs", input, &logs)
	s.resultError(w, r, 200, logs, e)
}
