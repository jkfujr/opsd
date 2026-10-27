package installation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
)

type AgentIntent struct {
	InstallDir    string `json:"install_dir"`
	ControlURL    string `json:"control_url"`
	ControlPublic string `json:"control_public_key"`
	Token         string `json:"token,omitempty"`
	Version       string `json:"version"`
	Stage         string `json:"stage"`
}

func InstallAgent(ctx context.Context, dataDir, source string, intent AgentIntent) (bool, error) {
	path := filepath.Join(dataDir, "install.json")
	var saved AgentIntent
	err := localstate.Read(path, &saved)
	if err == nil {
		if saved.Stage == "complete" {
			db, err := storage.ReadOnly(ctx, filepath.Join(dataDir, "agent.db"))
			if err != nil {
				return false, err
			}
			defer db.Close()
			_, err = agent.Load(ctx, db)
			return false, err
		}
		if saved.Version != intent.Version {
			return false, fmt.Errorf("须使用原安装版本继续")
		}
		intent = saved
	} else if !os.IsNotExist(err) {
		return false, err
	} else {
		target := filepath.Join(intent.InstallDir, "bin", "opsd-agent")
		sourcePath, _ := filepath.Abs(source)
		if _, err := os.Stat(target); err == nil && sourcePath != target {
			return false, fmt.Errorf("安装位置已有 Agent 程序，拒绝覆盖")
		} else if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if _, err := os.Stat(filepath.Join(dataDir, "agent.db")); err == nil {
			return false, fmt.Errorf("已有 Agent 身份但无本次安装记录，拒绝覆盖")
		} else if !os.IsNotExist(err) {
			return false, err
		}
		intent.Stage = "prepared"
		if err := localstate.Write(path, intent, 0600); err != nil {
			return false, err
		}
	}
	if intent.Stage != "prepared" && intent.Stage != "identity" && intent.Stage != "registered" {
		return false, fmt.Errorf("无法解释 Agent 安装阶段")
	}
	dbPath := filepath.Join(dataDir, "agent.db")
	if intent.Stage != "prepared" {
		if _, err := os.Stat(dbPath); err != nil {
			return false, fmt.Errorf("已初始化的 Agent 数据库缺失")
		}
	}
	db, err := storage.Open(ctx, dbPath, storage.Agent)
	if err != nil {
		return false, err
	}
	if intent.Stage == "prepared" {
		_, err = agent.Initialize(ctx, db, intent.ControlURL, intent.ControlPublic, intent.Token)
	} else {
		_, err = agent.Load(ctx, db)
	}
	closeErr := db.Close()
	if err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	if intent.Stage == "prepared" {
		intent.Stage = "identity"
		if err := localstate.Write(path, intent, 0600); err != nil {
			return false, err
		}
	}
	binary := filepath.Join(intent.InstallDir, "bin", "opsd-agent")
	if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		return false, err
	}
	if err := copyBinary(source, binary); err != nil {
		return false, err
	}
	if intent.Stage == "identity" {
		if err := RegisterAgent(dataDir, binary); err != nil {
			return false, err
		}
		if err := Command(ctx, "systemctl", "daemon-reload"); err != nil {
			return false, err
		}
		intent.Stage = "registered"
		if err := localstate.Write(path, intent, 0600); err != nil {
			return false, err
		}
	}
	if err := Command(ctx, "systemctl", "enable", "opsd-agent.service", "opsd-recover.service"); err != nil {
		return false, err
	}
	if err := Command(ctx, "systemctl", "start", "opsd-agent.service"); err != nil {
		return false, fmt.Errorf("Agent 服务未完成接入：%w", err)
	}
	intent.Stage = "complete"
	intent.Token = ""
	if err := localstate.Write(path, intent, 0600); err != nil {
		return false, err
	}
	return true, nil
}
