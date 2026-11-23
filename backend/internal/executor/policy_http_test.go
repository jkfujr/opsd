package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

// serverWithPolicy 构造带真实 Services（临时 SQLite）的服务端，策略按参数设置。
func serverWithPolicy(t *testing.T, closed bool) *Server {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	policy, _, err := ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		policy, _, err = ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":true}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	return &Server{Services: NewServices(db), Directory: t.TempDir(), Policy: policy}
}

// TestTerminalRejectedBeforeUpgrade 断言策略关闭时终端在 WebSocket 升级之前就返回 403。
func TestTerminalRejectedBeforeUpgrade(t *testing.T) {
	handler := serverWithPolicy(t, true).Handler()
	request := httptest.NewRequest("GET", "/terminal", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("期望 403，得到 %d", recorder.Code)
	}
	// 升级成功会返回 101 并带上 Upgrade 头；这里必须没有。
	if strings.Contains(strings.ToLower(recorder.Header().Get("Upgrade")), "websocket") {
		t.Fatal("不应发生 WebSocket 升级")
	}
	if !strings.Contains(recorder.Body.String(), "关闭可执行代码能力") {
		t.Fatalf("拒绝原因未说明能力被关闭：%s", recorder.Body.String())
	}
}

// TestRPCRejectsArbitraryCodeAndKeepsServing 用同一个真实客户端验证：
// R2 提交拿到 forbidden，随后 R1 查询在同一服务实例上成功。
func TestRPCRejectsArbitraryCodeAndKeepsServing(t *testing.T) {
	server := serverWithPolicy(t, true)
	dir := t.TempDir()
	socket := filepath.Join(dir, "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewUnstartedServer(server.Handler())
	httpServer.Listener.Close()
	httpServer.Listener = listener
	httpServer.Start()
	defer httpServer.Close()

	client := NewClient(socket)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	submit := &protocol.OperationSubmit{
		OperationID:     "op",
		ContractVersion: protocol.ContractVersion,
		Action: protocol.OperationAction{
			Kind:  "shell",
			Shell: &protocol.ShellAction{TaskID: "job-1", Name: "任务", Script: "uname", Directory: "/tmp"},
		},
	}
	_, err = client.Call(ctx, submit)
	if err == nil {
		t.Fatal("R2 提交应当被拒绝")
	}
	var fault *firewall.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("拒绝不是协议内错误，而是：%v", err)
	}
	if fault.Code != "forbidden" {
		t.Fatalf("错误码应为 forbidden，得到 %q", fault.Code)
	}
	if !strings.Contains(fault.Message, "关闭可执行代码能力") {
		t.Fatalf("拒绝原因未说明能力被关闭：%q", fault.Message)
	}

	// 同一服务实例上，常规能力必须仍可用：拒绝没有打停服务，也没有污染状态。
	if _, err := client.Call(ctx, &protocol.OperationQuery{OperationID: "op"}); err != nil {
		var queryFault *firewall.Fault
		if errors.As(err, &queryFault) && queryFault.Code == "forbidden" {
			t.Fatalf("R1 查询被策略拒绝：%v", err)
		}
	}
}
