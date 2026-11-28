package databaseengine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func deploymentExample() protocol.DatabaseDeployment {
	return protocol.DatabaseDeployment{ID: "example", Kind: "galera", Image: GaleraImage, Directory: "/var/lib/opsd/databases/example", Address: "192.0.2.10", SQLPort: 3306, ReplicationPort: 4567, ISTPort: 4568, SSTPort: 4444, ClusterName: "example", Seeds: []string{"192.0.2.10:4567"}, Bootstrap: true, InstallDocker: true, SSTMethod: "mariabackup", SSTUser: "opsd_sst", SSTPassword: "sync-secret", Connection: protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "root", Password: "admin-secret"}}
}

func deploymentTestDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("部署目录验收需要 Linux root")
	}
	dir, err := os.MkdirTemp("/srv", "opsd-database-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func TestDeploymentKeepsExistingFilesAndBootstrapIntent(t *testing.T) {
	d := deploymentExample()
	d.Directory = filepath.Join(deploymentTestDir(t), "database")
	if err := writeDeploymentFiles(d); err != nil {
		t.Fatal(err)
	}
	if ok, err := deploymentFilesMatch(d); err != nil || !ok {
		t.Fatal("部署文件未读回", err)
	}
	start, err := os.ReadFile(filepath.Join(d.Directory, "start.sh"))
	if err != nil || strings.Count(string(start), "--wsrep-new-cluster") != 1 || !strings.Contains(string(start), "set -C;") {
		t.Fatal("首次启动缺少持久意图", err)
	}
	data := filepath.Join(d.Directory, "data", "existing")
	if err = os.WriteFile(data, []byte("保留业务数据"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = writeDeploymentFiles(d); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(data); err != nil || string(b) != "保留业务数据" {
		t.Fatal("重入改变了已有数据", err)
	}
	config := filepath.Join(d.Directory, "database.cnf")
	if err = os.WriteFile(config, []byte("外部配置"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = writeDeploymentFiles(d); err == nil {
		t.Fatal("覆盖了外部配置")
	}
	d.Directory = filepath.Join(deploymentTestDir(t), "occupied")
	if err = os.Mkdir(d.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(d.Directory, "existing"), []byte("保留"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = writeDeploymentFiles(d); err == nil {
		t.Fatal("接受了已有数据目录")
	}
}

func TestDeploymentValidation(t *testing.T) {
	for name, mutate := range map[string]func(*protocol.DatabaseDeployment){
		"浮动镜像":  func(d *protocol.DatabaseDeployment) { d.Image = "mariadb:latest" },
		"非本机凭据": func(d *protocol.DatabaseDeployment) { d.Connection.Address = "192.0.2.10:3306" },
		"重复端口":  func(d *protocol.DatabaseDeployment) { d.ISTPort = d.SQLPort },
		"空集群身份": func(d *protocol.DatabaseDeployment) { d.Bootstrap = false },
		"目录逃逸":  func(d *protocol.DatabaseDeployment) { d.Directory = "/var/lib/../etc" },
		"证书路径":  func(d *protocol.DatabaseDeployment) { d.TLS = map[string]string{"../external": "value"} },
	} {
		t.Run(name, func(t *testing.T) {
			d := deploymentExample()
			mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("接受了无效部署参数")
			}
		})
	}
}

func TestDeploymentPreviewSkipsRejectedDirectory(t *testing.T) {
	directory := deploymentTestDir(t)
	if err := os.WriteFile(filepath.Join(directory, "existing"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(deploymentTestDir(t), "alias")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	m := Manager{Docker: &dockerengine.Manager{Socket: filepath.Join(t.TempDir(), "docker.sock")}}
	d := deploymentExample()
	for _, path := range []string{directory, link} {
		d.Directory = path
		result, err := m.CheckDeployment(t.Context(), d)
		if err != nil {
			t.Fatal(err)
		}
		problems := strings.Join(result.Problems, "\n")
		if strings.Contains(problems, "数据库目录非空") != (path == directory) || strings.Contains(problems, "目录包含链接") != (path == link) {
			t.Fatal("部署预览目录检查结果不符", problems)
		}
	}
}

func TestDeploymentRejectsUnsafePathsOnRecovery(t *testing.T) {
	root := deploymentTestDir(t)
	d := deploymentExample()
	d.Directory = filepath.Join(root, "custom", "database")
	if err := writeDeploymentFiles(d); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(d.Directory, "data", "keep")
	if err := os.WriteFile(data, []byte("保留"), 0600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(d.Directory)
	for _, perms := range []os.FileMode{0770, 0777} {
		if err := os.Chmod(parent, perms); err != nil {
			t.Fatal(err)
		}
		if err := writeDeploymentFiles(d); err == nil {
			t.Fatalf("接受了可被外部用户写入的父目录：%o", perms)
		}
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, err := deploymentFilesMatch(d); err == nil {
		t.Fatal("接受了非 root 父目录")
	}
	if err := os.Chown(parent, 0, 0); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(d.Directory, "database.cnf")
	if err := os.Rename(config, config+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(config+".original", config); err != nil {
		t.Fatal(err)
	}
	if _, err := deploymentFilesMatch(d); err == nil {
		t.Fatal("接受了配置文件链接")
	}
	if err := dockerengine.VerifyDeploymentMounts(d, ""); err == nil {
		t.Fatal("接受了 Docker 绑定来源链接")
	}
	if err := os.Rename(config+".original", config); err != nil {
		t.Fatal(err)
	}
	if err := writeDeploymentFiles(d); err != nil {
		t.Fatal("原部署恢复失败", err)
	}
	if got, err := os.ReadFile(data); err != nil || string(got) != "保留" {
		t.Fatal("原业务数据被改变", err)
	}
}
