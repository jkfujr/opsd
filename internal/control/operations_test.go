package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/firewall"
	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/operations"
	"github.com/opsd-labs/opsd/internal/storage"
)

// 原生命令使用受控替身；HTTP、Noise、SQLite、Agent 入口与持久操作均走实际实现。
type firewallPolicyHost struct {
	policy atomic.Bool
	writes atomic.Int64
}

func (h *firewallPolicyHost) Read(path string) ([]byte, error) {
	switch path {
	case "/etc/ufw/ufw.conf":
		return []byte("ENABLED=yes\n"), nil
	case "/etc/default/ufw":
		p := "DROP"
		if h.policy.Load() {
			p = "ACCEPT"
		}
		return []byte("IPV6=no\nDEFAULT_INPUT_POLICY=" + p + "\n"), nil
	case "/etc/ufw/user.rules":
		return []byte("*filter\n:ufw-user-input - [0:0]\nCOMMIT\n"), nil
	}
	return nil, os.ErrNotExist
}
func (h *firewallPolicyHost) Exists(path string) bool {
	return path == "/etc/ufw/ufw.conf" || path == "/etc/ufw/user.rules"
}
func (*firewallPolicyHost) LookPath(string) bool { return false }
func (*firewallPolicyHost) Replace(string, []byte, []byte) error {
	return fmt.Errorf("测试不修改原生文件")
}
func (h *firewallPolicyHost) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "iptables-save" {
		p := "DROP"
		if h.policy.Load() {
			p = "ACCEPT"
		}
		return []byte("*filter\n:INPUT " + p + " [0:0]\n:ufw-user-input - [0:0]\nCOMMIT\n"), nil
	}
	if name == "ufw" && len(args) == 4 && args[1] == "default" {
		h.policy.Store(args[2] == "allow")
		h.writes.Add(1)
		return nil, nil
	}
	return nil, fmt.Errorf("测试命令未定义：%s %v", name, args)
}

func TestFirewallHTTPNoiseOperationAndReconnect(t *testing.T) {
	f := setup(t)
	session := f.login(f.client, "/secret-test", "admin", "test-password")
	token := f.token(session.CsrfToken)
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"), storage.Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = agent.Initialize(t.Context(), db, f.server.URL, identity.Encode(f.service.key.Public), token.Token); err != nil {
		t.Fatal(err)
	}
	h := &firewallPolicyHost{}
	engine := &operations.Engine{Store: operations.Store{DB: db}, Manager: &firewall.Manager{Host: h}, Clock: func() (string, int64, error) { return "测试启动", int64(time.Second), nil }}
	workerCtx, stopWorkers := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	var mu sync.Mutex
	running := map[string]bool{}
	ctx, stopAgent := context.WithCancel(t.Context())
	var lostReply atomic.Bool
	services := &agent.Services{Engine: engine, Start: func(_ context.Context, id string) error {
		mu.Lock()
		defer mu.Unlock()
		if running[id] {
			return nil
		}
		running[id] = true
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { mu.Lock(); delete(running, id); mu.Unlock() }()
			engine.Run(workerCtx, id)
		}()
		if lostReply.CompareAndSwap(false, true) {
			stopAgent()
		}
		return nil
	}}
	defer func() { stopWorkers(); workers.Wait() }()
	agentDone := make(chan error, 1)
	go func() { agentDone <- agent.RunWithServices(ctx, db, nil, services) }()
	agentRunning := true
	defer func() {
		if agentRunning {
			stopAgent()
			<-agentDone
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	nodeID := ""
	for {
		state, e := agent.Load(t.Context(), db)
		if e == nil && state.NodeID != "" {
			nodeID = state.NodeID
			c := f.service.currentConnection(nodeID)
			if c != nil {
				f.service.mu.Lock()
				ready := c.ready
				f.service.mu.Unlock()
				if ready {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("真实 Agent 未就绪")
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, data := f.request(f.client, "GET", "/api/v1/nodes/"+nodeID+"/firewall", "", "", false)
	expect(t, response, data, 200)
	var state httpapi.FirewallState
	if err = json.Unmarshal(data, &state); err != nil || state.Backend != "ufw" || !state.Complete {
		t.Fatalf("原生状态未传至 HTTP：%s %v", data, err)
	}
	body := `{"kind":"set_default_policy","scope":"both","family":"ipv4","action":"allow","reload":false}`
	response, data = f.request(f.client, "POST", "/api/v1/nodes/"+nodeID+"/firewall/plans", body, session.CsrfToken, true)
	expect(t, response, data, 201)
	var plan httpapi.ChangePlan
	if err = json.Unmarshal(data, &plan); err != nil || !plan.Executable || !plan.RequiresConfirmation {
		t.Fatalf("预览契约错误：%s %v", data, err)
	}
	response, data = f.request(f.client, "POST", "/api/v1/firewall/plans/"+plan.Id+"/apply", "", session.CsrfToken, true)
	expect(t, response, data, 202)
	var operation httpapi.Operation
	if err = json.Unmarshal(data, &operation); err != nil {
		t.Fatal(err)
	}
	id := operation.Id
	response, data = f.request(f.client, "POST", "/api/v1/firewall/plans/"+plan.Id+"/apply", "", session.CsrfToken, true)
	expect(t, response, data, 202)
	var repeated httpapi.Operation
	json.Unmarshal(data, &repeated)
	if repeated.Id != id {
		t.Fatal("同预览重复应用创建了第二个操作")
	}
	// Agent 已持久受理，但在提交应答发出前断开；独立执行不随通信取消。
	select {
	case <-agentDone:
		agentRunning = false
	case <-time.After(5 * time.Second):
		t.Fatal("未注入提交应答丢失")
	}
	deadline = time.Now().Add(3 * time.Second)
	for f.service.currentConnection(nodeID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("离线连接未清除")
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, data = f.request(f.client, "GET", "/api/v1/operations/"+id, "", "", false)
	expect(t, response, data, 200)
	json.Unmarshal(data, &operation)
	if !operation.ResultPendingSync || operation.State == "failed" {
		t.Fatalf("应答丢失只能报告待同步：%s", data)
	}
	response, data = f.request(f.client, "POST", "/api/v1/firewall/plans/"+plan.Id+"/apply", "", session.CsrfToken, true)
	expect(t, response, data, 202)
	json.Unmarshal(data, &repeated)
	if repeated.Id != id {
		t.Fatal("离线重复应用必须定位原操作")
	}
	ctx, stopAgent = context.WithCancel(t.Context())
	agentRunning = true
	go func() { agentDone <- agent.RunWithServices(ctx, db, nil, services) }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		c := f.service.currentConnection(nodeID)
		ready := false
		if c != nil {
			f.service.mu.Lock()
			ready = c.ready
			f.service.mu.Unlock()
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("重连未完成双方未决核对")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		response, data = f.request(f.client, "GET", "/api/v1/operations/"+id, "", "", false)
		expect(t, response, data, 200)
		json.Unmarshal(data, &operation)
		if operation.State == "awaiting_confirmation" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("未进入待确认：%s", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if operation.ConfirmationRemainingSeconds == nil || *operation.ConfirmationRemainingSeconds != 120 {
		t.Fatalf("HTTP 未传递 Agent 剩余期限：%s", data)
	}
	var second httpapi.ChangePlan
	secondBody := `{"kind":"set_default_policy","scope":"both","family":"ipv4","action":"deny","reload":false}`
	response, data = f.request(f.client, "POST", "/api/v1/nodes/"+nodeID+"/firewall/plans", secondBody, session.CsrfToken, true)
	expect(t, response, data, 201)
	json.Unmarshal(data, &second)
	response, data = f.request(f.client, "POST", "/api/v1/firewall/plans/"+second.Id+"/apply", "", session.CsrfToken, true)
	expect(t, response, data, 409)
	response, data = f.request(f.client, "POST", "/api/v1/operations/"+id+"/confirm", fmt.Sprintf(`{"expected_revision":%d}`, operation.Revision-1), session.CsrfToken, true)
	expect(t, response, data, 409)
	if operation.ResultPendingSync {
		t.Fatalf("重连后未取得原操作：%s", data)
	}
	response, data = f.request(f.client, "POST", "/api/v1/operations/"+id+"/confirm", fmt.Sprintf(`{"expected_revision":%d}`, operation.Revision), session.CsrfToken, true)
	expect(t, response, data, 202)
	deadline = time.Now().Add(5 * time.Second)
	for {
		response, data = f.request(f.client, "GET", "/api/v1/operations/"+id, "", "", false)
		expect(t, response, data, 200)
		json.Unmarshal(data, &operation)
		if operation.State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("确认结果未同步：%s", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h.writes.Load() != 1 {
		t.Fatalf("重复受理、重连或确认重放了原生动作：%d", h.writes.Load())
	}
	// 原生配置在预览后被其他管理者改变时，HTTP 受理前返回冲突。
	h.policy.Store(false)
	response, data = f.request(f.client, "POST", "/api/v1/firewall/plans/"+second.Id+"/apply", "", session.CsrfToken, true)
	expect(t, response, data, 409)
	var prior sql.NullString
	if err = f.db.QueryRow("SELECT operation_id FROM firewall_plans WHERE id=?", second.Id).Scan(&prior); err != nil || prior.Valid {
		t.Fatalf("冲突不能占用写入名额：%v", err)
	}
	response, data = f.request(f.client, "GET", "/api/v1/operations?node_id="+nodeID, "", "", false)
	expect(t, response, data, 200)
	var history httpapi.OperationList
	json.Unmarshal(data, &history)
	if len(history.Items) != 1 || history.Items[0].Id != id {
		t.Fatalf("历史未保留原操作：%s", data)
	}
}
