package operations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestResourceAdmissionSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executor.db")
	db, err := storage.Open(t.Context(), path, storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db}
	action := func(id string) protocol.OperationAction {
		return protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: strings.Repeat(id, 64)}, Action: "restart", ProjectKnown: true}}
	}
	first, _, err := store.Receive(t.Context(), "one", protocol.ContractVersion, action("a"), "boot")
	if err != nil {
		t.Fatal(err)
	}
	first.Record.State = protocol.AttentionRequired
	if err = store.Save(t.Context(), &first); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.Open(t.Context(), path, storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.DB = db
	if _, created, err := store.Receive(t.Context(), "two", protocol.ContractVersion, action("b"), "boot"); err != nil || !created {
		t.Fatal("无冲突操作未独立受理", created, err)
	}
	if _, _, err := store.Receive(t.Context(), "same", protocol.ContractVersion, action("a"), "boot"); !errors.Is(err, ErrBusy) {
		t.Fatal("待处理操作丢失资源责任", err)
	}
	first.Record.State = protocol.Succeeded
	if err = store.Save(t.Context(), &first); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.Receive(t.Context(), "same", protocol.ContractVersion, action("a"), "boot"); err != nil || !created {
		t.Fatal("终结后未释放资源", created, err)
	}
}

func TestResourceLocksAllowSeparateContainers(t *testing.T) {
	directory := t.TempDir()
	action := protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: "one"}, ProjectKnown: true}}
	unlock, err := LockResources(directory, action)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	other := action
	copy := *action.Container
	copy.ContainerID = "two"
	other.Container = &copy
	release, err := LockResources(directory, other)
	if err != nil {
		t.Fatal("独立容器发生锁冲突", err)
	}
	release()
	if release, err := LockResources(directory, action); err == nil {
		release()
		t.Fatal("同一容器重复取得锁")
	}
}
