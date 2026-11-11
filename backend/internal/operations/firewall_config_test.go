package operations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type managedPolicyHost struct {
	mu           sync.Mutex
	files        map[string][]byte
	runtime      []byte
	writes       int
	fail         bool
	afterRuntime func()
}

func newManagedPolicyHost(active bool) *managedPolicyHost {
	enabled := "no"
	if active {
		enabled = "yes"
	}
	return &managedPolicyHost{files: map[string][]byte{
		"/etc/ufw/ufw.conf":   []byte("ENABLED=" + enabled + "\n"),
		"/etc/default/ufw":    []byte("IPV6=no\nDEFAULT_INPUT_POLICY=DROP\n"),
		"/etc/ufw/user.rules": []byte("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n### END RULES ###\nCOMMIT\n"),
	}, runtime: []byte("*filter\n:INPUT DROP [0:0]\n:ufw-user-input - [0:0]\n-A ufw-skip-to-policy-input -j DROP\nCOMMIT\n")}
}
func (h *managedPolicyHost) Read(path string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if b, ok := h.files[path]; ok {
		return bytes.Clone(b), nil
	}
	return nil, os.ErrNotExist
}
func (h *managedPolicyHost) Exists(path string) bool { _, err := h.Read(path); return err == nil }
func (*managedPolicyHost) LookPath(string) bool      { return false }
func (h *managedPolicyHost) Replace(path string, before, after []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !bytes.Equal(h.files[path], before) {
		return fmt.Errorf("文件已改变")
	}
	if h.fail {
		return fmt.Errorf("写入失败")
	}
	h.files[path] = bytes.Clone(after)
	h.writes++
	return nil
}
func (h *managedPolicyHost) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name == "iptables-save" {
		return bytes.Clone(h.runtime), nil
	}
	return nil, fmt.Errorf("未知命令：%s", name)
}
func (h *managedPolicyHost) RunInput(_ context.Context, b []byte, name string, _ ...string) ([]byte, error) {
	if name != "iptables-restore" {
		return nil, fmt.Errorf("未知命令：%s", name)
	}
	h.mu.Lock()
	if h.fail {
		h.mu.Unlock()
		return nil, fmt.Errorf("写入失败")
	}
	lines := []string{"*filter", ":ufw-user-input - [0:0]"}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, ":INPUT ") || strings.HasPrefix(line, "-A ") {
			lines = append(lines, line)
		}
	}
	h.runtime = []byte(strings.Join(append(lines, "COMMIT", ""), "\n"))
	h.writes++
	callback := h.afterRuntime
	h.mu.Unlock()
	if callback != nil {
		callback()
	}
	return nil, nil
}

func managedAction(t *testing.T, e *Engine, active bool) (*managedPolicyHost, protocol.OperationAction) {
	t.Helper()
	h := newManagedPolicyHost(active)
	e.Manager.Host = h
	first, err := e.InspectFirewall(t.Context(), true)
	if err != nil || first.Target == nil || !first.InSync {
		t.Fatalf("首次导入失败：%+v %v", first, err)
	}
	d, err := firewall.ChangeConfiguration(first.Target.Configuration, protocol.FirewallChange{Kind: "set_default_policy", Action: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	return h, protocol.OperationAction{Kind: "firewall_config", FirewallConfig: &protocol.FirewallTarget{Revision: 2, Configuration: d}}
}

func TestManagedImportPersistsAndOnlyConfirmedTargetCanAutoApply(t *testing.T) {
	e, _, _ := operationFixture(t)
	h, action := managedAction(t, e, false)
	h.files["/etc/default/ufw"] = []byte("IPV6=no\nDEFAULT_INPUT_POLICY=ACCEPT\n")
	again, err := e.InspectFirewall(t.Context(), true)
	if err != nil || again.Target.Configuration.DefaultPolicies[0].Action != "deny" || again.InSync {
		t.Fatalf("重新导入覆盖了确认配置：%+v %v", again, err)
	}
	action.FirewallConfig.Automatic = true
	if _, _, err = e.Store.Receive(t.Context(), "unconfirmed", protocol.ContractVersion, action, "启动一"); firewall.Code(err) != "conflict" {
		t.Fatalf("未确认配置可自动应用：%v", err)
	}
}

func TestManagedConfigurationConfirmationFailureAndRecovery(t *testing.T) {
	for _, mode := range []string{"confirm", "timeout", "interrupted", "failed", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			e, _, clock := operationFixture(t)
			h, action := managedAction(t, e, mode != "inactive")
			accept(t, e, "managed", action)
			if _, created, err := e.Store.Receive(t.Context(), "managed", protocol.ContractVersion, action, "启动一"); err != nil || created {
				t.Fatalf("重复受理：%v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if mode == "failed" {
				h.fail = true
			}
			if mode == "interrupted" {
				h.afterRuntime = cancel
			}
			done := make(chan error, 1)
			go func() { done <- e.Run(ctx, "managed") }()
			if mode == "confirm" || mode == "timeout" {
				for {
					l, err := e.Store.Get(ctx, "managed")
					if err != nil {
						t.Fatal(err)
					}
					if l.Record.State == protocol.AwaitingConfirmation {
						if mode == "confirm" {
							_, err = e.Store.Request(ctx, "managed", "confirm", l.Record.Revision)
							if err != nil {
								t.Fatal(err)
							}
						} else {
							clock.Store(int64(122 * time.Second))
						}
						break
					}
					select {
					case err := <-done:
						t.Fatalf("待确认前退出：%v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
			}
			err := <-done
			if mode == "interrupted" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				h.afterRuntime = nil
				clock.Store(int64(122 * time.Second))
				err = e.Run(t.Context(), "managed")
			}
			if err != nil {
				t.Fatal(err)
			}
			l, err := e.Store.Get(t.Context(), "managed")
			if err != nil {
				t.Fatal(err)
			}
			want, confirmed := protocol.Reverted, uint64(1)
			if mode == "confirm" || mode == "inactive" {
				want, confirmed = protocol.Succeeded, 2
			}
			if mode == "failed" {
				want = protocol.Failed
			}
			if l.Record.State != want {
				t.Fatalf("状态错误：%+v", l.Record)
			}
			saved, err := e.Store.ConfirmedFirewall(t.Context())
			if err != nil || saved.Revision != confirmed {
				t.Fatalf("确认配置错误：%+v %v", saved, err)
			}
			writes := h.writes
			if err = e.Run(t.Context(), "managed"); err != nil || writes != h.writes {
				t.Fatalf("结束操作重复应用：%v", err)
			}
			inspection, err := e.InspectFirewall(t.Context(), false)
			if err != nil || !inspection.InSync {
				t.Fatalf("最终配置不一致：%+v %v", inspection, err)
			}
		})
	}
}
