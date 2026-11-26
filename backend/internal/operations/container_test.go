package operations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type containerRuntime struct {
	current     protocol.ContainerDetail
	writes      int
	lost        bool
	interrupted func()
	inspectErr  error
}

func (c *containerRuntime) Inspect(context.Context, protocol.ContainerRef) (protocol.ContainerDetail, error) {
	c.current.CheckedAt = time.Now().UTC()
	return c.current, c.inspectErr
}
func (c *containerRuntime) Change(_ context.Context, a protocol.ContainerAction) error {
	c.writes++
	switch a.Action {
	case "stop", "kill", "remove":
		c.current.State = "exited"
	default:
		c.current.State = "running"
		at := c.current.StartedAt.Add(time.Second)
		c.current.StartedAt = &at
	}
	if c.interrupted != nil {
		c.interrupted()
	}
	if c.lost {
		return errors.New("Docker 应答丢失")
	}
	return nil
}

func TestContainerDestructiveActionReadback(t *testing.T) {
	missing := &dockerengine.Fault{Code: "not_found", Message: "原容器已移除"}
	wrongEngine := &dockerengine.Fault{Code: "conflict", Message: "Docker Engine 身份已变化"}
	for _, c := range []struct {
		name, action, state string
		before, after       error
		want                protocol.OperationState
		writes              int
	}{
		{"原容器在发送前已消失", "remove", "exited", missing, nil, protocol.Rejected, 0},
		{"拒绝删除运行容器", "remove", "running", nil, nil, protocol.Rejected, 0},
		{"删除丢失应答后由原 ID 缺失确认", "remove", "exited", nil, missing, protocol.Succeeded, 1},
		{"删除后换了 Engine", "remove", "exited", nil, wrongEngine, protocol.AttentionRequired, 1},
		{"删除后仍读到原容器", "remove", "exited", nil, nil, protocol.AttentionRequired, 1},
		{"强制停止后原容器仍在且已停止", "kill", "running", nil, nil, protocol.Succeeded, 1},
		{"强制停止后原容器消失", "kill", "running", nil, missing, protocol.AttentionRequired, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _, _ := operationFixture(t)
			runtime := &containerRuntime{current: protocol.ContainerDetail{ContainerSummary: protocol.ContainerSummary{
				ContainerRef: protocol.ContainerRef{EngineID: "本机 Docker", ContainerID: strings.Repeat("a", 64)}, Name: "测试容器", State: c.state,
			}}, inspectErr: c.before, lost: true}
			runtime.interrupted = func() { runtime.inspectErr = c.after }
			e.Containers = runtime
			action := protocol.ContainerAction{ContainerRef: runtime.current.ContainerRef, Action: c.action, ExpectedRevision: protocol.ContainerRevision(runtime.current.ContainerSummary)}
			accept(t, e, "container-action", protocol.OperationAction{Kind: "container", Container: &action})
			if err := e.Run(t.Context(), "container-action"); err != nil {
				t.Fatal(err)
			}
			local, err := e.Store.Get(t.Context(), "container-action")
			if err != nil || local.Record.State != c.want || runtime.writes != c.writes {
				t.Fatal(local.Record, err, runtime.writes)
			}
			if c.action == "remove" && c.want == protocol.AttentionRequired {
				runtime.inspectErr = missing
				if _, err = e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
					t.Fatal(err)
				}
				if err = e.Run(t.Context(), local.Record.ID); err != nil {
					t.Fatal(err)
				}
				local, err = e.Store.Get(t.Context(), local.Record.ID)
				if err != nil || local.Record.State != protocol.Succeeded || runtime.writes != 1 {
					t.Fatal("读回原操作时重发了删除或未确认缺失", local.Record, err, runtime.writes)
				}
			}
		})
	}
}
func TestContainerRestartUnknownNeverRepeats(t *testing.T) {
	e, _, _ := operationFixture(t)
	at := time.Now().Add(-time.Minute).UTC()
	runtime := &containerRuntime{current: protocol.ContainerDetail{ContainerSummary: protocol.ContainerSummary{ContainerRef: protocol.ContainerRef{EngineID: "本机 Docker", ContainerID: strings.Repeat("a", 64)}, Name: "测试容器", State: "running"}, StartedAt: &at}, lost: true}
	e.Containers = runtime
	action := protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: runtime.current.ContainerRef, Action: "restart"}}
	accept(t, e, "container-restart", action)
	if err := e.Run(t.Context(), "container-restart"); err != nil {
		t.Fatal(err)
	}
	local, err := e.Store.Get(t.Context(), "container-restart")
	if err != nil || local.Record.State != protocol.AttentionRequired || runtime.writes != 1 {
		t.Fatal(local, err, runtime.writes)
	}
	// 重新打开持久记录，并通过同一操作核对实际状态。
	var path string
	if err = e.Store.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	e.Store.DB.Close()
	db, err := storage.Open(t.Context(), path, storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.Store = Store{DB: db}
	e = &Engine{Store: e.Store, Clock: e.Clock, Containers: runtime}
	if _, err = e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err = e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatal(err)
	}
	local, _ = e.Store.Get(t.Context(), local.Record.ID)
	if runtime.writes != 1 || local.Record.State != protocol.AttentionRequired {
		t.Fatal("未知重启被重新执行或误报成功", local, runtime.writes)
	}
	if _, err = e.Store.Request(t.Context(), local.Record.ID, "resolve", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err = e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatal(err)
	}
	local, _ = e.Store.Get(t.Context(), local.Record.ID)
	if local.Record.State != protocol.ResolvedManually || runtime.writes != 1 {
		t.Fatal(local, runtime.writes)
	}
}
func TestContainerInterruptedStartReadsBackWithoutRepeating(t *testing.T) {
	e, _, _ := operationFixture(t)
	at := time.Now().Add(-time.Minute).UTC()
	runtime := &containerRuntime{current: protocol.ContainerDetail{ContainerSummary: protocol.ContainerSummary{ContainerRef: protocol.ContainerRef{EngineID: "本机 Docker", ContainerID: strings.Repeat("b", 64)}, Name: "测试容器", State: "exited"}, StartedAt: &at}}
	e.Containers = runtime
	action := protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: runtime.current.ContainerRef, Action: "start"}}
	accept(t, e, "container-start", action)
	ctx, cancel := context.WithCancel(t.Context())
	runtime.interrupted = cancel
	if err := e.Run(ctx, "container-start"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	local, err := e.Store.Get(t.Context(), "container-start")
	if err != nil || local.ContainerState == nil || !local.ContainerState.Attempted {
		t.Fatal(local, err)
	}
	runtime.interrupted = nil
	if err = e.Run(t.Context(), "container-start"); err != nil {
		t.Fatal(err)
	}
	local, _ = e.Store.Get(t.Context(), "container-start")
	if local.Record.State != protocol.Succeeded || runtime.writes != 1 {
		t.Fatal(local, runtime.writes)
	}
	_, created, err := e.Store.Receive(t.Context(), "container-start", protocol.ContractVersion, action, "启动一")
	if err != nil || created || runtime.writes != 1 {
		t.Fatal("重复请求改变了结果", created, err)
	}
}
