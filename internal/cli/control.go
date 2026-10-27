package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/opsd-labs/opsd/frontend"
	"github.com/opsd-labs/opsd/internal/buildinfo"
	"github.com/opsd-labs/opsd/internal/command"
	"github.com/opsd-labs/opsd/internal/control"
	"github.com/opsd-labs/opsd/internal/installation"
	"github.com/opsd-labs/opsd/internal/localstate"
	"github.com/opsd-labs/opsd/internal/storage"
)

func RunControl(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd version | install | serve | recovery")
	}
	switch args[0] {
	case "version", "--version":
		fmt.Printf("opsd %s（界面 %s）\n", buildinfo.Version, frontend.Version())
		return nil
	case "serve":
		return serve(args[1:])
	case "install":
		return installControl(args[1:])
	case "recovery":
		return recovery(args[1:])
	case "internal-recovery":
		return recoveryWorker(args[1:])
	default:
		return fmt.Errorf("未知命令；用法：opsd version | install | serve | recovery")
	}
}

func serve(args []string) error {
	set := command.Flags("serve")
	instancePath := set.String("instance", command.DefaultInstance, "")
	if err := command.Parse(set, args); err != nil {
		return err
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return fmt.Errorf("控制端须以专用普通用户运行")
	}
	installation.PrivateFiles()
	var instance localstate.Instance
	if err := localstate.Read(*instancePath, &instance); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbPath := filepath.Join(instance.DataDir, "control.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("控制端尚未初始化：%w", err)
	}
	db, err := storage.Open(ctx, dbPath, storage.Control)
	if err != nil {
		return err
	}
	defer db.Close()
	service, err := control.New(ctx, db, control.Options{Assets: frontend.Assets(), ControlVersion: buildinfo.Version, FrontendVersion: frontend.Version()})
	if err != nil {
		return err
	}
	defer service.Close()
	listener, err := net.Listen("tcp", instance.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: service.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	if err := installation.NotifyReady(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	fmt.Printf("控制端已监听 %s\n", instance.Listen)
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		service.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func installControl(args []string) error {
	value, err := command.ReadInstall(args, false)
	if err != nil {
		return err
	}
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	if err := installation.RefuseUnclaimedServices("/etc/opsd/install.json", "opsd.service", "opsd-agent.service", "opsd-operation@.service", "opsd-recover.service"); err != nil {
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
	instance := localstate.Instance{InstallDir: value.Directory, DataDir: filepath.Join(value.Directory, "data", "control"), AgentDir: command.DefaultAgentDir, Listen: ":" + value.Port}
	intent, created, err := installation.InstallControl(ctx, "/etc/opsd/install.json", command.DefaultInstance, installation.Intent{Instance: instance, Username: value.Username, Password: value.Password, Entrance: value.Entrance, Version: buildinfo.Version}, installation.LinuxHooks(command.DefaultInstance, source, filepath.Join(filepath.Dir(source), "opsd-agent")))
	if err != nil {
		return err
	}
	if !created {
		fmt.Println("已安装，保留现有账号、身份和数据；未执行升级")
		return nil
	}
	fmt.Println("控制端服务已就绪；本机 Agent 已完成加密接入")
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "127.0.0.1"
	}
	_, port, _ := net.SplitHostPort(intent.Instance.Listen)
	fmt.Printf("访问地址：http://%s:%s%s\n账号：%s\n密码：%s\n外部访问：未验证\n", hostname, port, intent.Entrance, intent.Username, intent.Password)
	return nil
}
