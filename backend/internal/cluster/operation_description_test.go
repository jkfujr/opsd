package cluster

import (
	"encoding/json"
	"testing"
)

func TestTaskHistoryKeepsAcceptedNameAcrossRename(t *testing.T) {
	n, _ := openTestNode(t)
	old := ScheduledTask{ID: "task", Name: "原任务", Schedule: "0 * * * *", Timezone: "UTC", Script: "true", Directory: "/", Revision: 1}
	body, _ := json.Marshal(old)
	if _, err := n.DB.Exec("INSERT INTO scheduled_tasks(id,revision,body) VALUES(?,?,?)", old.ID, old.Revision, string(body)); err != nil {
		t.Fatal(err)
	}
	changed := old
	changed.Name = "新任务"
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "rename-task-description", Kind: "task_save", ResourceID: old.ID, Payload: payload(changed)})
	if err != nil {
		t.Fatal(err)
	}
	if op.Action != "save" || op.TargetName != "原任务" || op.Resource == nil || op.Resource.ID != old.ID || op.SupportsLogs == nil || *op.SupportsLogs {
		t.Fatal(op)
	}
	n.advance()
	read, err := n.GetOperation(t.Context(), op.ID)
	if err != nil || read.TargetName != "原任务" {
		t.Fatal("资源改名改变了历史名称", read, err)
	}
	if _, err = n.DB.Exec("DELETE FROM scheduled_tasks WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	read, err = n.GetOperation(t.Context(), op.ID)
	if err != nil || read.TargetName != "原任务" || read.Resource.ID != old.ID {
		t.Fatal("资源删除改变了历史引用", read, err)
	}
}
