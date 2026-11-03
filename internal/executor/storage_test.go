package executor

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

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
	credentials, e := s.storage(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = exec.Command("docker", "stop", "opsd-couchdb").Run()
		_ = exec.Command("docker", "rm", "-v", "opsd-couchdb").Run()
	}()
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
	disabled := config
	disabled.Enabled = false
	if _, e = s.storage(t.Context(), disabled); e != nil {
		t.Fatal(e)
	}
	config.Port = 5995
	if _, e = s.storage(t.Context(), config); e != nil {
		t.Fatal("停用后不能调整端口", e)
	}
}
