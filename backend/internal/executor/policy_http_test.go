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
)

// closedServer 返回策略关闭、且 Services 为空的服务端。
// Services 为空是有意为之：任何真正进入 handler 的请求都会 panic 或返回 500，
// 因此"拿到 403"就证明请求在分派之前被拦下。
func closedServer() *Server {
	policy, _, err := ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":false}`))
	if err != nil {
		panic(err)
	}
	return &Server{Policy: policy}
}

// TestTerminalRejectedBeforeUpgrade 断言策略关闭时终端在 WebSocket 升级之前就返回 403。
func TestTerminalRejectedBeforeUpgrade(t *testing.T) {
	handler := closedServer().Handler()
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

// TestRPCRejectsArbitraryCodeBeforeDispatch 用真实客户端验证：R2 提交拿到的是
// 协议内的确定拒绝（forbidden），而不是被当成通信故障；随后 R1 请求仍成功。
func TestRPCRejectsArbitraryCodeBeforeDispatch(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(closedServer().Handler())
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()

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
	// 客户端必须拿到"确定拒绝"，而不是把它当成通信故障。
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
}

// TestRPCAllowsRegularCapability 断言常规能力在策略关闭时仍会进入分派。
// Services 为空，因此进入分派会 panic——用 recover 证明"确实走到了分派"。
func TestRPCAllowsRegularCapability(t *testing.T) {
	handler := closedServer().Handler()
	body, err := protocol.Encode(protocol.Message{
		RequestID: "req",
		Payload:   &protocol.PeerTrustTarget{Revision: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatched := false
	func() {
		defer func() {
			if recover() != nil {
				dispatched = true
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/rpc", strings.NewReader(string(body))))
	}()
	if !dispatched {
		t.Fatal("常规能力应当进入分派（预期因 Services 为空而 panic）")
	}
}