package executor

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
)

func TestCouchDBReadinessUsesLocalAddress(t *testing.T) {
	for _, host := range []string{"127.0.0.5", "::1"} {
		t.Run(host, func(t *testing.T) {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				remote, _, _ := net.SplitHostPort(r.RemoteAddr)
				if remote != host || r.Method != http.MethodGet || r.URL.Path != "/_up" {
					t.Errorf("就绪检查的来源或路径无效：%s %s %s", remote, r.Method, r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
			}))
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			defer server.Close()
			endpoint, err := storageEndpoint(cluster.StorageConfig{Address: host, Port: listener.Addr().(*net.TCPAddr).Port})
			if err != nil {
				t.Fatal(err)
			}
			if err := waitDatabase(t.Context(), endpoint); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := storageEndpoint(cluster.StorageConfig{Address: "192.0.2.10", Port: 5984}); err == nil {
		t.Fatal("接受了非本机地址")
	}
}

func TestCouchDBReadinessRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_up" {
			t.Error("就绪检查跟随了重定向")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/ready", http.StatusFound)
	}))
	defer server.Close()
	if err := waitDatabase(t.Context(), server.Listener.Addr().(*net.TCPAddr)); err == nil {
		t.Fatal("重定向被认作数据库已就绪")
	}
}

func TestManagedCouchDBContainer(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("OPSD_TEST_DOCKER") != "1" {
		t.Skip("未启用本地 Docker 验收")
	}
	if os.Geteuid() != 0 {
		t.Fatal("本机执行器验收须以 root 运行")
	}
	s := &Server{Directory: t.TempDir(), Host: firewall.NativeHost{}}
	config := cluster.StorageConfig{Enabled: true, Address: "127.0.0.5", Port: 5994}
	monitor, e := net.Listen("tcp", net.JoinHostPort(config.Address, "9100"))
	if e != nil {
		t.Fatal(e)
	}
	defer monitor.Close()
	defer func() {
		out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=opsd.storage="+s.Directory).Output()
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("docker", "stop", id).Run()
			_ = exec.Command("docker", "rm", id).Run()
		}
	}()
	credentials, e := s.storage(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	if credentials.Username == "" || credentials.Password == "" {
		t.Fatal("没有生成独立数据库凭据")
	}
	if conn, e := net.Dial("tcp", "127.0.0.1:5994"); e == nil {
		conn.Close()
		t.Fatal("数据库绑定到了其他地址")
	}
	again, e := s.storage(t.Context(), config)
	if e != nil || again != credentials {
		t.Fatal("重复启用改变了数据库身份", e)
	}
	path := filepath.Join(s.Directory, "couchdb.json")
	var before, restarted storageIdentity
	if e := localstate.Read(path, &before); e != nil {
		t.Fatal(e)
	}
	disabled := config
	disabled.Enabled = false
	if _, e = s.storage(t.Context(), disabled); e != nil {
		t.Fatal(e)
	}
	if got, err := s.storage(t.Context(), config); err != nil || got != credentials {
		t.Fatal("原容器重启未恢复数据库", err)
	}
	if e := localstate.Read(path, &restarted); e != nil || restarted.FullID != before.FullID {
		t.Fatal("重启改变了完整容器 ID", e)
	}
	if _, e = s.storage(t.Context(), disabled); e != nil {
		t.Fatal(e)
	}
	config.Port = 5995
	if _, e = s.storage(t.Context(), config); e != nil {
		t.Fatal("停用后不能调整端口", e)
	}
}
