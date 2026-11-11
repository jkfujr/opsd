package installation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/buildinfo"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/storage"
)

type NodeFile struct {
	DataDir       string `json:"data_dir"`
	ExecutorDir   string `json:"executor_dir"`
	Socket        string `json:"socket"`
	ControlSocket string `json:"control_socket"`
	Binary        string `json:"binary"`
	GroupID       int    `json:"group_id"`
}
type NodeInstall struct {
	Account                                               *cluster.AccountChange
	Directory                                             string
	ConfigPath                                            string
	Config                                                cluster.Config
	Username, Password, Entrance, Seed, SeedPublic, Token string
}

func InstallNode(ctx context.Context, o NodeInstall) (bool, error) {
	if e := RequireLinuxRoot(); e != nil {
		return false, e
	}
	if _, e := os.Stat(o.ConfigPath); e == nil {
		f, err := NodePaths(o.ConfigPath)
		if err != nil {
			return false, err
		}
		if buildinfo.Version != "dev" {
			installed, e := exec.CommandContext(ctx, f.Binary, "version", "--plain").Output()
			if e != nil || strings.TrimSpace(string(installed)) != buildinfo.Version {
				return false, fmt.Errorf("已安装其他版本，请通过明确的程序更新流程升级")
			}
		}
		return false, activateNode(ctx, f, o.ConfigPath)
	} else if !os.IsNotExist(e) {
		return false, e
	}
	address, e := NodeAddress(ctx, o.Config, o.Seed)
	if e != nil {
		return false, e
	}
	if o.Config.Address == "" {
		o.Config.AddressMode = "auto"
	} else {
		o.Config.AddressMode = "manual"
	}
	o.Config.Address = address
	if e := RefuseUnclaimedServices(o.ConfigPath, "opsd.service", "opsd-executor.service", "opsd-operation@.service", "opsd-recover.service"); e != nil {
		return false, e
	}
	if _, e := user.Lookup("opsd"); e != nil {
		if e = Command(ctx, "useradd", "--system", "--user-group", "--no-create-home", "--home-dir", "/var/lib/opsd/node", "--shell", "/usr/sbin/nologin", "opsd"); e != nil {
			return false, e
		}
	}
	u, e := user.Lookup("opsd")
	if e != nil {
		return false, e
	}
	uid, e := strconv.Atoi(u.Uid)
	if e != nil {
		return false, e
	}
	gid, e := strconv.Atoi(u.Gid)
	if e != nil {
		return false, e
	}
	f := NodeFile{DataDir: "/var/lib/opsd/node", ExecutorDir: "/var/lib/opsd/executor", Socket: "/run/opsd/executor.sock", ControlSocket: "/run/opsd-node/control.sock", Binary: filepath.Join(o.Directory, "bin", "opsd"), GroupID: gid}
	for _, dir := range []string{filepath.Dir(f.Binary), filepath.Dir(o.ConfigPath), "/var/lib/opsd", f.DataDir, f.ExecutorDir} {
		if e = os.MkdirAll(dir, 0755); e != nil {
			return false, e
		}
	}
	if e = os.Chmod(f.ExecutorDir, 0700); e != nil {
		return false, e
	}
	if e = os.Chmod(f.DataDir, 0700); e != nil {
		return false, e
	}
	source, e := os.Executable()
	if e != nil {
		return false, e
	}
	if e = copyBinary(source, f.Binary); e != nil {
		return false, e
	}
	n, e := cluster.Open(ctx, filepath.Join(f.DataDir, "node.db"), cluster.Options{Config: o.Config, Username: o.Username, Password: o.Password, Entrance: o.Entrance})
	if e != nil {
		return false, e
	}
	if o.Seed != "" {
		e = n.Join(ctx, o.Seed, o.SeedPublic, o.Token)
	}
	if e == nil && o.Account != nil {
		*o.Account, e = n.Password(ctx, n.ID)
	}
	closeErr := n.Close()
	if e != nil {
		return false, e
	}
	if closeErr != nil {
		return false, closeErr
	}
	files, e := os.ReadDir(f.DataDir)
	if e != nil {
		return false, e
	}
	for _, file := range files {
		if e = os.Chown(filepath.Join(f.DataDir, file.Name()), uid, gid); e != nil {
			return false, e
		}
	}
	if e = os.Chown(f.DataDir, uid, gid); e != nil {
		return false, e
	}
	db, e := storage.Open(ctx, filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
	if e != nil {
		return false, e
	}
	if e = db.Close(); e != nil {
		return false, e
	}
	if e = localstate.Write(o.ConfigPath, f, 0640); e != nil {
		return false, e
	}
	if e = os.Chown(o.ConfigPath, 0, gid); e != nil {
		return false, e
	}
	return true, activateNode(ctx, f, o.ConfigPath)
}
func activateNode(ctx context.Context, f NodeFile, configPath string) error {
	var e error
	units := Units(f, configPath)
	for name, body := range units {
		if e = os.WriteFile(filepath.Join("/etc/systemd/system", name), []byte(body), 0644); e != nil {
			return e
		}
	}
	if e = Command(ctx, "systemctl", "daemon-reload"); e != nil {
		return e
	}
	if e = Command(ctx, "systemctl", "enable", "opsd.service", "opsd-executor.service", "opsd-recover.service"); e != nil {
		return e
	}
	if e = Command(ctx, "systemctl", "start", "opsd-executor.service", "opsd.service"); e != nil {
		return e
	}
	return nil
}
func Units(f NodeFile, configPath string) map[string]string {
	command := quote(f.Binary)
	config := " --config " + quote(configPath)
	return map[string]string{
		"opsd.service":            "[Unit]\nDescription=OPSD 对等节点\nAfter=network-online.target opsd-executor.service\nWants=network-online.target opsd-executor.service\n\n[Service]\nUser=opsd\nType=notify\nNotifyAccess=main\nTimeoutStartSec=45\nGroup=opsd\nRuntimeDirectory=opsd-node\nRuntimeDirectoryMode=0700\nUMask=0077\nExecStart=" + command + " node" + config + "\nRestart=on-failure\nRestartSec=3\nEnvironment=GOMEMLIMIT=64MiB\nNoNewPrivileges=true\nProtectSystem=strict\nReadWritePaths=" + quote(f.DataDir) + "\n\n[Install]\nWantedBy=multi-user.target\n",
		"opsd-executor.service":   "[Unit]\nDescription=OPSD 本机执行器\nAfter=network.target opsd-recover.service\nWants=opsd-recover.service\n\n[Service]\nUser=root\nType=notify\nNotifyAccess=main\nTimeoutStartSec=45\nGroup=opsd\nUMask=0007\nRuntimeDirectory=opsd\nRuntimeDirectoryMode=0750\nExecStart=" + command + " executor" + config + "\nRestart=on-failure\nRestartSec=3\n\n[Install]\nWantedBy=multi-user.target\n",
		"opsd-operation@.service": "[Unit]\nDescription=OPSD 本机操作 %i\nAfter=network.target\n\n[Service]\nType=oneshot\nUser=root\nUMask=0077\nExecStart=" + command + " operation" + config + " --id %i\nTimeoutStartSec=infinity\nTimeoutStopSec=30\nKillMode=control-group\nRestart=on-failure\nRestartSec=3\n",
		"opsd-recover.service":    "[Unit]\nDescription=OPSD 本机操作恢复\nAfter=ufw.service firewalld.service nftables.service\nBefore=opsd-executor.service\n\n[Service]\nType=oneshot\nUser=root\nUMask=0077\nExecStart=" + command + " recover" + config + "\nTimeoutStartSec=infinity\nTimeoutStopSec=30\nKillMode=control-group\n\n[Install]\nWantedBy=multi-user.target\n"}
}
func NodePaths(configPath string) (NodeFile, error) {
	var f NodeFile
	e := localstate.Read(configPath, &f)
	if e == nil && (f.DataDir == "" || f.ExecutorDir == "" || f.Socket == "") {
		e = fmt.Errorf("节点安装配置不完整")
	}
	return f, e
}
