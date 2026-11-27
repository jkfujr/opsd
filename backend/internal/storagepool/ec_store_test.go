package storagepool

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestECRestoreCapacityFailureRetainsEncodedLayout(t *testing.T) {
	ctx := t.Context()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "pools.db"), storage.Pools)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, now := Store{DB: db}, time.Now()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(store.Begin(ctx, protocol.PoolPlan{PoolID: "pool", OwnerNodeID: "node", Normalized: protocol.PoolInput{Name: "测试池", ECEnabled: true}}, "create", "", now))
	w := ECWork{PoolID: "pool", Volume: 7, Operation: "encode", Generation: 1, Direction: "encode",
		Plan: ECPlan{Limit: 100, Bytes: 98, Input: protocol.PoolECWorkerInput{Generation: 1}}}
	check(store.BeginEC(ctx, w, now))
	w.State = "verifying"
	check(store.SaveEC(ctx, w, now))
	check(store.CommitEC(ctx, w, now))
	w.State, w.Cutover = "done", true
	check(store.SaveEC(ctx, w, now))
	settings := protocol.PoolECSettings{ID: "pool", Revision: 1, RestoreReplicas: true}
	check(store.SetECPolicy(ctx, settings, "restore-request"))
	w.Generation, w.Operation, w.Direction, w.Cutover = 2, "restore", "restore", false
	w.Plan.Input.Restore = []string{"addr-a", "addr-b"}
	check(store.BeginEC(ctx, w, now))
	w.State = "verifying"
	check(store.SaveEC(ctx, w, now))
	check(store.FailECRestore(ctx, w.PoolID, &w, "目标普通卷槽位不足", true, now))
	check(store.SetECPolicy(ctx, settings, "restore-request"))
	pending, message, err := store.ECRestoreResult(ctx, w.PoolID)
	check(err)
	if pending || message != "目标普通卷槽位不足" {
		t.Fatal(pending, message)
	}
	active, err := store.ActiveEC(ctx, w.PoolID)
	check(err)
	encoded, err := store.EncodedEC(ctx, w.PoolID)
	check(err)
	status, err := store.ECStatus(ctx, w.PoolID)
	check(err)
	if active.Operation != "" || len(encoded) != 1 || encoded[0].Generation != 1 ||
		!slices.Equal(encoded[0].Plan.Input.Restore, w.Plan.Input.Restore) || len(status) != 1 ||
		status[0].Layout != "ec" || status[0].CleanupPending || status[0].Conversion != "failed" {
		t.Fatal(active, encoded, status)
	}
}
