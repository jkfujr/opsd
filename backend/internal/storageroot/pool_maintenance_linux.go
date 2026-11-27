package storageroot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
)

// 同一个池的修复、证书维护和搬盘共享 etcd 租约。
func (m *Manager) lockPoolMaintenance(ctx context.Context, d poolNodeRecord, owner string) (context.Context, func(), error) {
	material, err := readPoolMaterial(filepath.Join(m.poolDir(d.PoolID), "runtime"))
	if err != nil {
		return ctx, nil, err
	}
	tls, _, err := storagepool.VerifyMaterial(material, d.Member.PrivateIP, time.Now())
	if err != nil {
		return ctx, nil, err
	}
	transport := &http.Transport{TLSClientConfig: tls}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	_, list, _ := poolMasters(d.Members)
	endpoints := strings.Split(list, ",")
	post := func(ctx context.Context, path string, in, out any) error {
		body, err := json.Marshal(in)
		if err != nil {
			return err
		}
		for _, endpoint := range endpoints {
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(body))
			if reqErr != nil {
				return reqErr
			}
			req.Header.Set("Content-Type", "application/json")
			res, reqErr := client.Do(req)
			if reqErr == nil {
				if res.StatusCode == http.StatusOK {
					reqErr = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(out)
				} else {
					reqErr = fmt.Errorf("etcd 返回 HTTP %d", res.StatusCode)
				}
				_ = res.Body.Close()
			}
			if reqErr == nil {
				return nil
			}
			err = reqErr
		}
		return err
	}
	var lease struct{ ID, TTL string }
	if err := post(ctx, "/v3/lease/grant", map[string]string{"TTL": "60"}, &lease); err != nil || lease.ID == "" || lease.ID == "0" {
		transport.CloseIdleConnections()
		return ctx, nil, fault("dependency_unavailable", "无法取得池维护租约：%v", err)
	}
	revoke := func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var out json.RawMessage
		_ = post(release, "/v3/lease/revoke", map[string]string{"ID": lease.ID}, &out)
		transport.CloseIdleConnections()
	}
	key := base64.StdEncoding.EncodeToString([]byte("\x00opsd/maintenance/" + d.PoolID))
	value := base64.StdEncoding.EncodeToString([]byte(owner))
	txn := map[string]any{
		"compare": []map[string]string{{"key": key, "target": "CREATE", "createRevision": "0"}},
		"success": []map[string]any{{"requestPut": map[string]string{"key": key, "value": value, "lease": lease.ID}}},
	}
	var result struct{ Succeeded bool }
	if err := post(ctx, "/v3/kv/txn", txn, &result); err != nil || !result.Succeeded {
		// 写应答可能丢失，按本次租约读回同一个锁。
		var read struct{ Kvs []struct{ Lease string } }
		if readErr := post(ctx, "/v3/kv/range", map[string]string{"key": key}, &read); readErr != nil || len(read.Kvs) != 1 || read.Kvs[0].Lease != lease.ID {
			revoke()
			if err != nil || readErr != nil {
				return ctx, nil, fault("dependency_unavailable", "无法核对池维护锁")
			}
			return ctx, nil, fault("node_busy", "同池已有维护或迁移正在执行")
		}
	}
	held, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-held.Done():
				return
			case <-ticker.C:
				var keep struct{ Result struct{ TTL string } }
				if err := post(held, "/v3/lease/keepalive", map[string]string{"ID": lease.ID}, &keep); err != nil || keep.Result.TTL == "" || keep.Result.TTL == "0" {
					cancel()
					return
				}
			}
		}
	}()
	return held, func() { cancel(); <-done; revoke() }, nil
}

func (m *Manager) repairPoolNode(ctx context.Context, d poolNodeRecord) error {
	if d.Phase != "serving" {
		return fault("conflict", "存储池节点尚未完成首次部署")
	}
	states, err := m.auditPoolControl(ctx, d, true, false)
	if err != nil {
		return err
	}
	filer := ""
	for _, s := range states {
		if strings.HasSuffix(s.Name, "-filer") && s.Running {
			filer = s.ID
		}
	}
	if filer == "" {
		return fault("dependency_unavailable", "池 Filer 不可用于修复")
	}
	masters, _, _ := poolMasters(d.Members)
	command := exec.CommandContext(ctx, "docker", "exec", "-i", filer, "timeout", "22s", "weed", "shell",
		"-master="+masters, "-filer="+storagepool.Address(d.Member.PrivateIP, d.Member.Ports.Filer),
		"-filerGroup="+d.PoolID, "-connectTimeout=5s")
	command.Stdin = strings.NewReader("lock\nvolume.fix.replication -apply -doDelete=false -retry=1 -maxParallelization=1\nunlock\n")
	body, err := command.CombinedOutput()
	if err != nil {
		return fault("dependency_unavailable", "池副本修复未完成：%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (m *Manager) lockPoolMigration(ctx context.Context, r record) (context.Context, func(), error) {
	if r.From.Component != protocol.StoragePoolData {
		return ctx, func() {}, nil
	}
	d, err := m.readPoolNode(r.From.ComponentID)
	if err != nil {
		// I3 的独立单卷部署记录不使用分布式池控制面。
		if errors.Is(err, os.ErrNotExist) {
			return ctx, func() {}, nil
		}
		return ctx, nil, err
	}
	return m.lockPoolMaintenance(ctx, d, r.ID)
}
