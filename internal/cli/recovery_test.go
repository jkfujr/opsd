package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/opsd-labs/opsd/internal/account"
	"github.com/opsd-labs/opsd/internal/control"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/storage"
)

func TestRecoveryChangesCredentialsAndPreservesNodeIdentity(t *testing.T) {
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "control.db"), storage.Control)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	nodeKey, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Initialize(t.Context(), db, "admin", "old-password", "/old-entrance", identity.Encode(nodeKey.Public), "本机"); err != nil {
		t.Fatal(err)
	}
	key, err := control.LoadKey(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sessions(id,token_hash,csrf_token,created_at,expires_at,last_seen_at,remote_address) VALUES('session',X'01','csrf',0,9999999999,0,'127.0.0.1')"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRecovery(t.Context(), db, recoveryChange{Kind: "reset-password", Password: "new-password"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRecovery(t.Context(), db, recoveryChange{Kind: "reset-entrance", Entrance: "/new-entrance"}); err != nil {
		t.Fatal(err)
	}
	var hash, entrance, public string
	var active int
	if err := db.QueryRow("SELECT password_hash,entrance FROM account WHERE id=1").Scan(&hash, &entrance); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT public_key FROM nodes WHERE local=1").Scan(&public); err != nil {
		t.Fatal(err)
	}
	restored, err := control.LoadKey(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if !account.Verify(hash, "new-password") || account.Verify(hash, "old-password") || entrance != "/new-entrance" || active != 0 {
		t.Fatal("恢复后的密码、入口或旧会话状态错误")
	}
	if !bytes.Equal(key.Private, restored.Private) || public != identity.Encode(nodeKey.Public) {
		t.Fatal("账号恢复更换了控制端或本机节点身份")
	}
	if err := ApplyRecovery(t.Context(), db, recoveryChange{Kind: "reset-entrance", Entrance: "/settings"}); err == nil {
		t.Fatal("恢复允许入口覆盖业务路由")
	}
}
