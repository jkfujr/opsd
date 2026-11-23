package control

import (
	"encoding/base64"
	"errors"
	"encoding/json"
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
	out := httpapi.ScheduledTaskList{Items: []httpapi.ScheduledTaskMetadata{}, Unavailable: []httpapi.UnavailableNode{}, Nodes: []httpapi.ResourceNodeRead{}}
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
			item := convertHTTP[httpapi.ScheduledTaskMetadata](task)
			item.NodeId = &node.Id
			item.NodeName = &node.Name
			out.Items = append(out.Items, item)
		}
	}
	write(w, 200, out)
}
func (s *Server) SaveScheduledTask(w http.ResponseWriter, r *http.Request, id, taskID string, _ httpapi.SaveScheduledTaskParams) {
	var body httpapi.ScheduledTaskDefinition
	if !s.body(w, r, &body) {
		return
	}
	task := convertHTTP[cluster.ScheduledTask](body)
	task.ID = taskID
	task.Revision = expectedRevision(r)
	s.submit(w, r, id, "task_save", task, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) ExecuteScheduledTask(w http.ResponseWriter, r *http.Request, id, taskId string, _ httpapi.ExecuteScheduledTaskParams) {
	var body httpapi.ExecuteScheduledTaskJSONRequestBody
	if !s.body(w, r, &body) {
		return
	}
	input := struct {
		ID               string `json:"id"`
		Key              string `json:"key"`
		Actor            string `json:"actor"`
		Digest           string `json:"digest"`
		ExpectedRevision uint64 `json:"expected_revision"`
	}{taskId, r.Header.Get("Idempotency-Key"), actor(r), requestFingerprint(r), uint64(body.ExpectedRevision)}
	var op cluster.Operation
	e := s.node.Call(r.Context(), id, "task.run", input, &op)
	if e == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, 202, op, e)
}
func (s *Server) ListScheduledTaskRuns(w http.ResponseWriter, r *http.Request, id, taskId string, p httpapi.ListScheduledTaskRunsParams) {
	q := cluster.TaskRunsQuery{TaskID: taskId, Limit: 50}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}
	cursor, e := decodeCursor(r)
	if e != nil {
		s.fail(w, r, 400, "malformed_request", e.Error())
		return
	}
	if r.URL.Query().Get("cursor") != "" {
		body, err := base64.RawURLEncoding.DecodeString(cursor.Position)
		if err != nil || len(body) > 16000 || json.Unmarshal(body, &q.Before) != nil || q.Before == nil || q.Before.CreatedAt.IsZero() || cluster.OperationOwner(q.Before.ID) != id {
			s.fail(w, r, 400, "malformed_request", "分页位置无效")
			return
		}
	}
	var list cluster.OperationList
	e = s.node.Call(r.Context(), id, "task.runs.page", q, &list)
	if legacyNeeded(e) {
		// 目标节点是旧版本：它按 seq 倒序返回全部记录，没有分页能力。
		// 已带新游标的请求说明客户端先看到了分页应答，此时让客户端重读首页，
		// 避免把首页当成下一页追加。
		if r.URL.Query().Get("cursor") != "" {
			s.fail(w, r, 400, "malformed_request", "目标节点不支持分页，请重新读取首页")
			return
		}
		var items []cluster.Operation
		e = s.node.Call(r.Context(), id, "task.runs", taskId, &items)
		if e == nil && len(items) > q.Limit {
			items = items[:q.Limit]
		}
		// 旧节点没有游标，不生成 next_cursor；界面退化为只显示最近一页。
		list = cluster.OperationList{Items: items}
	}
	out := httpapi.ScheduledTaskRuns{Items: convertHTTP[[]httpapi.Operation](list.Items)}
	if list.Next != nil {
		body, _ := json.Marshal(list.Next)
		cursor := encodeCursor(r, collectionCursor{Position: base64.RawURLEncoding.EncodeToString(body)})
		out.NextCursor = &cursor
	}
	s.resultError(w, r, 200, out, e)
}
func (s *Server) ReadExecutionLogs(w http.ResponseWriter, r *http.Request, operationId string, p httpapi.ReadExecutionLogsParams) {
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
	e := s.node.Call(r.Context(), cluster.OperationOwner(operationId), "execution.logs", input, &logs)
	s.resultError(w, r, 200, logs, e)
}

// legacyNeeded 判断目标节点是否不认识分页方法。
func legacyNeeded(err error) bool {
	var fault *cluster.Fault
	return errors.As(err, &fault) && fault.Code == "invalid_request" && fault.Message == "未知节点操作"
}
