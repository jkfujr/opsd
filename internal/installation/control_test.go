package installation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/opsd-labs/opsd/internal/account"
	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/control"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
)

func TestInstallationResumesWithoutReplacingIdentity(t *testing.T) {
	for _, phase := range []string{"prepare", "register", "start"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			instance := localstate.Instance{InstallDir: filepath.Join(root, "install"), DataDir: filepath.Join(root, "control"), AgentDir: filepath.Join(root, "agent"), Listen: ":34567"}
			intent := Intent{Instance: instance, Username: "original", Password: "original-password", Entrance: "/original-entry", Version: "test"}
			record, config := filepath.Join(root, "install.json"), filepath.Join(root, "instance.json")
			fail := true
			hook := func(name string) func(context.Context, localstate.Instance) error {
				return func(context.Context, localstate.Instance) error {
					if fail && phase == name {
						return errors.New("模拟安装中断")
					}
					return nil
				}
			}
			hooks := Hooks{Prepare: hook("prepare"), Register: hook("register"), Start: hook("start")}
			if _, _, err := InstallControl(t.Context(), record, config, intent, hooks); err == nil {
				t.Fatal("中断不应被计为安装成功")
			}
			var saved Intent
			if err := localstate.Read(record, &saved); err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{"prepare": "prepared", "register": "identity", "start": "registered"}[phase]
			if saved.Stage != expected {
				t.Fatalf("中断阶段错误：%s", saved.Stage)
			}
			var prior []byte
			if phase != "prepare" {
				db, err := storage.ReadOnly(t.Context(), filepath.Join(instance.DataDir, "control.db"))
				if err != nil {
					t.Fatal(err)
				}
				key, err := control.LoadKey(t.Context(), db)
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
				prior = key.Private
			}
			replacement := intent
			replacement.Username = "replacement"
			replacement.Password = "replacement-password"
			replacement.Entrance = "/replacement-entry"
			replacement.Instance.Listen = ":45678"
			fail = false
			result, created, err := InstallControl(t.Context(), record, config, replacement, hooks)
			if err != nil || !created {
				t.Fatalf("续接失败：%v", err)
			}
			if result.Username != "original" || result.Instance.Listen != ":34567" {
				t.Fatal("续接丢弃了原安装意图")
			}
			saved = Intent{}
			if err := localstate.Read(record, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Stage != "complete" || saved.Password != "" {
				t.Fatal("完成记录仍保留初始明文密码或阶段未提交")
			}
			db, err := storage.ReadOnly(t.Context(), filepath.Join(instance.DataDir, "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			key, err := control.LoadKey(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			if prior != nil && !bytes.Equal(prior, key.Private) {
				t.Fatal("续接重建了控制端身份")
			}
			var username, hash, entrance, localPublic string
			if err := db.QueryRow("SELECT username,password_hash,entrance FROM account WHERE id=1").Scan(&username, &hash, &entrance); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow("SELECT public_key FROM nodes WHERE local=1").Scan(&localPublic); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if username != "original" || entrance != "/original-entry" || !account.Verify(hash, "original-password") {
				t.Fatal("续接覆盖了原账号")
			}
			agentDB, err := storage.ReadOnly(t.Context(), filepath.Join(instance.AgentDir, "agent.db"))
			if err != nil {
				t.Fatal(err)
			}
			agentIdentity, err := agent.Load(t.Context(), agentDB)
			agentDB.Close()
			if err != nil {
				t.Fatal(err)
			}
			if localPublic != identity.Encode(agentIdentity.Key.Public) || !bytes.Equal(agentIdentity.ControlPublic, key.Public) {
				t.Fatal("本机绑定未使用持久身份")
			}
			if _, created, err := InstallControl(t.Context(), record, config, replacement, hooks); err != nil || created {
				t.Fatalf("重复安装发生覆盖或隐式升级：%v", err)
			}
			if err := os.Remove(filepath.Join(instance.AgentDir, "agent.db")); err != nil {
				t.Fatal(err)
			}
			if _, _, err := InstallControl(t.Context(), record, config, replacement, hooks); err == nil {
				t.Fatal("身份丢失后静默重新安装")
			}
		})
	}
}

func TestDeploymentLockSurvivesItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deployment.lock")
	unlock, err := localstate.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := localstate.Lock(path); err == nil {
		second()
		t.Fatal("并发安装取得了相同部署锁")
	}
	unlock()
	next, err := localstate.Lock(path)
	if err != nil {
		t.Fatal("锁文件保留时不能续接：", err)
	}
	next()
}
