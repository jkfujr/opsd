package firewall

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

// 原生命令只在本测试创建的网络命名空间执行；持久文件写入临时目录。
type trustNativeHost struct {
	NativeHost
	directory, backend string
}

func (h trustNativeHost) path(path string) string {
	return filepath.Join(h.directory, strings.TrimPrefix(path, "/"))
}
func (h trustNativeHost) Read(path string) ([]byte, error) { return os.ReadFile(h.path(path)) }
func (h trustNativeHost) Replace(path string, before, after []byte) error {
	return h.NativeHost.Replace(h.path(path), before, after)
}
func (h trustNativeHost) Exists(path string) bool { _, e := os.Stat(h.path(path)); return e == nil }
func (h trustNativeHost) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "systemctl" {
		if len(args) > 0 && args[0] == "show" {
			return []byte("/usr/sbin/nft -f /etc/nftables.conf"), nil
		}
		return []byte("active"), nil
	}
	if name == "firewall-cmd" {
		return nil, fmt.Errorf("未启动")
	}
	if name == "firewall-offline-cmd" {
		args = append([]string{"--system-config=" + h.path("/etc/firewalld")}, args...)
	}
	return h.NativeHost.Run(ctx, name, args...)
}

func TestPeerTrustNativeNetwork(t *testing.T) {
	if os.Getenv("OPSD_NATIVE_TRUST") != "1" {
		t.Skip("按需在 Linux CI 验证原生规则")
	}
	if os.Getenv("OPSD_TRUST_CHILD") != "1" {
		binary, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(t.Context(), "unshare", "--net", binary, "-test.run=^TestPeerTrustNativeNetwork$", "-test.v")
		cmd.Env = append(os.Environ(), "OPSD_TRUST_CHILD=1")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("隔离流量验收失败：%v\n%s", e, b)
		} else {
			t.Log(string(b))
		}
		return
	}
	h := trustNativeHost{directory: t.TempDir()}
	write := func(path, text string) {
		t.Helper()
		path = h.path(path)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	run := func(name string, args ...string) {
		t.Helper()
		if b, e := h.NativeHost.Run(t.Context(), name, args...); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	run("ip", "link", "set", "lo", "up")
	for _, address := range []string{"8.8.8.8/32", "8.8.4.4/32", "9.9.9.9/32", "2606:4700::1111/128", "2606:4700::1001/128", "2606:4700::9999/128"} {
		run("ip", "address", "add", address, "dev", "lo", "nodad")
	}
	check := func(results []protocol.ScopeResult) {
		t.Helper()
		for _, r := range results {
			if r.State != "in_sync" && r.State != "inactive" {
				t.Fatalf("应用失败：%+v", r)
			}
		}
	}
	for _, backend := range []string{"ufw", "nftables"} {
		t.Run(backend, func(t *testing.T) {
			run("nft", "flush", "ruleset")
			externalBefore := map[protocol.AddressFamily]string{}
			externalRules := func(text string) string {
				lines := []string{}
				for _, line := range strings.Split(text, "\n") {
					if strings.HasPrefix(line, "-A OPSD_EXTERNAL ") || strings.HasPrefix(line, "-A INPUT ") && !strings.Contains(line, trustMarker) {
						lines = append(lines, line)
					}
				}
				return strings.Join(lines, "\n")
			}
			if backend == "nftables" {
				config := "table inet filter {\n chain input {\n  type filter hook input priority 0; policy drop;\n  ct state established,related accept\n  tcp dport 22 accept comment \"existing\"\n }\n chain output {\n  type filter hook output priority 0;\n  policy accept;\n }\n}\n"
				write("/etc/nftables.conf", config)
				run("nft", "-f", h.path("/etc/nftables.conf"))
			} else {
				write("/etc/ufw/ufw.conf", "ENABLED=yes\n")
				write("/etc/default/ufw", "IPV6=yes\n")
				for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
					prefix, restore, path := "ufw", "iptables-restore", "/etc/ufw/before.rules"
					if family == protocol.IPv6 {
						prefix, restore, path = "ufw6", "ip6tables-restore", "/etc/ufw/before6.rules"
					}
					config := "*filter\n:INPUT DROP [0:0]\n:FORWARD ACCEPT [0:0]\n:OUTPUT ACCEPT [0:0]\n:OPSD_EXTERNAL - [0:0]\n:" + prefix + "-before-input - [0:0]\n-A INPUT -j OPSD_EXTERNAL\n-A INPUT -j " + prefix + "-before-input\n-A OPSD_EXTERNAL -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN\n-A OPSD_EXTERNAL -p tcp --dport 22 -j RETURN\n-A OPSD_EXTERNAL -p tcp -j REJECT\n-A OPSD_EXTERNAL -j RETURN\n-A " + prefix + "-before-input -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT\n-A " + prefix + "-before-input -p tcp --dport 22 -j ACCEPT\nCOMMIT\n"
					persistent := []string{}
					for _, line := range strings.Split(config, "\n") {
						if strings.HasPrefix(line, ":INPUT ") || strings.HasPrefix(line, ":FORWARD ") || strings.HasPrefix(line, ":OUTPUT ") || strings.HasPrefix(line, ":OPSD_EXTERNAL ") || strings.HasPrefix(line, "-A INPUT ") || strings.HasPrefix(line, "-A OPSD_EXTERNAL ") {
							continue
						}
						persistent = append(persistent, line)
					}
					write(path, strings.Join(persistent, "\n"))
					if _, e := h.RunInput(t.Context(), []byte(config), restore); e != nil {
						t.Fatal(e)
					}
					raw, e := h.Run(t.Context(), strings.Replace(restore, "-restore", "-save", 1), "-t", "filter")
					if e != nil {
						t.Fatal(e)
					}
					externalBefore[family] = externalRules(string(raw))
				}
			}
			sync := func(target protocol.PeerTrustTarget) {
				if backend == "ufw" {
					check(syncUFWTrust(t.Context(), h, target))
				} else {
					check(syncNFTTrust(t.Context(), h, target))
				}
			}
			connection := func(source, destination string, want bool) {
				t.Helper()
				network := "tcp4"
				if strings.Contains(source, ":") {
					network = "tcp6"
				}
				listener, e := net.Listen(network, net.JoinHostPort(destination, "0"))
				if e != nil {
					t.Fatal(e)
				}
				defer listener.Close()
				go func() {
					c, e := listener.Accept()
					if e == nil {
						c.Close()
					}
				}()
				d := net.Dialer{Timeout: 300 * time.Millisecond, LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}}
				c, e := d.DialContext(t.Context(), network, listener.Addr().String())
				if c != nil {
					c.Close()
				}
				if (e == nil) != want {
					t.Fatalf("新连接 %s→%s：%v，期望放行=%v", source, destination, e, want)
				}
			}
			probe := func(want bool) {
				connection("8.8.8.8", "8.8.4.4", want)
				connection("9.9.9.9", "8.8.4.4", false)
				connection("2606:4700::1111", "2606:4700::1001", want)
				connection("2606:4700::9999", "2606:4700::1001", false)
			}
			probe(false)
			target := protocol.PeerTrustTarget{Enabled: true, Revision: 1, IPv4: []string{"8.8.8.8"}, IPv6: []string{"2606:4700::1111"}}
			sync(target)
			probe(true)
			sync(target)
			probe(true)
			if backend == "nftables" {
				run("nft", "flush", "ruleset")
				run("nft", "-f", h.path("/etc/nftables.conf"))
				probe(true)
			} else {
				checkHooks := func(enabled bool) {
					t.Helper()
					for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
						save := "iptables-save"
						if family == protocol.IPv6 {
							save = "ip6tables-save"
						}
						raw, e := h.Run(t.Context(), save, "-t", "filter")
						if e != nil {
							t.Fatal(e)
						}
						_, hooks, exists, e := readUFWTrustRuntime(string(raw), family)
						if e != nil || enabled && !ufwTrustHooksCurrent(string(raw), family, hooks) || !enabled && (exists || len(hooks) > 0) {
							t.Fatalf("互信跳转不符：%v\n%s", e, raw)
						}
						if externalRules(string(raw)) != externalBefore[family] {
							t.Fatalf("外部规则被改变：\n%s", raw)
						}
					}
				}
				checkHooks(true)
				for _, command := range []string{"iptables", "ip6tables"} {
					run(command, "-D", "INPUT", "-j", "OPSD_EXTERNAL")
					run(command, "-I", "INPUT", "1", "-j", "OPSD_EXTERNAL")
					run(command, "-I", "INPUT", "1", "-m", "comment", "--comment", trustMarker, "-j", trustChain)
				}
				sync(target)
				checkHooks(true)
				probe(true)
				for _, file := range []struct{ path, restore string }{{"/etc/ufw/before.rules", "iptables-restore"}, {"/etc/ufw/before6.rules", "ip6tables-restore"}} {
					before, e := h.Read(file.path)
					if e != nil {
						t.Fatal(e)
					}
					if e = inputRules(h, t.Context(), string(before), file.restore, "--noflush"); e != nil {
						t.Fatal(e)
					}
				}
				sync(target)
				checkHooks(true)
				probe(true)
				replacement := protocol.PeerTrustTarget{Enabled: true, Revision: 2, IPv4: []string{"9.9.9.9"}, IPv6: []string{"2606:4700::9999"}}
				sync(replacement)
				connection("8.8.8.8", "8.8.4.4", false)
				connection("9.9.9.9", "8.8.4.4", true)
				connection("2606:4700::1111", "2606:4700::1001", false)
				connection("2606:4700::9999", "2606:4700::1001", true)
				write("/etc/ufw/ufw.conf", "ENABLED=no\n")
				sync(replacement)
				checkHooks(false)
				connection("9.9.9.9", "8.8.4.4", false)
				before, e := h.Read("/etc/ufw/before.rules")
				if e != nil || !strings.Contains(string(before), "-s 9.9.9.9 -j ACCEPT") {
					t.Fatal("停用时未保留互信持久配置", e)
				}
				write("/etc/ufw/ufw.conf", "ENABLED=yes\n")
				sync(target)
				checkHooks(true)
				probe(true)
			}
			sync(protocol.PeerTrustTarget{Revision: 3})
			probe(false)
		})
	}
	write("/etc/firewalld/firewalld.conf", "DefaultZone=public\n")
	target := protocol.PeerTrustTarget{Enabled: true, IPv4: []string{"8.8.8.8"}, IPv6: []string{"2606:4700::1111"}}
	check(syncFirewalldTrust(t.Context(), h, target))
	check(syncFirewalldTrust(t.Context(), h, protocol.PeerTrustTarget{}))
	got, _, e := readFirewalldTrust(t.Context(), h, false, true)
	if e != nil || len(got[protocol.IPv4])+len(got[protocol.IPv6]) != 0 {
		t.Fatalf("firewalld 撤除失败：%v %+v", e, got)
	}
}
