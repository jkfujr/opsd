package installation

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jkfujr/opsd/internal/storage"
)

//go:embed storage-check.sh
var storageCheckScript string

var storageMountUUID = regexp.MustCompile(`^[0-9a-zA-Z-]{1,128}$`)

func (f NodeFile) usesMetadataRoot() bool {
	return f.DataDir == storage.NodeDir && f.ExecutorDir == storage.ExecutorDir
}

func (f NodeFile) storageCheckPath() string {
	return filepath.Join(filepath.Dir(f.Binary), "opsd-storage-check")
}

func checkStorage(ctx context.Context, mode, uuid string) (string, error) {
	command := exec.CommandContext(ctx, "/bin/sh", "-s", "--", mode, uuid)
	command.Stdin = strings.NewReader(strings.ReplaceAll(storageCheckScript, "\r\n", "\n"))
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("存储根自检失败：%s（%w）", strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (f NodeFile) CheckStorage(ctx context.Context) error {
	if !f.usesMetadataRoot() {
		return nil
	}
	_, err := checkStorage(ctx, "check", f.StorageMountUUID)
	return err
}

func (f NodeFile) installStorageCheck() error {
	path := f.storageCheckPath()
	temp, err := os.CreateTemp(filepath.Dir(path), ".opsd-storage-check-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.WriteString(strings.ReplaceAll(storageCheckScript, "\r\n", "\n"))
	if err == nil {
		err = temp.Chmod(0755)
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp.Name(), path)
}
