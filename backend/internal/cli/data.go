package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/protocol"
)

func dataCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：opsd data init | status | recover")
	}
	action := args[0]
	if action != "init" && action != "status" && action != "recover" {
		return fmt.Errorf("数据命令无效")
	}
	set := command.Flags("data " + action)
	path := set.String("config", command.DefaultInstance, "")
	id := set.String("cluster-id", "", "")
	if err := command.Parse(set, args[1:]); err != nil {
		return err
	}
	f, err := installation.NodePaths(*path)
	if err != nil {
		return err
	}
	ctx, cancel := processContext()
	defer cancel()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", f.ControlSocket)
	}}, Timeout: 30 * time.Second}
	method, url := "GET", "http://node/shared"
	var body io.Reader
	if action == "init" {
		if *id == "" {
			return fmt.Errorf("请提供 --cluster-id")
		}
		method = "POST"
		url += "/initialize"
		data, _ := json.Marshal(cluster.SharedInitialize{ClusterID: *id, Database: protocol.SharedDatabaseName})
		body = bytes.NewReader(data)
	} else if action == "recover" {
		method = "POST"
		url += "/recover"
	}
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("数据操作未完成：%s", bytes.TrimSpace(data))
	}
	if action == "init" {
		var op cluster.Operation
		if err = json.Unmarshal(data, &op); err != nil {
			return err
		}
		fmt.Printf("操作：%s\n", op.ID)
	} else {
		fmt.Println(string(bytes.TrimSpace(data)))
	}
	return nil
}
