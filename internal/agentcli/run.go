package agentcli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/buildinfo"
	"github.com/opsd-labs/opsd/internal/command"
	"github.com/opsd-labs/opsd/internal/firewall"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/installation"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/operations"
	"github.com/opsd-labs/opsd/internal/storage"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd-agent version | install | run | execute | recover")
	}
	switch args[0] {
	case "version", "--version":
		fmt.Printf("opsd-agent %s\n", buildinfo.Version)
		return nil
	case "install":
		return install(args[1:])
	case "run":
		return run(args[1:])
	case "execute":
		return execute(args[1:], false)
	case "recover":
		return execute(args[1:], true)
	default:
		return fmt.Errorf("未知命令；用法：opsd-agent version | install | run | execute | recover")
	}
}

func run(args []string) error {
	set := command.Flags("run")
	directory := set.String("data-dir", command.DefaultAgentDir, "")
	if err := command.Parse(set, args); err != nil {
		return err
	}
	installation.PrivateFiles()
	path := filepath.Join(*directory, "agent.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("Agent 尚未初始化：%w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := storage.Open(ctx, path, storage.Agent)
	if err != nil {
		return err
	}
	defer db.Close()
	services := agent.NewServices(db)
	if err := services.Recover(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "本机操作启动未完成，保留未决限制：", err)
	}
	var notifyError error
	err = agent.RunWithServices(ctx, db, func() {
		notifyError = installation.NotifyReady()
		if notifyError != nil {
			stop()
		}
	}, services)
	if notifyError != nil {
		return notifyError
	}
	return err
}

func execute(args []string, recoverAll bool) error {
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	set := command.Flags("execute")
	directory := set.String("data-dir", command.DefaultAgentDir, "")
	id := set.String("operation-id", "", "")
	if err := command.Parse(set, args); err != nil {
		return err
	}
	if !recoverAll && *id == "" {
		return fmt.Errorf("缺少操作标识")
	}
	installation.PrivateFiles()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	path := filepath.Join(*directory, "agent.db")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := storage.Open(ctx, path, storage.Agent)
	if err != nil {
		return err
	}
	defer db.Close()
	service := agent.NewServices(db)
	ids := []string{*id}
	if recoverAll {
		ids, err = service.Engine.Store.Pending(ctx)
		if err != nil {
			return err
		}
	}
	cgroup, e := os.ReadFile("/proc/self/cgroup")
	if e != nil {
		return e
	}
	if recoverAll && (os.Getenv("INVOCATION_ID") == "" || !strings.Contains(string(cgroup), "/opsd-recover.service")) {
		return fmt.Errorf("恢复入口须由 systemd 恢复服务调用，请运行 systemctl start opsd-recover.service")
	}
	if !recoverAll {
		if !operations.ValidID(*id) {
			return fmt.Errorf("操作标识无效")
		}
		if os.Getenv("INVOCATION_ID") == "" || !strings.Contains(string(cgroup), "/opsd-operation@"+*id+".service") {
			return fmt.Errorf("执行入口须由本次 systemd 操作服务调用，人工恢复请使用 recover")
		}
	} else {
		host := firewall.NativeHost{}
		for _, operationID := range ids {
			if !operations.ValidID(operationID) {
				return fmt.Errorf("未决操作标识无法解释")
			}
			unit := "opsd-operation@" + operationID + ".service"
			if _, e := host.Run(ctx, "systemctl", "stop", unit); e != nil {
				return fmt.Errorf("旧操作进程未能停止：%w", e)
			}
			group, e := host.Run(ctx, "systemctl", "show", unit, "--property=ControlGroup", "--value")
			if e != nil {
				return e
			}
			if value := strings.TrimSpace(string(group)); value != "" {
				path := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(value, "/"))
				events, e := os.ReadFile(filepath.Join(path, "cgroup.events"))
				if e == nil {
					if strings.Contains(string(events), "populated 1") {
						return fmt.Errorf("旧操作仍有受管命令，不能启动相反的恢复动作")
					}
				} else if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					return fmt.Errorf("无法确认旧操作命令全部停止：%w", e)
				}
			}
		}
	}
	unlock, err := localstate.Lock(filepath.Join(*directory, "operation.lock"))
	if err != nil {
		return fmt.Errorf("已有本机操作进程，不能并行执行或恢复：%w", err)
	}
	defer unlock()
	for _, operationID := range ids {
		if err = service.Engine.Run(ctx, operationID); err != nil {
			return err
		}
		if recoverAll {
			l, err := service.Engine.Store.Get(ctx, operationID)
			if err != nil {
				return err
			}
			if !operations.Terminal(l.Record.State) {
				return fmt.Errorf("操作 %s 仍需处理，已保留写入限制", operationID)
			}
		}
	}
	return nil
}

func install(args []string) error {
	value, err := command.ReadInstall(args, true)
	if err != nil {
		return err
	}
	if err := agent.ValidateAddress(value.Address); err != nil {
		return err
	}
	if _, err := identity.Decode(value.Public); err != nil {
		return err
	}
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	if err := installation.RefuseUnclaimedServices(filepath.Join(command.DefaultAgentDir, "install.json"), "opsd-agent.service", "opsd-operation@.service", "opsd-recover.service"); err != nil {
		return err
	}
	installation.PrivateFiles()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	unlock, err := localstate.Lock(filepath.Join(command.DefaultAgentDir, "deployment.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	source, err := os.Executable()
	if err != nil {
		return err
	}
	created, err := installation.InstallAgent(ctx, command.DefaultAgentDir, source, installation.AgentIntent{InstallDir: value.Directory, ControlURL: value.Address, ControlPublic: value.Public, Token: value.Token, Version: buildinfo.Version})
	if err != nil {
		return err
	}
	if created {
		fmt.Println("Agent 服务已就绪，已完成加密接入")
	} else {
		fmt.Println("已安装，保留现有身份和数据；未执行升级")
	}
	return nil
}
