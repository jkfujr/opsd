package cluster

import (
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

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
