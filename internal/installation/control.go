package installation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/control"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
)

type Intent struct {
	Instance localstate.Instance `json:"instance"`
	Username string              `json:"username"`
	Password string              `json:"password,omitempty"`
	Entrance string              `json:"entrance"`
	Version  string              `json:"version"`
	Stage    string              `json:"stage"`
}

// Hooks 仅隔离安装所需的主机操作，临时目录测试不会伪装为 systemd 验收。
type Hooks struct {
	Prepare  func(context.Context, localstate.Instance) error
	Register func(context.Context, localstate.Instance) error
	Start    func(context.Context, localstate.Instance) error
}

func InstallControl(ctx context.Context, recordPath, instancePath string, intent Intent, hooks Hooks) (Intent, bool, error) {
	var saved Intent
	err := localstate.Read(recordPath, &saved)
	if err == nil {
		if saved.Stage != "complete" && saved.Version != intent.Version {
			return intent, false, fmt.Errorf("须使用原安装版本继续，不能在续接时隐式升级")
		}
		intent = saved
	} else if !errors.Is(err, os.ErrNotExist) {
		return intent, false, err
	} else {
		if _, err := os.Stat(instancePath); err == nil {
			return intent, false, fmt.Errorf("已有安装配置，拒绝覆盖或隐式升级")
		} else if !errors.Is(err, os.ErrNotExist) {
			return intent, false, err
		}
		for _, path := range []string{filepath.Join(intent.Instance.DataDir, "control.db"), filepath.Join(intent.Instance.AgentDir, "agent.db"), filepath.Join(intent.Instance.InstallDir, "bin", "opsd"), filepath.Join(intent.Instance.InstallDir, "bin", "opsd-agent")} {
			if _, err := os.Stat(path); err == nil {
				return intent, false, fmt.Errorf("发现不属于本次安装记录的已有文件，拒绝覆盖")
			} else if !errors.Is(err, os.ErrNotExist) {
				return intent, false, err
			}
		}
		intent.Stage = "prepared"
		if err := localstate.Write(recordPath, intent, 0600); err != nil {
			return intent, false, err
		}
	}
	if intent.Stage == "complete" {
		return intent, false, requireIdentity(ctx, intent.Instance)
	}
	if intent.Stage != "prepared" && intent.Stage != "identity" && intent.Stage != "registered" {
		return intent, false, fmt.Errorf("无法解释安装阶段：%s", intent.Stage)
	}
	if err := hooks.Prepare(ctx, intent.Instance); err != nil {
		return intent, false, err
	}
	if intent.Stage == "prepared" {
		if err := initializeControl(ctx, intent); err != nil {
			return intent, false, err
		}
		intent.Stage = "identity"
		if err := localstate.Write(recordPath, intent, 0600); err != nil {
			return intent, false, err
		}
	} else {
		// 已完成身份阶段时，缺失数据库或密钥必须停止，不能重新初始化。
		if err := requireIdentity(ctx, intent.Instance); err != nil {
			return intent, false, err
		}
	}
	if err := localstate.Write(instancePath, intent.Instance, 0640); err != nil {
		return intent, false, err
	}
	if intent.Stage == "identity" {
		if err := hooks.Register(ctx, intent.Instance); err != nil {
			return intent, false, err
		}
		intent.Stage = "registered"
		if err := localstate.Write(recordPath, intent, 0600); err != nil {
			return intent, false, err
		}
	}
	if err := hooks.Start(ctx, intent.Instance); err != nil {
		return intent, false, err
	}
	intent.Stage = "complete"
	result := intent
	intent.Password = ""
	if err := localstate.Write(recordPath, intent, 0600); err != nil {
		return result, false, err
	}
	return result, true, nil
}

func initializeControl(ctx context.Context, intent Intent) error {
	instance := intent.Instance
	for _, path := range []string{instance.DataDir, instance.AgentDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
	}
	controlDB, err := storage.Open(ctx, filepath.Join(instance.DataDir, "control.db"), storage.Control)
	if err != nil {
		return err
	}
	defer controlDB.Close()
	var controlCount int
	if err := controlDB.QueryRowContext(ctx, "SELECT count(*) FROM account").Scan(&controlCount); err != nil {
		return err
	}
	agentDB, err := storage.Open(ctx, filepath.Join(instance.AgentDir, "agent.db"), storage.Agent)
	if err != nil {
		return err
	}
	defer agentDB.Close()
	var count int
	if err := agentDB.QueryRowContext(ctx, "SELECT count(*) FROM agent_identity").Scan(&count); err != nil {
		return err
	}
	var agentKey identity.Key
	if count == 0 {
		if controlCount != 0 {
			return fmt.Errorf("控制端身份已存在，但本机 Agent 身份缺失，拒绝重新生成")
		}
		agentKey, err = agent.Initialize(ctx, agentDB, "", "", "")
	} else {
		var private, public []byte
		err = agentDB.QueryRowContext(ctx, "SELECT private_key,public_key FROM agent_identity WHERE id=1").Scan(&private, &public)
		if err == nil {
			agentKey, err = identity.Load(private, public)
		}
	}
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	if err := control.Initialize(ctx, controlDB, intent.Username, intent.Password, intent.Entrance, identity.Encode(agentKey.Public), hostname); err != nil {
		return err
	}
	var localPublic string
	if err := controlDB.QueryRowContext(ctx, "SELECT public_key FROM nodes WHERE local=1").Scan(&localPublic); err != nil || localPublic != identity.Encode(agentKey.Public) {
		return fmt.Errorf("本机节点绑定与 Agent 身份不匹配")
	}
	key, err := control.LoadKey(ctx, controlDB)
	if err != nil {
		return err
	}
	address := LocalAddress(instance.Listen)
	_, err = agentDB.ExecContext(ctx, "UPDATE agent_identity SET control_url=?,control_public_key=? WHERE id=1", address, identity.Encode(key.Public))
	return err
}

func requireIdentity(ctx context.Context, instance localstate.Instance) error {
	for _, item := range []struct {
		path string
		kind storage.Kind
	}{{filepath.Join(instance.DataDir, "control.db"), storage.Control}, {filepath.Join(instance.AgentDir, "agent.db"), storage.Agent}} {
		if _, err := os.Stat(item.path); err != nil {
			return fmt.Errorf("已有安装的数据库缺失：%w", err)
		}
		db, err := storage.ReadOnly(ctx, item.path)
		if err != nil {
			return err
		}
		if item.kind == storage.Control {
			_, err = control.LoadKey(ctx, db)
		} else {
			_, err = agent.Load(ctx, db)
		}
		db.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func LocalAddress(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	return "http://127.0.0.1:" + port
}
