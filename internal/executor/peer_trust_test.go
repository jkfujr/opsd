package executor

import (
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestTrustTargetPersistsAndRejectsOldOrConflictingVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executor.db")
	db, e := storage.Open(t.Context(), path, storage.Executor)
	if e != nil {
		t.Fatal(e)
	}
	s := NewServices(db)
	target := protocol.PeerTrustTarget{Revision: 2, Enabled: true, IPv4: []string{"8.8.8.8"}, IPv6: []string{}}
	if _, e = s.Handle(t.Context(), &target); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = storage.Open(t.Context(), path, storage.Executor)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s = NewServices(db)
	if _, e = s.Handle(t.Context(), &target); e != nil {
		t.Fatal("重启后相同目标被拒绝", e)
	}
	old := protocol.PeerTrustTarget{Revision: 1}
	if _, e = s.Handle(t.Context(), &old); e != nil {
		t.Fatal(e)
	}
	changed := target
	changed.Enabled, changed.IPv4 = false, []string{}
	if _, e = s.Handle(t.Context(), &changed); firewall.Code(e) != "conflict" {
		t.Fatal("同版本冲突未拒绝", e)
	}
	stored, _, e := s.trustState(t.Context())
	if e != nil || stored.Revision != 2 || !stored.Enabled {
		t.Fatal("旧目标覆盖已保存目标", stored, e)
	}
	changed.Revision = 3
	if _, e = s.Handle(t.Context(), &changed); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Handle(t.Context(), &target); e != nil {
		t.Fatal(e)
	}
	stored, _, e = s.trustState(t.Context())
	if e != nil || stored.Enabled || stored.Revision != 3 {
		t.Fatal("迟到开启覆盖关闭", stored, e)
	}
}
