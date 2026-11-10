//go:build !linux

package terminal

import (
	"fmt"
	"os"
	"os/exec"
)

func startShell() (*os.File, *exec.Cmd, error) {
	return nil, nil, fmt.Errorf("网页终端仅支持 Linux")
}
func stopShell(int) {}
