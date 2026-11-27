package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
	"github.com/jkfujr/opsd/internal/storageroot"
)

func configureStorageRoots(s *executor.Services, config string, f installation.NodeFile) {
	host := &storageroot.Manager{Config: config, Binary: f.Binary, NodeDir: f.DataDir, ExecutorDir: f.ExecutorDir, Store: s.Engine.Store, Docker: dockerengine.New()}
	s.StorageRoots = host
	s.Docker.StorageBinding = host.Directory
	s.Docker.StorageArchivePrepare = host.PrepareArchive
	s.Docker.StorageArchiveApply = host.ApplyArchive
	s.Docker.StorageDatabaseStart = host.StartDatabase
}

func storageRootCommand(args []string) error {
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd storage-root status | apply | recover | rollback --id <申请ID>")
	}
	action := args[0]
	set := command.Flags("storage-root " + action)
	config := set.String("config", command.DefaultInstance, "")
	id := set.String("id", "", "")
	mode := set.String("mode", "", "")
	component := set.String("component", "", "")
	componentID := set.String("component-id", "", "")
	if err := command.Parse(set, args[1:]); err != nil {
		return err
	}
	unitCommand := slices.Contains([]string{"component-check", "component-start", "component-stop"}, action)
	if unitCommand {
		if *id != "" || *mode != "" || !operations.ValidID(*componentID) ||
			!slices.Contains([]string{protocol.StorageArchive, protocol.StorageDatabase, protocol.StoragePoolData}, *component) {
			return fmt.Errorf("组件启动参数无效")
		}
	} else if !slices.Contains([]string{"status", "apply", "recover", "rollback"}, action) ||
		!operations.ValidID(*id) || *component != "" || *componentID != "" ||
		action == "recover" && *mode != "repair-target" && *mode != "reverse" || action != "recover" && *mode != "" {
		return fmt.Errorf("存储根命令、申请标识或恢复模式无效")
	}
	f, err := installation.NodePaths(*config)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(*config)
	if err != nil {
		return err
	}
	ctx, cancel := processContext()
	defer cancel()
	host := &storageroot.Manager{Config: absolute, Binary: f.Binary, NodeDir: f.DataDir, ExecutorDir: f.ExecutorDir, Docker: dockerengine.New()}
	if action == "status" || action == "rollback" {
		legacy, err := host.LegacyRecord(*id)
		if err != nil {
			return err
		}
		if legacy {
			out, err := host.MetadataRollback(ctx, *id, action == "status")
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
	}
	db, err := storage.ReadOnly(ctx, filepath.Join(f.ExecutorDir, "executor.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	host.Store = operations.Store{DB: db}
	if unitCommand {
		return host.Component(ctx, action, *component, *componentID)
	}
	if action != "status" {
		if err = host.Run(ctx, *id, action, *mode); err != nil {
			return err
		}
	}
	out, err := host.Status(ctx, *id)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
