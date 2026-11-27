//go:build !linux

package cli

import (
	"fmt"

	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/installation"
)

func storageRootCommand([]string) error { return fmt.Errorf("存储根迁移需要 Linux") }

func configureStorageRoots(*executor.Services, string, installation.NodeFile) {}
