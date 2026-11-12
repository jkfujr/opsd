package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/robfig/cron/v3"
	"strings"
	"time"
	_ "time/tzdata"
)

type ScheduledTask struct {
	ID        string     `json:"id"`
	Revision  uint64     `json:"revision"`
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"`
	Timezone  string     `json:"timezone"`
	Script    string     `json:"script"`
	Directory string     `json:"directory"`
	Enabled   bool       `json:"enabled"`
	NextAt    *time.Time `json:"next_at,omitempty"`
}

func (t ScheduledTask) schedule() (cron.Schedule, error) {
	if t.Timezone == "" || !operations.ValidID(t.ID) || len(strings.Fields(t.Schedule)) != 5 || strings.ContainsAny(t.Timezone, " \t\r\n") {
		return nil, fault("invalid_request", "任务标识、五字段计划或时区无效")
	}
	if _, e := time.LoadLocation(t.Timezone); e != nil {
		return nil, fault("invalid_request", "时区无效")
	}
	if err := t.action().Validate(); err != nil {
		return nil, fault("invalid_request", err.Error())
	}
	schedule, e := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse("CRON_TZ=" + t.Timezone + " " + t.Schedule)
	if e != nil || schedule.Next(time.Now()).IsZero() {
		return nil, fault("invalid_request", "五字段计划无效或五年内没有执行时间")
	}
	return schedule, nil
}
func (t ScheduledTask) action() protocol.ShellAction {
	return protocol.ShellAction{TaskID: t.ID, Name: t.Name, Script: t.Script, Directory: t.Directory}
}
func (n *Node) Task(ctx context.Context, id string) (ScheduledTask, error) {
	var body string
	var task ScheduledTask
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM scheduled_tasks WHERE id=?", id).Scan(&body)
	if e == nil {
		e = json.Unmarshal([]byte(body), &task)
	}
	return task, e
}
func (n *Node) Tasks(ctx context.Context) ([]ScheduledTask, error) {
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM scheduled_tasks ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ScheduledTask{}
	for rows.Next() {
		var body string
		var task ScheduledTask
		if e = rows.Scan(&body); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(body), &task); e != nil {
			return nil, e
		}
		if task.Enabled {
			if schedule, e := task.schedule(); e == nil {
				at := schedule.Next(n.Now())
				task.NextAt = &at
			}
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (n *Node) taskDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	switch method {
	case "tasks.list":
		return n.Tasks(ctx)
	case "task.get":
		var id string
		if e := json.Unmarshal(body, &id); e != nil {
			return nil, e
		}
		return n.Task(ctx, id)
	case "task.run":
		var input struct {
			ID               string `json:"id"`
			Key              string `json:"key"`
			Actor            string `json:"actor,omitempty"`
			Digest           string `json:"digest,omitempty"`
			ExpectedRevision uint64 `json:"expected_revision,omitempty"`
		}
		if e := json.Unmarshal(body, &input); e != nil {
			return nil, e
		}
		if input.Actor != "" {
			origin = input.Actor
		}
		return n.runTaskDigest(ctx, origin, input.ID, input.Key, input.Digest, input.ExpectedRevision)
	case "task.runs":
		var id string
		if e := json.Unmarshal(body, &id); e != nil {
			return nil, e
		}
		rows, e := n.DB.QueryContext(ctx, "SELECT body FROM queue WHERE kind='task_run' AND json_extract(body,'$.resource_id')=? ORDER BY seq DESC", id)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []Operation{}
		for rows.Next() {
			var b string
			var op Operation
			if e = rows.Scan(&b); e != nil {
				return nil, e
			}
			if e = json.Unmarshal([]byte(b), &op); e != nil {
				return nil, e
			}
			out = append(out, op)
		}
		return out, rows.Err()
	case "execution.logs":
		var input protocol.ExecutionLogsRead
		if e := json.Unmarshal(body, &input); e != nil {
			return nil, e
		}
		if operationOwner(input.OperationID) != n.ID {
			return nil, fault("forbidden", "日志不属于本机")
		}
		op, err := n.operation(ctx, input.OperationID)
		if err != nil {
			return nil, err
		}
		supported := op.Subject == "task_run" || op.Subject == "container" || op.Subject == "compose"
		if op.SupportsLogs != nil {
			supported = *op.SupportsLogs
		}
		if !supported {
			return nil, fault("logs_unsupported", "此操作不支持执行日志")
		}
		if op.Executor == "node" && op.State == protocol.Accepted {
			return protocol.ExecutionLogs{Data: []byte{}, Offset: input.Offset, NextOffset: input.Offset}, nil
		}
		result, e := n.executeRead(ctx, &input)
		if e != nil {
			if firewall.Code(e) == "logs_unsupported" {
				return nil, fault("logs_unsupported", "此操作不支持执行日志")
			}
			return nil, fault("logs_read_failed", "执行日志读取失败")
		}
		if p, ok := result.(*protocol.ExecutionLogsResult); ok {
			return p.Logs, nil
		}
		return nil, fmt.Errorf("日志应答无效")
	}
	return nil, fault("invalid_request", "任务操作无效")
}
func (n *Node) runTask(ctx context.Context, origin, id, key string) (Operation, error) {
	return n.runTaskDigest(ctx, origin, id, key, "")
}
func (n *Node) runTaskDigest(ctx context.Context, origin, id, key, digest string, expected ...uint64) (Operation, error) {
	if key == "" || len(key) > 128 {
		return Operation{}, fault("invalid_request", "缺少有效请求键")
	}
	var existing string
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM queue WHERE origin=? AND request_key=?", origin, key).Scan(&existing)
	if e == nil {
		if digest != "" {
			_, e := n.requestReceipt(ctx, requeststate.Write{Actor: origin, Key: key, Digest: digest})
			if e != nil {
				return Operation{}, e
			}
		}
		var op Operation
		if e = json.Unmarshal([]byte(existing), &op); e != nil {
			return op, e
		}
		if op.Subject != "task_run" || op.ResourceID != id {
			return Operation{}, fault("conflict", "同一请求不能用于其他任务")
		}
		return op, nil
	}
	if e != sql.ErrNoRows {
		return Operation{}, e
	}
	task, e := n.Task(ctx, id)
	if e != nil {
		return Operation{}, e
	}
	action := task.action()
	if len(expected) > 0 && expected[0] > 0 {
		if task.Revision != expected[0] {
			return Operation{}, fault("conflict", "任务定义已经改变，请重新读取")
		}
		action.ExpectedRevision = expected[0]
	}
	body, _ := json.Marshal(action)
	return n.enqueue(ctx, origin, Request{Actor: origin, InputDigest: digest, Kind: "task_run", ResourceID: task.ID, Key: key, Payload: body})
}

func (n *Node) applyTask(ctx context.Context, plain []byte, op *Operation) error {
	var task ScheduledTask
	if e := json.Unmarshal(plain, &task); e != nil {
		return e
	}
	if _, e := task.schedule(); e != nil {
		return e
	}
	task.NextAt = nil
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision uint64
	e = tx.QueryRowContext(ctx, "SELECT revision FROM scheduled_tasks WHERE id=?", task.ID).Scan(&revision)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if task.Revision != revision {
		return fault("conflict", "任务已改变，请重新读取")
	}
	if e == sql.ErrNoRows {
		var count int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM scheduled_tasks").Scan(&count); e != nil {
			return e
		}
		if count >= 1000 {
			return fault("node_busy", "任务数量已达上限")
		}
	}
	task.Revision++
	body, _ := json.Marshal(task)
	if _, e = tx.ExecContext(ctx, "INSERT INTO scheduled_tasks(id,revision,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,body=excluded.body", task.ID, task.Revision, string(body)); e != nil {
		return e
	}
	op.State = protocol.Succeeded
	op.Revision++
	op.UpdatedAt = n.Now().UTC()
	op.ResourceID = task.ID
	value, _ := json.Marshal(op)
	if _, e = tx.ExecContext(ctx, "UPDATE queue SET body=?,phase='finished',terminal=1 WHERE id=? AND terminal=0", string(value), op.ID); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		select {
		case n.taskWake <- struct{}{}:
		default:
		}
		n.notifyChange(Change{Resource: "tasks", NodeID: n.ID})
		n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
	}
	return e
}

func (n *Node) taskLoop() {
	runner := cron.New()
	runner.Start()
	defer runner.Stop()
	type entry struct {
		revision uint64
		id       cron.EntryID
	}
	registered := map[string]entry{}
	for {
		tasks, e := n.Tasks(n.ctx)
		var retry <-chan time.Time
		if e != nil {
			retry = time.After(5 * time.Second)
		}
		if e == nil {
			seen := map[string]bool{}
			for _, task := range tasks {
				seen[task.ID] = true
				old, exists := registered[task.ID]
				if exists && old.revision == task.Revision {
					continue
				}
				if exists {
					runner.Remove(old.id)
					delete(registered, task.ID)
				}
				if !task.Enabled {
					continue
				}
				schedule, err := task.schedule()
				if err != nil {
					continue
				}
				id := task.ID
				entryID := runner.Schedule(schedule, cron.FuncJob(func() {
					current, err := n.Task(n.ctx, id)
					if err != nil || !current.Enabled || current.Revision != task.Revision {
						return
					}
					key := "scheduled:" + id + ":" + n.Now().UTC().Truncate(time.Minute).Format("20060102T1504Z")
					_, _ = n.runTask(n.ctx, n.ID, id, key)
				}))
				registered[id] = entry{revision: task.Revision, id: entryID}
			}
			for id, old := range registered {
				if !seen[id] {
					runner.Remove(old.id)
					delete(registered, id)
				}
			}
		}
		select {
		case <-n.ctx.Done():
			<-runner.Stop().Done()
			return
		case <-n.taskWake:
		case <-retry:
		}
	}
}
func (n *Node) taskQueueLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		rows, e := n.DB.QueryContext(n.ctx, "SELECT id FROM queue WHERE terminal=0 AND kind='task_run' ORDER BY seq LIMIT 16")
		var ids []string
		if e == nil {
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
		for _, id := range ids {
			n.advanceFiltered("id=? AND kind='task_run'", id)
		}
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) cancelTask(ctx context.Context, id string) (Operation, error) {
	op, e := n.operation(ctx, id)
	if e != nil {
		return op, e
	}
	if op.Subject != "task_run" || operations.Terminal(op.State) {
		return op, fault("conflict", "任务已结束或不支持取消")
	}
	if cancelled, err := n.cancelQueued(ctx, id); err == nil {
		return cancelled, nil
	}
	response, e := n.executeRead(ctx, &protocol.OperationQuery{OperationID: id})
	if e != nil {
		return op, e
	}
	query, ok := response.(*protocol.OperationQueryResult)
	if !ok || !query.Found || query.Record == nil {
		return op, fault("conflict", "任务受理结果待确认，请重新读取")
	}
	if operations.Terminal(query.Record.State) {
		return n.GetOperation(ctx, id)
	}
	_, e = n.executeRead(ctx, &protocol.OperationCancel{OperationID: id, ExpectedRevision: query.Record.Revision})
	return op, e
}
