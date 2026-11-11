//go:build !linux

package firewall

import (
	"os"
	"os/exec"
)

func configureCommand(cmd *exec.Cmd)                      {}
func preserveOwner(file *os.File, info os.FileInfo) error { return nil }
