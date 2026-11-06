package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/storage"
)

func network(args []string) error {
	if err := installation.RequireLinuxRoot(); err != nil {
		return err
	}
	set := command.Flags("network")
	config := set.String("config", command.DefaultInstance, "")
	listen := set.String("listen", "", "")
	mode := set.String("address-mode", "", "")
	address := set.String("address", "", "")
	if err := command.Parse(set, args); err != nil {
		return err
	}
	if *listen == "" && *mode == "" && *address == "" {
		return fmt.Errorf("请提供监听地址、地址模式或通信 URL")
	}
	f, err := installation.NodePaths(*config)
	if err != nil {
		return err
	}
	unlock, err := localstate.Lock(filepath.Join(f.DataDir, "node.lock"))
	if err != nil {
		return fmt.Errorf("请先停止 opsd.service")
	}
	defer unlock()
	executorUnlock, err := localstate.Lock(filepath.Join(f.ExecutorDir, "operation.lock"))
	if err != nil {
		return err
	}
	defer executorUnlock()
	ctx, cancel := processContext()
	defer cancel()
	for _, path := range []string{filepath.Join(f.DataDir, "node.db"), filepath.Join(f.ExecutorDir, "executor.db")} {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	db, err := storage.ReadOnly(ctx, filepath.Join(f.ExecutorDir, "executor.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	var pending int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE terminal=0").Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fmt.Errorf("执行器存在未结束操作")
	}
	n, err := cluster.Open(ctx, filepath.Join(f.DataDir, "node.db"), cluster.Options{})
	if err != nil {
		return err
	}
	defer n.Close()
	if err = n.ConfigureNetwork(ctx, *listen, *mode, *address); err != nil {
		return err
	}
	fmt.Println("网络配置已保存，请启动 opsd.service")
	return nil
}
