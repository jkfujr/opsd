package agentcli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/buildinfo"
	"github.com/opsd-labs/opsd/internal/command"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/installation"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd-agent version | install | run")
	}
	switch args[0] {
	case "version", "--version":
		fmt.Printf("opsd-agent %s\n", buildinfo.Version)
		return nil
	case "install":
		return install(args[1:])
	case "run":
		return run(args[1:])
	default:
		return fmt.Errorf("未知命令；用法：opsd-agent version | install | run")
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
	var notifyError error
	err = agent.Run(ctx, db, func() {
		notifyError = installation.NotifyReady()
		if notifyError != nil {
			stop()
		}
	})
	if notifyError != nil {
		return notifyError
	}
	return err
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
	if err := installation.RefuseUnclaimedServices(filepath.Join(command.DefaultAgentDir, "install.json"), "opsd-agent.service"); err != nil {
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
