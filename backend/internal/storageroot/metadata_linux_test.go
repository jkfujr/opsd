package storageroot

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestTrustedStoragePathOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("目录所有权验收需要 root")
	}
	dir, err := os.MkdirTemp("/srv", "opsd-storage-path-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := trustedDir(dir); err != nil {
		t.Fatal("合法的绝对目录被拒绝", err)
	}
	for name, mode := range map[string]os.FileMode{"foreign-owner": 0700, "group-writable": 0770} {
		path := filepath.Join(dir, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if name == "foreign-owner" {
			if err := os.Chown(path, 65534, 0); err != nil {
				t.Fatal(err)
			}
		}
		if err := trustedDir(filepath.Join(path, "child")); err == nil {
			t.Fatal("接受了不受信任的父目录", name)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := trustedDir(filepath.Join(link, "child")); err == nil {
		t.Fatal("接受了含符号链接的绝对目录")
	}
}

func TestCompletedMetadataRollbackOnSystemd(t *testing.T) {
	bin := os.Getenv("OPSD_TEST_BINARY")
	if os.Getenv("OPSD_TEST_STORAGE_ROOT") != "1" || bin == "" || os.Geteuid() != 0 {
		t.Skip("需要隔离宿主上的正式 Linux 工件与 systemd")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	run := func(name string, args ...string) string {
		t.Helper()
		start := time.Now()
		out, err := output(ctx, name, args...)
		t.Logf("%s %v · %s · %s", name, args, time.Since(start).Round(time.Millisecond), out)
		must(err)
		return out
	}
	lab, err := os.MkdirTemp("/srv", "opsd-i06-rollback-")
	must(err)
	t.Cleanup(func() { _ = os.RemoveAll(lab) })
	must(os.Chmod(lab, 0755))
	name := filepath.Base(lab)
	priorSource, priorTarget, priorServices, priorUnits := metadataSource, metadataTarget, metadataServices, metadataUnits
	metadataSource = metadataInstance{DataDir: filepath.Join(lab, "current", "node"), ExecutorDir: filepath.Join(lab, "current", "executor")}
	metadataTarget = metadataInstance{DataDir: filepath.Join(lab, "legacy", "node"), ExecutorDir: filepath.Join(lab, "legacy", "executor")}
	metadataServices = []string{name + "-executor.service", name + ".service"}
	metadataUnits = append(append([]string{}, metadataServices...), name+"-operation@.service", name+"-recover.service")
	t.Cleanup(func() {
		tidy, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_, _ = output(tidy, "systemctl", append([]string{"stop"}, metadataServices...)...)
		for _, unit := range metadataUnits {
			_ = os.Remove(metadataDrop(unit))
			_ = os.Remove(filepath.Dir(metadataDrop(unit)))
			_ = os.Remove(filepath.Join(unitDirectory, unit))
		}
		_, _ = output(tidy, "systemctl", "daemon-reload")
		metadataSource, metadataTarget, metadataServices, metadataUnits = priorSource, priorTarget, priorServices, priorUnits
	})
	for _, p := range []string{filepath.Dir(metadataSource.DataDir), filepath.Dir(metadataTarget.DataDir), filepath.Join(lab, "config")} {
		must(os.Mkdir(p, 0755))
	}
	must(os.Mkdir(metadataSource.ExecutorDir, 0700))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	addr := ln.Addr().String()
	must(ln.Close())
	n, err := cluster.Open(ctx, filepath.Join(metadataSource.DataDir, "node.db"), cluster.Options{
		Config: cluster.Config{Listen: addr, Address: "http://" + addr}, Username: "admin", Password: "test-password", Entrance: "/signin",
	})
	must(err)
	id := n.ID
	must(n.Close())
	db, err := storage.Open(ctx, filepath.Join(metadataSource.ExecutorDir, "executor.db"), storage.Executor)
	must(err)
	must(db.Close())
	for _, name := range []string{"node.db", "executor.db"} {
		root := metadataSource.DataDir
		if name == "executor.db" {
			root = metadataSource.ExecutorDir
		}
		d, e := storage.Open(ctx, filepath.Join(root, name), map[string]storage.Kind{"node.db": storage.Node, "executor.db": storage.Executor}[name])
		must(e)
		_, e = d.ExecContext(ctx, "CREATE TABLE i3_replay (id INTEGER PRIMARY KEY, value TEXT NOT NULL)")
		must(e)
		_, e = d.ExecContext(ctx, "INSERT INTO i3_replay VALUES (1,'保留旧数据')")
		must(e)
		must(d.Close())
	}
	must(filepath.WalkDir(metadataSource.DataDir, func(p string, _ os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		return os.Chown(p, 65534, 65534)
	}))
	run("cp", "-a", metadataSource.DataDir, metadataTarget.DataDir)
	run("cp", "-a", metadataSource.ExecutorDir, metadataTarget.ExecutorDir)
	control := filepath.Join(lab, "control")
	must(os.Mkdir(control, 0700))
	must(os.Chown(control, 65534, 65534))
	uid := 65534
	f := installation.NodeFile{DataDir: metadataSource.DataDir, ExecutorDir: metadataSource.ExecutorDir, Binary: bin,
		Socket: filepath.Join(lab, "executor.sock"), ControlSocket: filepath.Join(control, "node.sock"), GroupID: uid, UserID: &uid}
	config := filepath.Join(lab, "config", "instance.json")
	must(localstate.Write(config, f, 0644))
	m := Manager{Config: config, NodeDir: f.DataDir, ExecutorDir: f.ExecutorDir}
	const recordID = "completed-i06"
	dir := filepath.Join(m.stateDir(), recordID)
	must(os.MkdirAll(dir, 0700))
	old := f
	old.DataDir, old.ExecutorDir = metadataTarget.DataDir, metadataTarget.ExecutorDir
	must(localstate.Write(filepath.Join(dir, "instance.before.json"), old, 0600))
	must(localstate.Write(filepath.Join(dir, "result.json"), map[string]string{"status": "migrated"}, 0600))
	added := []string{}
	for i, unit := range metadataUnits {
		file, drop := filepath.Join(unitDirectory, unit), metadataDrop(unit)
		body := "[Service]\nType=oneshot\nExecStart=/usr/bin/true\n"
		extra := "[Unit]\nRequiresMountsFor=" + metadataSource.ExecutorDir + "\n# Requires=opsd-storage-check.service\n"
		if i == 0 {
			body = fmt.Sprintf("[Service]\nType=notify\nExecStart=%s executor --config %s\n", unitArg(bin), unitArg(config))
		} else if i == 1 {
			body = fmt.Sprintf("[Service]\nType=notify\nUser=nobody\nGroup=nogroup\nReadWritePaths=%s\nExecStart=%s node --config %s\n",
				metadataTarget.DataDir, unitArg(bin), unitArg(config))
			extra += "\n[Service]\nReadWritePaths=\nReadWritePaths=" + metadataSource.DataDir + "\n"
		}
		must(os.WriteFile(file, []byte(body), 0644))
		must(os.Mkdir(filepath.Dir(drop), 0755))
		must(os.WriteFile(drop, []byte(extra), 0644))
		added = append(added, drop)
	}
	must(localstate.Write(filepath.Join(dir, "preflight.json"), map[string]any{"node_id": id, "added_files": added}, 0600))
	run("systemctl", "daemon-reload")
	run("systemctl", append([]string{"start"}, metadataServices...)...)
	for _, p := range []string{filepath.Join(f.DataDir, "node.db"), filepath.Join(f.ExecutorDir, "executor.db")} {
		role := storage.Node
		if filepath.Base(p) == "executor.db" {
			role = storage.Executor
		}
		d, err := storage.Open(ctx, p, role)
		must(err)
		_, err = d.ExecContext(ctx, "INSERT INTO i3_replay VALUES (2,'迁移之后的新数据')")
		must(err)
		must(d.Close())
	}
	for _, p := range []string{filepath.Join(f.DataDir, "themes"), filepath.Join(f.ExecutorDir, "logs")} {
		must(os.MkdirAll(p, 0700))
		must(os.WriteFile(filepath.Join(p, "i3-record"), []byte("新文件\n"), 0600))
	}
	available, err := m.LegacyRecord(recordID)
	must(err)
	if !available {
		t.Fatal("已完成的 I0.6 记录未找到")
	}
	start := time.Now()
	out, err := m.MetadataRollback(ctx, recordID, false)
	t.Logf("MetadataRollback %s · %s · %+v", recordID, time.Since(start).Round(time.Millisecond), out)
	must(err)
	v := out.(metadataRollback)
	if v.Phase != "rolled_back" || !v.WriteIntent {
		t.Fatal(v)
	}
	for _, item := range []struct {
		path string
		want int
	}{
		{filepath.Join(metadataTarget.DataDir, "node.db"), 2}, {filepath.Join(metadataTarget.ExecutorDir, "executor.db"), 2},
		{filepath.Join(metadataSource.DataDir, "node.db"), 2}, {filepath.Join(metadataSource.ExecutorDir, "executor.db"), 2},
		{filepath.Join(metadataTarget.DataDir+".pre-rollback-"+recordID, "node.db"), 1},
	} {
		d, e := storage.ReadOnly(ctx, item.path)
		must(e)
		var count int
		var last string
		must(d.QueryRowContext(ctx, "SELECT count(*), max(value) FROM i3_replay").Scan(&count, &last))
		must(d.Close())
		t.Logf("读取 %s · count=%d · %s", item.path, count, last)
		if count != item.want {
			t.Fatalf("回退数据行数不符：%s", item.path)
		}
	}
	for _, p := range []string{filepath.Join(metadataTarget.DataDir, "themes"), filepath.Join(metadataTarget.ExecutorDir, "logs")} {
		body, err := os.ReadFile(filepath.Join(p, "i3-record"))
		must(err)
		if string(body) != "新文件\n" {
			t.Fatal("新文件未同步", p)
		}
	}
	run("systemctl", append([]string{"is-active"}, metadataServices...)...)
}
