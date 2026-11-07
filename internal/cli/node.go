package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jkfujr/opsd/frontend"
	"github.com/jkfujr/opsd/internal/buildinfo"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/control"
	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd version | install | node | network | console | api | account | executor | operation | recover")
	}
	switch args[0] {
	case "version", "--version":
		if len(args) == 2 && args[1] == "--plain" {
			fmt.Println(buildinfo.Version)
			return nil
		}
		fmt.Printf("opsd %s（界面 %s）\n", buildinfo.Version, frontend.Version())
		return nil
	case "install":
		return install(args[1:])
	case "node":
		return serveNode(args[1:])
	case "network":
		return network(args[1:])
	case "executor":
		return serveExecutor(args[1:])
	case "operation":
		return operation(args[1:], false)
	case "recover":
		return operation(args[1:], true)
	case "console", "api", "account":
		return localChange(args[0], args[1:])
	default:
		return fmt.Errorf("未知命令")
	}
}
func processContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
func fileFlags(name string, args []string) (installation.NodeFile, error) {
	set := command.Flags(name)
	path := set.String("config", command.DefaultInstance, "")
	if e := command.Parse(set, args); e != nil {
		return installation.NodeFile{}, e
	}
	return installation.NodePaths(*path)
}
func serveNode(args []string) error {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return fmt.Errorf("节点服务须以普通用户运行")
	}
	f, e := fileFlags("node", args)
	if e != nil {
		return e
	}
	ctx, cancel := processContext()
	defer cancel()
	unlock, e := localstate.Lock(filepath.Join(f.DataDir, "node.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	n, e := cluster.Open(ctx, filepath.Join(f.DataDir, "node.db"), cluster.Options{Executor: executor.NewClient(f.Socket)})
	if e != nil {
		return e
	}
	defer n.Close()
	s, e := control.New(ctx, n.DB, control.Options{EnforceListenerState: true, Node: n, Assets: frontend.Assets(), NodeVersion: buildinfo.Version, FrontendVersion: frontend.Version()})
	if e != nil {
		return e
	}
	defer s.Close()
	c, e := n.Config(ctx)
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp", c.Listen)
	if e != nil {
		return e
	}
	peerServer := &http.Server{Handler: n.PeerHandler(), ReadHeaderTimeout: 10 * time.Second}
	go peerServer.Serve(listener)
	defer peerServer.Close()
	var web *http.Server
	var listen string
	var webMu sync.Mutex
	defer func() {
		webMu.Lock()
		defer webMu.Unlock()
		if web != nil {
			web.Close()
		}
	}()
	n.Configure = func(ctx context.Context, next cluster.Config) (func(bool), error) {
		webMu.Lock()
		var candidate net.Listener
		if (next.Console || next.API) && (web == nil || listen != next.ConsoleListen) {
			var err error
			candidate, err = net.Listen("tcp", next.ConsoleListen)
			if err != nil {
				webMu.Unlock()
				return nil, &cluster.Fault{Code: "conflict", Message: "Web 监听地址不可用"}
			}
		}
		return func(commit bool) {
			defer webMu.Unlock()
			if !commit {
				if candidate != nil {
					candidate.Close()
				}
				return
			}
			if web != nil && (!(next.Console || next.API) || candidate != nil) {
				web.Close()
				web = nil
			}
			if candidate != nil {
				web = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
				listen = next.ConsoleListen
				go web.Serve(candidate)
			}
		}, nil
	}
	ready, e := n.Configure(ctx, c)
	if e != nil {
		return e
	}
	ready(true)
	local, e := executor.Listen(f.ControlSocket)
	if e != nil {
		return e
	}
	defer local.Close()
	if e = os.Chmod(f.ControlSocket, 0600); e != nil {
		return e
	}
	localServer := &http.Server{Handler: n.LocalHandler(), ReadHeaderTimeout: 5 * time.Second}
	go localServer.Serve(local)
	defer localServer.Close()
	n.Start()
	if e = installation.NotifyReady(); e != nil {
		return e
	}
	<-ctx.Done()
	return nil
}
func serveExecutor(args []string) error {
	if e := installation.RequireLinuxRoot(); e != nil {
		return e
	}
	f, e := fileFlags("executor", args)
	if e != nil {
		return e
	}
	ctx, cancel := processContext()
	defer cancel()
	db, e := storage.Open(ctx, filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
	if e != nil {
		return e
	}
	defer db.Close()
	service := executor.NewServices(db)
	service.OperationLock = func() (func(), error) { return localstate.Lock(filepath.Join(f.ExecutorDir, "operation.lock")) }
	trustDone := make(chan struct{})
	go func() { defer close(trustDone); service.MaintainTrust(ctx) }()
	defer func() { cancel(); <-trustDone }()
	l, e := executor.Listen(f.Socket)
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chown(f.Socket, 0, f.GroupID); e != nil {
		return e
	}
	handler := (&executor.Server{Services: service, Directory: f.ExecutorDir}).Handler()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); server.Close() }()
	if e = installation.NotifyReady(); e != nil {
		return e
	}
	e = server.Serve(l)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func operation(args []string, recoverAll bool) error {
	if e := installation.RequireLinuxRoot(); e != nil {
		return e
	}
	set := command.Flags("operation")
	config := set.String("config", command.DefaultInstance, "")
	id := set.String("id", "", "")
	if e := command.Parse(set, args); e != nil {
		return e
	}
	f, e := installation.NodePaths(*config)
	if e != nil {
		return e
	}
	if !recoverAll && !operations.ValidID(*id) {
		return fmt.Errorf("操作标识无效")
	}
	ctx, cancel := processContext()
	defer cancel()
	cgroup, e := os.ReadFile("/proc/self/cgroup")
	if e != nil {
		return e
	}
	unit := "opsd-operation@" + *id + ".service"
	if recoverAll {
		unit = "opsd-recover.service"
	}
	if os.Getenv("INVOCATION_ID") == "" || !strings.Contains(string(cgroup), "/"+unit) {
		return fmt.Errorf("此入口须由对应 systemd 操作服务调用")
	}
	db, e := storage.Open(ctx, filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
	if e != nil {
		return e
	}
	defer db.Close()
	service := executor.NewServices(db)
	ids := []string{*id}
	if recoverAll {
		ids, e = service.Engine.Store.Pending(ctx)
		if e != nil {
			return e
		}
	}
	if recoverAll {
		host := firewall.NativeHost{}
		for _, value := range ids {
			if !operations.ValidID(value) {
				return fmt.Errorf("操作标识无效")
			}
			unit := "opsd-operation@" + value + ".service"
			if _, e = host.Run(ctx, "systemctl", "stop", unit); e != nil {
				return e
			}
			group, err := host.Run(ctx, "systemctl", "show", unit, "--property=ControlGroup", "--value")
			if err != nil {
				return err
			}
			if path := strings.TrimSpace(string(group)); path != "" {
				path = filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(path, "/"))
				events, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
				if err == nil {
					if strings.Contains(string(events), "populated 1") {
						return fmt.Errorf("旧操作仍有执行中的命令")
					}
				} else if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					return fmt.Errorf("无法确认旧操作已经停止")
				}
			}
		}
	}
	unlock, e := localstate.Lock(filepath.Join(f.ExecutorDir, "operation.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	for _, value := range ids {
		if e = service.Engine.Run(ctx, value); e != nil {
			return e
		}
	}
	return nil
}
func install(args []string) error {
	set := command.Flags("install")
	expectedVersion := set.String("expected-version", "", "")
	directory := set.String("install-dir", "/opt/opsd", "")
	config := set.String("config", command.DefaultInstance, "")
	username := set.String("username", "admin", "")
	password := set.String("password", os.Getenv("OPSD_PASSWORD"), "")
	entrance := set.String("entrance", "", "")
	address := set.String("address", "", "")
	name := set.String("name", "", "")
	region := set.String("region", "", "")
	listen := set.String("listen", ":8444", "")
	consoleListen := set.String("console-listen", ":65533", "")
	seed := set.String("peer-address", "", "")
	public := set.String("peer-public-key", "", "")
	token := set.String("enrollment-token", os.Getenv("OPSD_ENROLLMENT_TOKEN"), "")
	set.StringVar(seed, "e", "", "")
	set.StringVar(public, "k", "", "")
	set.StringVar(token, "t", *token, "")
	set.StringVar(name, "n", "", "")
	set.StringVar(region, "r", "", "")
	_ = set.Bool("non-interactive", false, "")
	if e := command.Parse(set, args); e != nil {
		return e
	}
	if *expectedVersion != "" && *expectedVersion != buildinfo.Version {
		return fmt.Errorf("安装程序版本与接入命令不匹配")
	}
	if *password == "" {
		*password = identity.Random()
	}
	if *entrance == "" {
		*entrance = "/s_" + identity.Random()
	}
	if !filepath.IsAbs(*directory) || filepath.Clean(*directory) == string(filepath.Separator) {
		return fmt.Errorf("安装目录须为明确的绝对目录")
	}
	ctx, cancel := processContext()
	defer cancel()
	var actual cluster.AccountChange
	created, e := installation.InstallNode(ctx, installation.NodeInstall{Account: &actual, Directory: *directory, ConfigPath: *config, Config: cluster.Config{Name: strings.TrimSpace(*name), Address: *address, Region: *region, Listen: *listen, ConsoleListen: *consoleListen}, Username: *username, Password: *password, Entrance: *entrance, Seed: *seed, SeedPublic: *public, Token: *token})
	if e != nil {
		return e
	}
	if !created {
		fmt.Println("节点已安装")
		return nil
	}
	fmt.Printf("节点已安装\n账号：%s\n密码：%s\n登录入口：%s\n", actual.Username, actual.Password, actual.Entrance)
	return nil
}
func localChange(kind string, args []string) error {
	set := command.Flags(kind)
	path := set.String("config", command.DefaultInstance, "")
	enabled := set.Bool("enabled", true, "")
	password := set.String("password", os.Getenv("OPSD_PASSWORD"), "")
	entrance := set.String("entrance", "", "")
	if e := command.Parse(set, args); e != nil {
		return e
	}
	f, e := installation.NodePaths(*path)
	if e != nil {
		return e
	}
	ctx, cancel := processContext()
	defer cancel()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", f.ControlSocket)
	}}, Timeout: 10 * time.Second}
	call := func(method, path string, body, out any) error {
		var reader io.Reader
		if body != nil {
			b, e := json.Marshal(body)
			if e != nil {
				return e
			}
			reader = bytes.NewReader(b)
		}
		r, e := http.NewRequestWithContext(ctx, method, "http://node"+path, reader)
		if e != nil {
			return e
		}
		response, e := client.Do(r)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("本机请求未完成（%d）", response.StatusCode)
		}
		return json.NewDecoder(response.Body).Decode(out)
	}
	var self struct{ ID string }
	if e = call("GET", "/self", nil, &self); e != nil {
		return e
	}
	request := cluster.Request{ID: cluster.NewOperationID(self.ID), Kind: kind}
	if kind == "console" || kind == "api" {
		request.Payload, _ = json.Marshal(map[string]bool{"enabled": *enabled})
	} else {
		if *password == "" && *entrance == "" {
			return fmt.Errorf("请提供新密码或登录入口")
		}
		request.Payload, _ = json.Marshal(cluster.AccountChange{Password: *password, Entrance: *entrance})
	}
	var op cluster.Operation
	if e = call("POST", "/command", request, &op); e != nil {
		if queryErr := call("GET", "/operations/"+request.ID, nil, &op); queryErr != nil {
			return fmt.Errorf("受理结果未知，操作 %s", request.ID)
		}
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if operations.Terminal(op.State) {
			if op.State != protocol.Succeeded {
				return fmt.Errorf("本机修改未完成：%s", op.ID)
			}
			fmt.Println("已保存")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if e = call("GET", "/operations/"+op.ID, nil, &op); e != nil {
				return e
			}
		}
	}
}
