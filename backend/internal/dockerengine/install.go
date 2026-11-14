package dockerengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func DockerInstallationOS() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("数据库部署需要 Linux 节点")
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), "\"'")
			if id == "debian" || id == "ubuntu" {
				return id, nil
			}
			return id, fmt.Errorf("此系统请先安装 Docker")
		}
	}
	return "", fmt.Errorf("系统发行版读取失败")
}

// InstallDocker 只在已确认的部署步骤中调用，已有 Docker 不更新软件包或重启服务。
func InstallDocker(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err == nil {
		return fmt.Errorf("Docker 已安装，请检查原服务状态")
	}
	if _, err := DockerInstallationOS(); err != nil {
		return err
	}
	for _, command := range [][]string{{"apt-get", "update"}, {"apt-get", "install", "-y", "--no-install-recommends", "docker.io"}, {"systemctl", "enable", "--now", "docker.service"}} {
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s 执行失败：%w", command[0], err)
		}
	}
	return nil
}
