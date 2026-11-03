package installation

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jkfujr/opsd/internal/localstate"
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
