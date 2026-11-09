package operations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type containerRuntime struct {
	current     protocol.ContainerDetail
	writes      int
	lost        bool
	interrupted func()
}

func (c *containerRuntime) Inspect(context.Context, protocol.ContainerRef) (protocol.ContainerDetail, error) {
	c.current.CheckedAt = time.Now().UTC()
	return c.current, nil
}
func (c *containerRuntime) Change(_ context.Context, a protocol.ContainerAction) error {
	c.writes++
	if a.Action == "stop" {
		c.current.State = "exited"
	} else {
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
