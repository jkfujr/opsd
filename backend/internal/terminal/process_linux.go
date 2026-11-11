//go:build linux

package terminal

import (
	"github.com/creack/pty"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func startShell() (*os.File, *exec.Cmd, error) {
	shell := "/bin/bash"
	if _, e := os.Stat(shell); e != nil {
		shell = "/bin/sh"
	}
	command := exec.Command(shell, "-l")
	command.Dir = "/root"
	command.Env = append(os.Environ(), "TERM=xterm-256color", "HOME=/root", "USER=root", "LOGNAME=root")
	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	return file, command, err
}

// 同一 PTY 的作业可建立自己的进程组，按会话 ID 关闭全部作业。
func stopShell(pid int) {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		child, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		body, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		end := strings.LastIndex(string(body), ")")
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(body)[end+1:])
		if len(fields) > 3 && fields[3] == strconv.Itoa(pid) {
			_ = syscall.Kill(child, syscall.SIGKILL)
		}
	}
}
