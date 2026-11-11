// 隔离的本地 HTTP 验收服务，数据和主题只写入根目录的 artifacts。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/control"
	"github.com/jkfujr/opsd/internal/webassets"
)

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return err
	}
	var project struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(data, &project); err != nil {
		return err
	}
	artifacts := filepath.Join(root, "artifacts", "frontend", "gpt2")
	if err = os.MkdirAll(artifacts, 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(artifacts, "local-console-")
	if err != nil {
		return err
	}
	n, err := cluster.Open(ctx, filepath.Join(dir, "node.db"), cluster.Options{Username: "admin", Password: "test-password", Entrance: "/signin", Config: cluster.Config{Name: "本地验收节点", Console: true, API: true, Region: "本地"}})
	if err != nil {
		return err
	}
	defer n.Close()
	n.Start()
	s, err := control.New(ctx, n.DB, control.Options{Node: n, Assets: webassets.Assets(), NodeVersion: project.Version, FrontendVersion: webassets.Version(), ThemeDir: filepath.Join(dir, "themes"), EnforceListenerState: true})
	if err != nil {
		return err
	}
	defer s.Close()
	web := &http.Server{Addr: "127.0.0.1:5183", Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- web.ListenAndServe() }()
	fmt.Printf("http://127.0.0.1:5183 node=%s data=%s\n", n.ID, dir)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return web.Shutdown(shutdown)
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
