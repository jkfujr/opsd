package cluster

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestTaskRunPagesFollowCreatedTime(t *testing.T) {
	n, _ := openTestNode(t)
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	insert := func(suffix, kind, task, owner string, at time.Time, sequence uint64) Operation {
		t.Helper()
		op := Operation{OperationRecord: protocol.OperationRecord{ID: n.ID + "_" + suffix, State: protocol.Succeeded, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}, NodeID: owner, Subject: kind, ResourceID: task, Sequence: sequence, CreatedAt: at, UpdatedAt: at, Executor: "node"}
		if _, err := n.DB.ExecContext(t.Context(), "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,phase,body,terminal) VALUES(?,?,?,'{}','',?,'finished',?,1)", op.ID, n.ID, kind, []byte("test"), string(payload(op))); err != nil {
			t.Fatal(err)
		}
		return op
	}
	want := []Operation{}
	for i := range 137 {
		index := i * 37 % 137
		at := base.Add(time.Duration(index/3) * time.Second)
		if index%3 == 1 {
			at = at.Add(time.Nanosecond)
		}
		want = append(want, insert(fmt.Sprintf("run-%03d", index), "task_run", "recording", n.ID, at, uint64(i+1)))
	}
	insert("saved", "task_save", "recording", n.ID, base.Add(time.Hour), 138)
	insert("other-task", "task_run", "other", n.ID, base.Add(time.Hour), 139)
	insert("other-owner", "task_run", "recording", "other-node", base.Add(time.Hour), 140)
	sort.Slice(want, func(i, j int) bool { return operationBefore(want[i], want[j]) })
	wantIDs := make([]string, len(want))
	for i, op := range want {
		wantIDs[i] = op.ID
	}
	q := TaskRunsQuery{TaskID: "recording", Limit: 100}
	ids := []string{}
	for pageIndex := range 3 {
		var page OperationList
		if err := n.Call(t.Context(), n.ID, "task.runs.page", q, &page); err != nil {
			t.Fatal(err)
		}
		if pageIndex == 0 {
			if len(page.Items) != 100 || page.Next == nil {
				t.Fatalf("首页未按上限分页：%+v", page)
			}
			insert("new-arrival", "task_run", "recording", n.ID, base.Add(2*time.Hour), 141)
		}
		for _, op := range page.Items {
			ids = append(ids, op.ID)
		}
		if page.Next == nil {
			break
		}
		q.Before = page.Next
	}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("时间、同时间 ID 或新增记录影响了已有翻页：%v，预期 %v", ids, wantIDs)
	}
	var empty OperationList
	if err := n.Call(t.Context(), n.ID, "task.runs.page", TaskRunsQuery{TaskID: "missing", Limit: 50}, &empty); err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.Next != nil {
		t.Fatalf("空记录未保留空集合：%+v %v", empty, err)
	}
	for _, q := range []TaskRunsQuery{
		{TaskID: "recording", Limit: 0},
		{TaskID: "recording", Limit: -1},
		{TaskID: "recording", Limit: 101},
		{Limit: 50},
		{TaskID: "recording", Limit: 50, Before: &OperationPosition{ID: want[0].ID}},
		{TaskID: "recording", Limit: 50, Before: &OperationPosition{CreatedAt: base}},
		{TaskID: "recording", Limit: 50, Before: &OperationPosition{CreatedAt: base, ID: "other-node_run"}},
	} {
		var page OperationList
		err := n.Call(t.Context(), n.ID, "task.runs.page", q, &page)
		var invalid *Fault
		if !errors.As(err, &invalid) || invalid.Code != "invalid_request" {
			t.Fatalf("非法分页条件被接受：%+v %v", q, err)
		}
	}
}

func TestTaskScheduleRevisionIdempotencyAndOverlap(t *testing.T) {
	n, _ := openTestNode(t)
	task := ScheduledTask{ID: "recording-move", Name: "录播移动", Schedule: "30 2 * * *", Timezone: "Asia/Shanghai", Script: "true", Directory: "/tmp"}
	schedule, err := task.schedule()
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 10, 18, 31, 0, 0, time.UTC)
	if next := schedule.Next(from); !next.Equal(time.Date(2026, 11, 11, 18, 30, 0, 0, time.UTC)) {
		t.Fatal("时区或下一次计划错误", next)
	}
	op, err := n.Submit(t.Context(), n.ID, Request{Kind: "task_save", Key: "save", Payload: payload(task)})
	if err != nil {
		t.Fatal(err)
	}
	n.advance()
	saved, err := n.Task(t.Context(), task.ID)
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	op, err = n.GetOperation(t.Context(), op.ID)
	if err != nil || op.State != protocol.Succeeded {
		t.Fatal(op, err)
	}
	a, err := n.runTask(t.Context(), n.ID, task.ID, "scheduled:recording-move:20261111T1830Z")
	if err != nil {
		t.Fatal(err)
	}
	b, err := n.runTask(t.Context(), n.ID, task.ID, "scheduled:recording-move:20261111T1830Z")
	if err != nil || a.ID != b.ID {
		t.Fatal("重复计划被再次受理", a, b, err)
	}
	c, err := n.runTask(t.Context(), n.ID, task.ID, "manual")
	if err != nil || c.State != protocol.Rejected || c.Error == nil || c.Error.Code != "task_busy" {
		t.Fatal("任务允许重入", c, err)
	}
	list, err := n.Tasks(t.Context())
	if err != nil || len(list) != 1 || list[0].NextAt != nil {
		t.Fatal(list, err)
	}
	if _, err = n.cancelTask(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if op, err = n.runTask(t.Context(), n.ID, task.ID, "after-cancel"); err != nil || op.State != protocol.Accepted {
		t.Fatal(op, err)
	}
	if !slices.Equal(defaultNavigation, []string{"overview", "nodes", "containers", "databases", "storage", "firewall", "tools", "settings"}) {
		t.Fatal(defaultNavigation)
	}
}
