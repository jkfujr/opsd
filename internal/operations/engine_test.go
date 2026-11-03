package operations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type policyHost struct {
	mu         sync.Mutex
	policy     string
	writes     []string
	afterWrite func()
}

func (h *policyHost) Read(path string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch path {
	case "/etc/ufw/ufw.conf":
		return []byte("ENABLED=yes\n"), nil
	case "/etc/default/ufw":
		return []byte("IPV6=no\nDEFAULT_INPUT_POLICY=" + h.policy + "\n"), nil
	case "/etc/ufw/user.rules":
		return []byte("*filter\n:ufw-user-input - [0:0]\nCOMMIT\n"), nil
	}
	return nil, os.ErrNotExist
}
func (h *policyHost) Exists(path string) bool {
	return path == "/etc/ufw/ufw.conf" || path == "/etc/ufw/user.rules"
}
func (*policyHost) LookPath(string) bool { return false }
func (*policyHost) Replace(string, []byte, []byte) error {
	return fmt.Errorf("本测试不修改文件")
}
func (h *policyHost) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	h.mu.Lock()
	if name == "iptables-save" {
		v := []byte("*filter\n:INPUT " + h.policy + " [0:0]\n:ufw-user-input - [0:0]\nCOMMIT\n")
		h.mu.Unlock()
		return v, nil
	}
	if name == "ufw" && len(args) == 3 && args[0] == "default" {
		h.policy = map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}[args[1]]
		h.writes = append(h.writes, args[1])
		callback := h.afterWrite
		h.mu.Unlock()
		if callback != nil {
			callback()
		}
		return nil, nil
	}
	h.mu.Unlock()
	return nil, fmt.Errorf("夹具未支持：%s %v", name, args)
}
func operationFixture(t *testing.T) (*Engine, *policyHost, *atomic.Int64) {
	t.Helper()
	db, e := storage.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"), storage.Executor)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	h := &policyHost{policy: "DROP"}
	clock := &atomic.Int64{}
	clock.Store(int64(time.Second))
	engine := &Engine{Store: Store{DB: db}, Manager: &firewall.Manager{Host: h}, Clock: func() (string, int64, error) { return "启动一", clock.Load(), nil }}
	return engine, h, clock
}
func policyAction(t *testing.T, e *Engine) protocol.OperationAction {
	t.Helper()
	reload := false
	p, err := e.Manager.Preview(t.Context(), protocol.FirewallChange{Kind: "set_default_policy", Scope: protocol.Both, Family: protocol.IPv4, Action: "allow", Reload: &reload})
	if err != nil || !p.Executable {
		t.Fatalf("预览失败：%+v %v", p, err)
	}
	return protocol.OperationAction{Kind: "firewall", Firewall: &p}
}
func accept(t *testing.T, e *Engine, id string, a protocol.OperationAction) Local {
	t.Helper()
	l, created, err := e.Store.Receive(t.Context(), id, protocol.Version, a, "启动一")
	if err != nil || !created {
		t.Fatalf("受理失败：%v", err)
	}
	return l
}

func TestReceiptIdentityAndSealedLateDelivery(t *testing.T) {
	e, _, _ := operationFixture(t)
	a := policyAction(t, e)
	l := accept(t, e, "op1", a)
	got, created, err := e.Store.Receive(t.Context(), "op1", protocol.Version, a, "启动一")
	if err != nil || created || got.Record.Revision != l.Record.Revision {
		t.Fatalf("重复投递未返回原结果：%+v %v", got, err)
	}
	b := a
	p := *a.Firewall
	p.Change.Action = "deny"
	b.Firewall = &p
	if _, _, err = e.Store.Receive(t.Context(), "op1", protocol.Version, b, "启动一"); firewall.Code(err) != "conflict" {
		t.Fatalf("同标识异参数必须拒绝：%v", err)
	}
	if _, _, err = e.Store.Receive(t.Context(), "op2", protocol.Version, a, "启动一"); !errors.Is(err, ErrBusy) {
		t.Fatalf("节点不能并行写入：%v", err)
	}
	sealed, err := e.Store.Reconcile(t.Context(), "late")
	if err != nil || sealed.Record.State != protocol.Rejected {
		t.Fatal(err)
	}
	got, created, err = e.Store.Receive(t.Context(), "late", protocol.Version, a, "启动一")
	if err != nil || created || got.Action != nil || got.Record.State != protocol.Rejected {
		t.Fatal("封存后晚到的请求不能执行")
	}
}

func TestKeepDecisionDeadlineAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name, boot       string
		now              int64
		stale, reverting bool
		want             string
	}{
		{"期限内", "启动一", 119, false, false, ""}, {"恰好到期", "启动一", 120, false, false, "expired"}, {"主机重启", "启动二", 1, false, false, "expired"}, {"旧修订", "启动一", 119, true, false, "conflict"}, {"已决定撤销", "启动一", 119, false, true, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, _ := operationFixture(t)
			l := accept(t, e, "keep", policyAction(t, e))
			l.Record.State = protocol.AwaitingConfirmation
			l.DeadlineNS = int64(120 * time.Second)
			if tc.reverting {
				l.Record.Decision = "revert"
			}
			if err := e.Store.Save(t.Context(), &l); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Store.Request(t.Context(), "keep", "confirm", l.Record.Revision); err != nil {
				t.Fatal(err)
			}
			revision := l.Record.Revision
			if tc.stale {
				revision--
			}
			_, err := e.Store.Keep(t.Context(), "keep", revision, func() (string, int64, error) { return tc.boot, tc.now * int64(time.Second), nil })
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := e.Store.Get(t.Context(), "keep")
				if got.Record.State != protocol.Succeeded || got.Record.Decision != "keep" {
					t.Fatal("确认决定和成功结果必须一起持久保存")
				}
			} else if tc.want == "expired" && !errors.Is(err, ErrExpired) {
				t.Fatalf("到期未拒绝确认：%v", err)
			} else if tc.want == "conflict" && firewall.Code(err) != "conflict" {
				t.Fatalf("旧阶段未拒绝：%v", err)
			}
		})
	}
}

func TestEngineConfirmationAndRuntimeReadback(t *testing.T) {
	e, h, _ := operationFixture(t)
	accept(t, e, "confirm", policyAction(t, e))
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() { done <- e.Run(ctx, "confirm") }()
	var l Local
	for {
		var err error
		l, err = e.Store.Get(ctx, "confirm")
		if err != nil {
			t.Fatal(err)
		}
		if l.Record.State == protocol.AwaitingConfirmation {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("待确认前退出：%v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	r, err := e.Record(ctx, "confirm")
	if err != nil || r.ConfirmationRemainingSeconds == nil || *r.ConfirmationRemainingSeconds != 120 {
		t.Fatalf("剩余期限读取错误：%+v %v", r, err)
	}
	if _, err = e.Store.Request(ctx, "confirm", "confirm", l.Record.Revision-1); firewall.Code(err) != "conflict" {
		t.Fatal("旧页面确认必须失败")
	}
	if _, err = e.Store.Request(ctx, "confirm", "confirm", l.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	l, _ = e.Store.Get(ctx, "confirm")
	if l.Record.State != protocol.Succeeded || l.Record.Decision != "keep" {
		t.Fatalf("确认未完成：%+v", l.Record)
	}
	if len(h.writes) != 1 {
		t.Fatalf("确认不能重发原生写入：%v", h.writes)
	}
}

func TestInterruptedWriteExpiresAndRestores(t *testing.T) {
	for _, reboot := range []bool{false, true} {
		t.Run(fmt.Sprint(reboot), func(t *testing.T) {
			e, h, clock := operationFixture(t)
			accept(t, e, "interrupted", policyAction(t, e))
			ctx, cancel := context.WithCancel(t.Context())
			h.afterWrite = cancel
			if err := e.Run(ctx, "interrupted"); !errors.Is(err, context.Canceled) {
				t.Fatalf("中断不能假完成：%v", err)
			}
			l, _ := e.Store.Get(t.Context(), "interrupted")
			if l.IntentStep == nil || l.Record.State != protocol.Applying {
				t.Fatal("原生副作用后必须保留未完成意图")
			}
			h.afterWrite = nil
			if reboot {
				e.Clock = func() (string, int64, error) { return "启动二", int64(time.Second), nil }
			} else {
				clock.Store(int64(122 * time.Second))
			}
			if err := e.Run(t.Context(), "interrupted"); err != nil {
				t.Fatal(err)
			}
			l, _ = e.Store.Get(t.Context(), "interrupted")
			if l.Record.State != protocol.Reverted || strings.Join(h.writes, ",") != "allow,deny" {
				t.Fatalf("恢复应读回并撤销，不继续正向重放：%+v %v", l.Record, h.writes)
			}
		})
	}
}

func TestInterruptedBeforeWriteAndThirdValue(t *testing.T) {
	for _, third := range []bool{false, true} {
		t.Run(fmt.Sprint(third), func(t *testing.T) {
			e, h, _ := operationFixture(t)
			a := policyAction(t, e)
			l := accept(t, e, "intent", a)
			adapter, _ := e.Manager.Adapter("ufw")
			recipe, err := adapter.Prepare(t.Context(), *a.Firewall)
			if err != nil {
				t.Fatal(err)
			}
			index := 0
			l.Recipe = recipe
			l.IntentStep = &index
			l.Record.State = protocol.Applying
			l.Record.Steps = []protocol.OperationStep{{Name: recipe[0].Name, State: "applying"}}
			l.DeadlineNS = int64(121 * time.Second)
			if err = e.Store.Save(t.Context(), &l); err != nil {
				t.Fatal(err)
			}
			if third {
				h.policy = "EXTERNAL"
			}
			if err = e.Run(t.Context(), "intent"); err != nil {
				t.Fatal(err)
			}
			l, _ = e.Store.Get(t.Context(), "intent")
			want := protocol.Failed
			if third {
				want = protocol.AttentionRequired
			}
			if l.Record.State != want || len(h.writes) != 0 {
				t.Fatalf("不可盲重放或覆盖第三值：%+v %v", l.Record, h.writes)
			}
			if third {
				if _, _, err = e.Store.Receive(t.Context(), "blocked", protocol.Version, a, "启动一"); !errors.Is(err, ErrBusy) {
					t.Fatal("需处理操作仍应阻止新写入")
				}
				if _, err = e.Store.Request(t.Context(), "intent", "resolve", l.Record.Revision-1); firewall.Code(err) != "conflict" {
					t.Fatal("旧修订不能人工结束")
				}
			}
		})
	}
}
