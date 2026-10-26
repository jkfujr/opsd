package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/opsd-labs/opsd/internal/account"
	"github.com/opsd-labs/opsd/internal/command"
	"github.com/opsd-labs/opsd/internal/installation"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
	"golang.org/x/term"
)

type recoveryChange struct {
	Kind     string `json:"kind"`
	Password string `json:"password,omitempty"`
	Entrance string `json:"entrance,omitempty"`
}

func recovery(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd recovery show-entrance | reset-password | reset-entrance --entrance <入口>")
	}
	action := args[0]
	if action != "show-entrance" && action != "reset-password" && action != "reset-entrance" {
		return fmt.Errorf("未知恢复命令")
	}
	set := command.Flags(action)
	instancePath := set.String("instance", command.DefaultInstance, "")
	entrance := set.String("entrance", "", "")
	if err := command.Parse(set, args[1:]); err != nil {
		return err
	}
	var instance localstate.Instance
	if err := localstate.Read(*instancePath, &instance); err != nil {
		return err
	}
	ctx := context.Background()
	if action == "show-entrance" {
		db, err := storage.ReadOnly(ctx, filepath.Join(instance.DataDir, "control.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		var path string
		if err := db.QueryRowContext(ctx, "SELECT entrance FROM account WHERE id=1").Scan(&path); err != nil {
			return err
		}
		_, port, err := net.SplitHostPort(instance.Listen)
		if err != nil {
			return err
		}
		fmt.Printf("http://127.0.0.1:%s%s\n", port, path)
		return nil
	}
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	change := recoveryChange{Kind: action, Entrance: *entrance}
	if action == "reset-password" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("密码重置须使用交互终端进行隐藏输入")
		}
		password, err := command.Prompt("新密码", "", true)
		if err != nil {
			return err
		}
		confirm, err := command.Prompt("再次输入新密码", "", true)
		if err != nil {
			return err
		}
		if password != confirm {
			return fmt.Errorf("两次密码不一致")
		}
		if len(password) < 8 || len(password) > 1024 {
			return fmt.Errorf("密码长度须为 8 至 1024 字节")
		}
		change.Password = password
	} else if err := account.Validate("admin", *entrance); err != nil {
		return err
	}
	unlock, err := localstate.Lock(filepath.Join(instance.AgentDir, "deployment.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(instance.AgentDir, "deployment.json")); err == nil {
		return fmt.Errorf("存在未收尾的部署记录，须先处理原部署操作")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := installation.Command(ctx, "systemctl", "stop", "opsd.service"); err != nil {
		return err
	}
	data, err := json.Marshal(change)
	if err != nil {
		return err
	}
	// 由数据库所属用户写入，密码经标准输入传递，不进入命令参数或系统日志。
	worker := exec.CommandContext(ctx, "runuser", "-u", "opsd", "--", filepath.Join(instance.InstallDir, "bin", "opsd"), "internal-recovery", "--instance", *instancePath)
	worker.Stdin = strings.NewReader(string(data))
	output, updateErr := worker.CombinedOutput()
	startErr := installation.Command(ctx, "systemctl", "start", "opsd.service")
	if updateErr != nil {
		if startErr != nil {
			return fmt.Errorf("恢复更新失败：%s；控制端也未能启动：%w", strings.TrimSpace(string(output)), startErr)
		}
		return fmt.Errorf("恢复更新失败，控制端已重新启动：%s", strings.TrimSpace(string(output)))
	}
	if startErr != nil {
		return fmt.Errorf("恢复更新已保存，控制端启动失败：%w", startErr)
	}
	fmt.Println("恢复更新已保存，旧会话已失效，控制端已重新启动")
	return nil
}

func recoveryWorker(args []string) error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("恢复写入须以控制端数据所属用户执行")
	}
	set := command.Flags("internal-recovery")
	instancePath := set.String("instance", command.DefaultInstance, "")
	if err := command.Parse(set, args); err != nil {
		return err
	}
	var instance localstate.Instance
	if err := localstate.Read(*instancePath, &instance); err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 16*1024))
	decoder.DisallowUnknownFields()
	var change recoveryChange
	if err := decoder.Decode(&change); err != nil {
		return fmt.Errorf("恢复输入无效")
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(instance.DataDir, "control.db"), storage.Control)
	if err != nil {
		return err
	}
	defer db.Close()
	return ApplyRecovery(ctx, db, change)
}

func ApplyRecovery(ctx context.Context, db *sql.DB, change recoveryChange) error {
	var field, value string
	switch change.Kind {
	case "reset-password":
		hash, err := account.Hash(change.Password)
		if err != nil {
			return err
		}
		field, value = "password_hash", hash
	case "reset-entrance":
		if err := account.Validate("admin", change.Entrance); err != nil {
			return err
		}
		field, value = "entrance", change.Entrance
	default:
		return fmt.Errorf("恢复动作无效")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE account SET "+field+"=?,revision=revision+1 WHERE id=1", value)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("管理员账号不存在")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE revoked_at IS NULL", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
