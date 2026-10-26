package installation

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/opsd-labs/opsd/internal/localstate"
)

func RequireLinuxRoot() error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("自动安装与本机恢复须在 Debian 或 Ubuntu 上以 root 执行")
	}
	release, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}
	supported := false
	for _, line := range strings.Split(string(release), "\n") {
		if line == "ID=debian" || line == "ID=ubuntu" || line == `ID="debian"` || line == `ID="ubuntu"` {
			supported = true
		}
	}
	if !supported {
		return fmt.Errorf("自动安装仅支持 Debian 与 Ubuntu")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("未找到 systemd")
	}
	return nil
}

func RefuseUnclaimedServices(recordPath string, names ...string) error {
	if _, err := os.Stat(recordPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, directory := range []string{"/etc/systemd/system", "/run/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"} {
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
				return fmt.Errorf("存在未由本次安装记录管理的 %s，拒绝覆盖", name)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func Command(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行 %s 失败：%s", name, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyBinary(source, target string) error {
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if sourceAbs == targetAbs {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(target+".tmp", 0755); err != nil {
		return err
	}
	if err := os.Rename(target+".tmp", target); err != nil {
		return err
	}
	return localstate.SyncDirectory(filepath.Dir(target))
}

func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(value) + `"`
}

func LinuxHooks(instancePath, controlSource, agentSource string) Hooks {
	return Hooks{
		Prepare: func(ctx context.Context, instance localstate.Instance) error {
			if _, err := user.Lookup("opsd"); err != nil {
				if err := Command(ctx, "useradd", "--system", "--user-group", "--no-create-home", "--home-dir", instance.InstallDir, "--shell", "/usr/sbin/nologin", "opsd"); err != nil {
					return err
				}
			}
			serviceUser, err := user.Lookup("opsd")
			if err != nil {
				return err
			}
			if serviceUser.Uid == "0" {
				return fmt.Errorf("opsd 服务账号不能是 root")
			}
			for _, path := range []string{instance.InstallDir, filepath.Join(instance.InstallDir, "bin"), filepath.Join(instance.InstallDir, "data")} {
				if err := os.MkdirAll(path, 0755); err != nil {
					return err
				}
				if err := os.Chmod(path, 0755); err != nil {
					return err
				}
			}
			if err := copyBinary(controlSource, filepath.Join(instance.InstallDir, "bin", "opsd")); err != nil {
				return err
			}
			return copyBinary(agentSource, filepath.Join(instance.InstallDir, "bin", "opsd-agent"))
		},
		Register: func(ctx context.Context, instance localstate.Instance) error {
			if err := ControlOwnership(instance, instancePath); err != nil {
				return err
			}
			serviceUser, err := user.Lookup("opsd")
			if err != nil {
				return err
			}
			serviceGroup, err := user.LookupGroupId(serviceUser.Gid)
			if err != nil {
				return err
			}
			controlUnit := "[Unit]\nDescription=OPSD 控制端\nAfter=network.target\n\n[Service]\nType=notify\nNotifyAccess=main\nUser=opsd\nGroup=" + serviceGroup.Name + "\nUMask=0077\nExecStart=" + quote(filepath.Join(instance.InstallDir, "bin", "opsd")) + " serve --instance " + quote(instancePath) + "\nRestart=on-failure\nRestartSec=3\nRestartPreventExitStatus=2\nTimeoutStartSec=60\n\n[Install]\nWantedBy=multi-user.target\n"
			if err := os.WriteFile("/etc/systemd/system/opsd.service", []byte(controlUnit), 0644); err != nil {
				return err
			}
			if err := RegisterAgent(instance.AgentDir, filepath.Join(instance.InstallDir, "bin", "opsd-agent")); err != nil {
				return err
			}
			return Command(ctx, "systemctl", "daemon-reload")
		},
		Start: func(ctx context.Context, instance localstate.Instance) error {
			if err := ControlOwnership(instance, instancePath); err != nil {
				return err
			}
			if err := Command(ctx, "systemctl", "enable", "opsd.service", "opsd-agent.service"); err != nil {
				return err
			}
			if err := Command(ctx, "systemctl", "start", "opsd.service"); err != nil {
				return fmt.Errorf("控制端未就绪：%w", err)
			}
			if err := Command(ctx, "systemctl", "start", "opsd-agent.service"); err != nil {
				return fmt.Errorf("控制端已启动，本机 Agent 未完成接入：%w", err)
			}
			return nil
		},
	}
}

func ControlOwnership(instance localstate.Instance, instancePath string) error {
	value, err := user.Lookup("opsd")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(value.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(value.Gid)
	if err != nil {
		return err
	}
	for _, path := range []string{instance.DataDir, filepath.Join(instance.DataDir, "control.db"), filepath.Join(instance.DataDir, "control.db-wal"), filepath.Join(instance.DataDir, "control.db-shm")} {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if info.IsDir() {
			mode = 0700
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
	}
	if err := os.Chown(instancePath, 0, gid); err != nil {
		return err
	}
	if err := os.Chmod(instancePath, 0640); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(instancePath), 0750); err != nil {
		return err
	}
	return os.Chown(filepath.Dir(instancePath), 0, gid)
}

func RegisterAgent(dataDir, binary string) error {
	unit := "[Unit]\nDescription=OPSD 节点\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=notify\nNotifyAccess=main\nUser=root\nUMask=0077\nExecStart=" + quote(binary) + " run --data-dir " + quote(dataDir) + "\nRestart=on-failure\nRestartSec=3\nRestartPreventExitStatus=2\nTimeoutStartSec=60\n\n[Install]\nWantedBy=multi-user.target\n"
	return os.WriteFile("/etc/systemd/system/opsd-agent.service", []byte(unit), 0644)
}
