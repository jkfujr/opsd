//go:build linux

package dockerengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerInstallationReusesExistingAndStopsOnFailure(t *testing.T) {
	if _, err := DockerInstallationOS(); err != nil {
		t.Skip("此检查使用 Debian 或 Ubuntu 的软件包入口")
	}
	for _, mode := range []string{"missing", "existing", "install_failure"} {
		t.Run(mode, func(t *testing.T) {
			bin := t.TempDir()
			log := filepath.Join(t.TempDir(), "commands")
			t.Setenv("PATH", bin)
			t.Setenv("OPSD_INSTALL_TEST_LOG", log)
			for _, name := range []string{"apt-get", "systemctl"} {
				script := "#!/bin/sh\nprintf '%s\\n' '" + name + " '\"$*\" >> \"$OPSD_INSTALL_TEST_LOG\"\n"
				if mode == "install_failure" && name == "apt-get" {
					script += "if [ \"$1\" = install ]; then exit 7; fi\n"
				}
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "existing" {
				if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			err := InstallDocker(t.Context())
			data, _ := os.ReadFile(log)
			calls := string(data)
			switch mode {
			case "existing":
				if err == nil || calls != "" {
					t.Fatal("已有 Docker 触发了安装或服务修改", err, calls)
				}
			case "install_failure":
				if err == nil || strings.Contains(calls, "systemctl") {
					t.Fatal("安装失败后继续修改服务", err, calls)
				}
			case "missing":
				if err != nil || calls != "apt-get update\napt-get install -y --no-install-recommends docker.io\nsystemctl enable --now docker.service\n" {
					t.Fatal("安装顺序错误", err, calls)
				}
			}
		})
	}
}
