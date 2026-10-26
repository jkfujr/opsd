package agent

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/storage"
)

func TestAgentIdentityIsRetainedAndMissingKeyFails(t *testing.T) {
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"), storage.Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	controlKey, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	original, err := Initialize(t.Context(), db, "http://127.0.0.1:34567", identity.Encode(controlKey.Public), "one-use")
	if err != nil {
		t.Fatal(err)
	}
	next, err := Initialize(t.Context(), db, "http://different.invalid:45678", identity.Encode(controlKey.Public), "different")
	if err != nil || !bytes.Equal(original.Private, next.Private) {
		t.Fatalf("重复初始化更换了身份：%v", err)
	}
	current, err := Load(t.Context(), db)
	if err != nil || current.ControlURL != "http://127.0.0.1:34567" || current.EnrollmentToken != "one-use" {
		t.Fatalf("重复安装覆盖了原接入配置：%+v %v", current, err)
	}
	if _, err := db.Exec("UPDATE agent_identity SET private_key=X'' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), db); err == nil {
		t.Fatal("私钥丢失时仍可接入")
	}
	if _, err := Initialize(t.Context(), db, "http://127.0.0.1:34567", identity.Encode(controlKey.Public), "new"); err == nil {
		t.Fatal("私钥丢失时生成了替代身份")
	}
}
