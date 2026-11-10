package operations

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestShellInterruptedIntentNeverRunsAgain(t *testing.T) {
	e, _, _ := operationFixture(t)
	e.LogDirectory = t.TempDir()
	marker := filepath.Join(t.TempDir(), "unexpected")
	a := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: "once", Name: "只执行一次", Script: "touch " + marker, Directory: "/tmp"}}
	l := accept(t, e, "shell-interrupted", a)
	zero := 0
	l.IntentStep = &zero
	l.Record.State = protocol.Applying
	if err := e.Store.Save(t.Context(), &l); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(t.Context(), l.Record.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = e.Store.Get(t.Context(), l.Record.ID)
	if l.Record.State != protocol.AttentionRequired {
		t.Fatal(l.Record)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("恢复时重放了脚本")
	}
	if _, err := e.Store.Request(t.Context(), l.Record.ID, "cancel", l.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(t.Context(), l.Record.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = e.Store.Get(t.Context(), l.Record.ID)
	if l.Record.State != protocol.Cancelled {
		t.Fatal(l.Record)
	}
}

func TestTaskAdmissionIsIndependentAndPreventsOverlap(t *testing.T) {
	e, _, _ := operationFixture(t)
	for _, id := range []string{"first", "second"} {
		a := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: id, Name: id, Script: "true", Directory: "/tmp"}}
		accept(t, e, "task-"+id, a)
		if _, created, err := e.Store.Receive(t.Context(), "duplicate-"+id, protocol.ContractVersion, a, "启动一"); !errors.Is(err, ErrBusy) || created {
			t.Fatal("同任务允许重入", created, err)
		}
	}
	runtime := &containerRuntime{current: protocol.ContainerDetail{ContainerSummary: protocol.ContainerSummary{ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: strings.Repeat("a", 64)}, State: "exited"}, CheckedAt: time.Now()}}
	at := time.Now().Add(-time.Minute)
	runtime.current.StartedAt = &at
	e.Containers = runtime
	accept(t, e, "ordinary-with-tasks", protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: runtime.current.ContainerRef, Action: "start"}})
	if err := e.Run(t.Context(), "ordinary-with-tasks"); err != nil {
		t.Fatal(err)
	}
	l, _ := e.Store.Get(t.Context(), "ordinary-with-tasks")
	if l.Record.State != protocol.Succeeded {
		t.Fatal("任务阻塞普通操作", l.Record)
	}
}

type updateRuntime struct {
	containerRuntime
	writes int
	goal   bool
}

func (r *updateRuntime) Update(_ context.Context, _ protocol.ContainerRef, id string, s *dockerengine.UpdateState, persist func() error, _ io.Writer) error {
	r.writes++
	s.OperationID = id
	s.Stage = "create"
	s.Intent = "start"
	if err := persist(); err != nil {
		return err
	}
	return errors.New("更新应答丢失")
}
func (r *updateRuntime) CheckUpdate(context.Context, protocol.ContainerRef, *dockerengine.UpdateState) (bool, error) {
	return r.goal, nil
}
func TestContainerUpdateUnknownOnlyReadsOriginal(t *testing.T) {
	e, _, _ := operationFixture(t)
	e.LogDirectory = t.TempDir()
	r := &updateRuntime{}
	e.Containers = r
	a := protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: strings.Repeat("a", 64)}, Action: "update"}}
	accept(t, e, "update-unknown", a)
	for range 2 {
		if err := e.Run(t.Context(), "update-unknown"); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := e.Store.Get(t.Context(), "update-unknown")
	if r.writes != 1 || l.Record.State != protocol.AttentionRequired {
		t.Fatal("未知更新被重复执行", r.writes, l.Record)
	}
	if _, err := e.Store.Request(t.Context(), l.Record.ID, "recover", l.Record.Revision); err != nil {
		t.Fatal(err)
	}
	r.goal = true
	if err := e.Run(t.Context(), l.Record.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = e.Store.Get(t.Context(), l.Record.ID)
	if r.writes != 1 || l.Record.State != protocol.Succeeded {
		t.Fatal(r.writes, l.Record)
	}
}

type projectRuntime struct {
	containerRuntime
	project protocol.ComposeProject
	writes  int
}

func (r *projectRuntime) Projects(context.Context) ([]protocol.ComposeProject, error) {
	return []protocol.ComposeProject{r.project}, nil
}
func (r *projectRuntime) ChangeCompose(context.Context, protocol.ComposeAction, io.Writer) error {
	r.writes++
	return errors.New("重建应答丢失")
}
func TestComposeUnknownDoesNotRepeatOrAcceptOldContainer(t *testing.T) {
	e, _, _ := operationFixture(t)
	e.LogDirectory = t.TempDir()
	ref := protocol.ComposeRef{ID: strings.Repeat("a", 64), EngineID: "engine", Name: "project", Directory: "/tmp", Files: []string{"/tmp/compose.yaml"}, EnvironmentFiles: []string{}}
	r := &projectRuntime{project: protocol.ComposeProject{ComposeRef: ref, Services: []protocol.ContainerSummary{{ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: strings.Repeat("b", 64)}, State: "running"}}}}
	e.Containers = r
	accept(t, e, "compose-unknown", protocol.OperationAction{Kind: "compose", Compose: &protocol.ComposeAction{Project: ref, Action: "recreate"}})
	for range 2 {
		if err := e.Run(t.Context(), "compose-unknown"); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := e.Store.Get(t.Context(), "compose-unknown")
	if r.writes != 1 || l.Record.State != protocol.AttentionRequired {
		t.Fatal("未知重建重复执行或误认旧容器", r.writes, l.Record)
	}
	if _, err := e.Store.Request(t.Context(), l.Record.ID, "recover", l.Record.Revision); err != nil {
		t.Fatal(err)
	}
	r.project.Services[0].ContainerID = strings.Repeat("c", 64)
	if err := e.Run(t.Context(), l.Record.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = e.Store.Get(t.Context(), l.Record.ID)
	if r.writes != 1 || l.Record.State != protocol.Succeeded {
		t.Fatal(r.writes, l.Record)
	}
}
