package executor

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

// 待人工核对的操作只有在存在有效人工请求时才允许被调度重新派发。
// 每个用例独立数据库：存储层保证同一时刻只有一个未结束的写操作。
func TestRecoverSchedulesAttentionOnlyWithManualRequest(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, store operations.Store, id string)
		want    bool
	}{
		{
			name:    "没有人工请求",
			prepare: func(*testing.T, operations.Store, string) {},
			want:    false,
		},
		{
			name: "有匹配的人工请求",
			prepare: func(t *testing.T, store operations.Store, id string) {
				local, err := store.Get(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Request(context.Background(), id, "recover", local.Record.Revision); err != nil {
					t.Fatal(err)
				}
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })

			started := []string{}
			services := &Services{
				Engine: &operations.Engine{Store: operations.Store{DB: db}},
				Start:  func(_ context.Context, id string) error { started = append(started, id); return nil },
			}
			store := operations.Store{DB: db}
			const id = "opawait"
			action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
				TaskID: "job-1", Name: "任务", Script: "true", Directory: "/tmp",
			}}
			if _, _, err := store.Receive(ctx, id, protocol.ContractVersion, action, ""); err != nil {
				t.Fatal(err)
			}
			local, err := store.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			local.Record.State = protocol.AttentionRequired
			if err := store.Save(ctx, &local); err != nil {
				t.Fatal(err)
			}
			c.prepare(t, store, id)

			if err := services.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			got := len(started) == 1 && started[0] == id
			if got != c.want {
				t.Fatalf("派发结果 %v，期望 %v（已派发：%v）", got, c.want, started)
			}
		})
	}
}

// 请求版本与记录版本不一致时，视为已过期，不能自动推进。
func TestManualRequestRejectsStaleRevision(t *testing.T) {
	cases := []struct {
		name string
		local operations.Local
		want bool
	}{
		{"无请求", operations.Local{}, false},
		{"匹配的 recover", operations.Local{Request: "recover", RequestRevision: 3, Record: protocol.OperationRecord{Revision: 3}}, true},
		{"匹配的 resolve", operations.Local{Request: "resolve", RequestRevision: 5, Record: protocol.OperationRecord{Revision: 5}}, true},
		{"匹配的 cancel", operations.Local{Request: "cancel", RequestRevision: 2, Record: protocol.OperationRecord{Revision: 2}}, true},
		{"过期的 recover", operations.Local{Request: "recover", RequestRevision: 2, Record: protocol.OperationRecord{Revision: 3}}, false},
		{"未知请求", operations.Local{Request: "confirm", RequestRevision: 1, Record: protocol.OperationRecord{Revision: 1}}, false},
	}
	for _, c := range cases {
		if got := manualRequest(c.local); got != c.want {
			t.Errorf("%s：得到 %v，期望 %v", c.name, got, c.want)
		}
	}
}
