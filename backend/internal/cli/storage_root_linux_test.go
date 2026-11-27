package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
	"github.com/jkfujr/opsd/internal/storageroot"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestStorageRootDualDisk(t *testing.T) {
	bin := os.Getenv("OPSD_TEST_BINARY")
	if os.Getenv("OPSD_TEST_STORAGE_ROOT") != "1" || bin == "" || os.Geteuid() != 0 {
		t.Skip("需要隔离宿主、双盘和正式 Linux 工件")
	}
	lab, err := os.MkdirTemp("/srv", "opsd-i3-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, disk := range []string{"disk-a", "disk-b"} {
			if exec.Command("mountpoint", "-q", filepath.Join(lab, disk)).Run() == nil {
				t.Errorf("保留仍有挂载的验收目录 %s", lab)
				return
			}
		}
		_ = os.RemoveAll(lab)
	})
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(os.Chmod(lab, 0755))
	command := func(expected bool, fault, name string, args ...string) string {
		t.Helper()
		start := time.Now()
		ctx, cancel := context.WithTimeout(t.Context(), 155*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		if fault != "" {
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(lab, "faults")+":"+os.Getenv("PATH"), "OPSD_I3_FAULT="+fault,
				"OPSD_I3_SYSTEMD="+filepath.Join(lab, "systemd-actions"))
		}
		body, e := cmd.CombinedOutput()
		out := strings.TrimSpace(string(body))
		t.Logf("%s %s · %s · %v · %s", filepath.Base(name), strings.Join(args, " "), time.Since(start).Round(time.Millisecond), e, out)
		if (e == nil) != expected {
			t.Fatalf("退出状态不符：%s %v", name, e)
		}
		return out
	}
	run := func(name string, args ...string) string { return command(true, "", name, args...) }
	mount := func(name string) (string, string) {
		t.Helper()
		p, image := filepath.Join(lab, name), filepath.Join(lab, name+".img")
		must(os.Mkdir(p, 0755))
		run("truncate", "-s", "1664M", image)
		device := run("losetup", "--find", "--show", image)
		t.Cleanup(func() {
			if exec.Command("mountpoint", "-q", p).Run() == nil {
				if body, e := exec.Command("umount", p).CombinedOutput(); e != nil {
					t.Errorf("卸载隔离盘：%v %s", e, body)
					return
				}
			}
			if body, e := exec.Command("losetup", "-d", device).CombinedOutput(); e != nil {
				t.Errorf("分离隔离盘：%v %s", e, body)
			}
		})
		run("mkfs.ext4", "-Fq", device)
		run("mount", "-o", "noatime", device, p)
		run("findmnt", "-no", "TARGET,SOURCE,FSTYPE,UUID", "--mountpoint", p)
		return p, device
	}
	a, aDev := mount("disk-a")
	b, bDev := mount("disk-b")
	namespace := filepath.Base(lab)
	foreign := namespace + "-foreign"
	foreignDir := filepath.Join(lab, "unrelated")
	must(os.Mkdir(foreignDir, 0755))
	run("docker", "run", "-d", "--name", foreign, "--mount", "type=bind,src="+foreignDir+",dst=/proof", "busybox:1.37.0", "sleep", "900")
	t.Cleanup(func() { _, _ = exec.Command("docker", "rm", "-f", foreign).CombinedOutput() })
	inspect := func(container string) string {
		raw := run("docker", "inspect", "--format", "{{.Id}} {{.State.Status}} {{.State.StartedAt}} {{.HostConfig.RestartPolicy.Name}} {{json .Mounts}}", container)
		head, data, _ := strings.Cut(raw, " [")
		var mounts []map[string]any
		must(json.Unmarshal([]byte("["+data), &mounts))
		slices.SortFunc(mounts, func(a, b map[string]any) int {
			return strings.Compare(fmt.Sprint(a["Destination"]), fmt.Sprint(b["Destination"]))
		})
		body, err := json.Marshal(mounts)
		must(err)
		return head + " " + string(body)
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	foreignBefore := inspect(foreign)

	nodeDir, execDir := filepath.Join(lab, "node"), filepath.Join(lab, "executor")
	must(os.Mkdir(execDir, 0700))
	peer, web := freeAddress(t), freeAddress(t)
	n, err := cluster.Open(t.Context(), filepath.Join(nodeDir, "node.db"), cluster.Options{
		Config:   cluster.Config{Listen: peer, Address: "http://" + peer, ConsoleListen: web, Console: true, API: true},
		Username: "admin", Password: "test-password", Entrance: "/signin",
	})
	must(err)
	nodeID := n.ID
	archiveConfig, err := n.ArchiveConfig(t.Context())
	must(err)
	must(n.Close())
	must(filepath.WalkDir(nodeDir, func(p string, _ os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		return os.Chown(p, 65534, 65534)
	}))
	uid := 65534
	file := installation.NodeFile{DataDir: nodeDir, ExecutorDir: execDir, Socket: filepath.Join(lab, "executor.sock"),
		ControlSocket: filepath.Join(nodeDir, "control.sock"), Binary: bin, GroupID: uid, UserID: &uid}
	config := filepath.Join(lab, "config", "instance.json")
	must(os.Mkdir(filepath.Dir(config), 0755))
	must(localstate.Write(config, file, 0644))
	root := launch(t, bin, "executor", "--config", config)
	socket := func(path string) bool {
		conn, err := net.Dial("unix", path)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	waitLocal(t, root, func() bool { return socket(file.Socket) })
	node := launch(t, "runuser", "-u", "nobody", "--", bin, "node", "--config", config)
	waitLocal(t, node, func() bool { return socket(file.ControlSocket) })
	base, path := "http://"+web, "/api/v2/nodes/"+nodeID+"/storage-root"
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	csrf := ""
	httpCall := func(method, resource, tag, key string, body any, expected int, out any) http.Header {
		t.Helper()
		start := time.Now()
		var input io.Reader
		if body != nil {
			data, e := json.Marshal(body)
			must(e)
			input = bytes.NewReader(data)
		}
		req, e := http.NewRequestWithContext(t.Context(), method, base+resource, input)
		must(e)
		req.Header.Set("Origin", base)
		req.Header.Set("Content-Type", "application/json")
		if tag != "" {
			req.Header.Set("If-Match", tag)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, e := client.Do(req)
		must(e)
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		must(e)
		if !strings.Contains(resource, "/auth/") {
			t.Logf("%s %s · %s · %d · %s", method, resource, time.Since(start).Round(time.Millisecond), res.StatusCode, bytes.TrimSpace(raw))
		}
		if res.StatusCode != expected {
			t.Fatalf("HTTP %d，预期 %d，%s", res.StatusCode, expected, raw)
		}
		if out != nil {
			must(json.Unmarshal(raw, out))
		}
		return res.Header.Clone()
	}
	var session httpapi.CurrentSession
	httpCall("POST", "/api/v2/auth/sessions", "", "", map[string]string{"username": "admin", "password": "test-password", "entrance": "/signin"}, 201, &session)
	csrf = session.CsrfToken
	get := func() (protocol.StorageRootView, string) {
		t.Helper()
		var r protocol.StorageRootView
		h := httpCall("GET", path, "", "", nil, 200, &r)
		return r, h.Get("ETag")
	}
	submit := func(key string, in protocol.StorageRootInput) string {
		t.Helper()
		_, etag := get()
		var op cluster.Operation
		h := httpCall("PUT", path, etag, key, in, 202, &op)
		var duplicate cluster.Operation
		httpCall("PUT", path, etag, key, in, 202, &duplicate)
		if op.ID == "" || op.ID != duplicate.ID || h.Get("Location") == "" {
			t.Fatal("原请求未返回同一操作", op, duplicate)
		}
		until := time.Now().Add(8 * time.Second)
		for time.Now().Before(until) {
			httpCall("GET", h.Get("Location"), "", "", nil, 200, &op)
			if op.State == protocol.Succeeded {
				step := "registered"
				if in.Action == "move" {
					step = "pending_restart"
				}
				if len(op.Steps) != 1 || op.Steps[0].State != step {
					t.Fatal("存储操作步骤错误", op)
				}
				return op.ID
			}
			if operations.Terminal(op.State) {
				t.Fatal("存储操作未完成", op)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("存储操作超时", op)
		return ""
	}
	submit("register-b", protocol.StorageRootInput{Action: "register", MountPoint: b, RelativeDir: "volume"})
	view, etag := get()
	var bID string
	for _, r := range view.Roots {
		if r.MountPoint == b && r.State == "ready" && r.UUID != "" && r.Type == "ext4" {
			bID = r.ID
		}
	}
	if bID == "" {
		t.Fatal("未登记原生 ext4")
	}
	httpCall("PUT", path, etag, "bad-abs", protocol.StorageRootInput{Action: "register", MountPoint: b + "/volume"}, 503, nil)
	httpCall("PUT", path, etag, "bad-relative", protocol.StorageRootInput{Action: "register", MountPoint: a, RelativeDir: "../outside"}, 422, nil)
	submit("register-a", protocol.StorageRootInput{Action: "register", MountPoint: a, RelativeDir: "volume"})
	httpCall("PUT", path, etag, "old-etag", protocol.StorageRootInput{Action: "register", MountPoint: a, RelativeDir: "late"}, 412, nil)
	db, err := storage.Open(t.Context(), filepath.Join(execDir, "executor.db"), storage.Executor)
	must(err)
	defer db.Close()
	store := operations.Store{DB: db}
	docker := dockerengine.New()
	port := func() int {
		_, p, e := net.SplitHostPort(freeAddress(t))
		must(e)
		n, e := strconv.Atoi(p)
		must(e)
		return n
	}
	cleanupManaged := func(component, id, name string) {
		h := sha256.Sum256([]byte(component + "/" + id))
		unit := fmt.Sprintf("opsd-storage-root-%x.service", h[:10])
		unitPath := filepath.Join("/etc/systemd/system", unit)
		if _, e := os.Stat(unitPath); e == nil {
			_, _ = exec.Command("systemctl", "stop", unit).CombinedOutput()
			for _, p := range []string{"multi-user.target.wants", "docker.service.wants", ""} {
				_ = os.Remove(filepath.Join("/etc/systemd/system", p, unit))
			}
			_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
		}
		_, _ = exec.Command("docker", "rm", "-f", name).CombinedOutput()
	}
	waitProxy := func(c *sql.DB) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for c.Ping() != nil && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		must(c.Ping())
	}
	writeSQL := func(c *sql.DB, name string) {
		t.Helper()
		waitProxy(c)
		_, e := c.ExecContext(t.Context(), "INSERT INTO mysql_users(username,password,default_hostgroup) VALUES (?,?,10)", name, "op-i3-synthetic")
		must(e)
		_, e = c.ExecContext(t.Context(), "SAVE MYSQL USERS TO DISK")
		must(e)
	}
	type summary struct {
		count, size int
		sha         [32]byte
	}
	readSQL := func(c *sql.DB) summary {
		t.Helper()
		rows, e := c.QueryContext(t.Context(), "SELECT username,password,default_hostgroup FROM disk.mysql_users WHERE username LIKE 'opsd_i3_%' ORDER BY username")
		must(e)
		defer rows.Close()
		var text bytes.Buffer
		count := 0
		for rows.Next() {
			var u, p string
			var hostgroup int
			must(rows.Scan(&u, &p, &hostgroup))
			fmt.Fprintf(&text, "%q,%q,%d\n", u, p, hostgroup)
			count++
		}
		must(rows.Err())
		s := summary{count, text.Len(), sha256.Sum256(text.Bytes())}
		t.Logf("ProxySQL 持久行 · count=%d bytes=%d sha256=%x", s.count, s.size, s.sha)
		return s
	}
	newProxy := func(scenario string) (protocol.DatabaseDeployment, *sql.DB) {
		t.Helper()
		admin, normal := port(), port()
		for normal == admin {
			normal = port()
		}
		d := protocol.DatabaseDeployment{ID: namespace + "-" + scenario, Kind: "proxysql", Image: databaseengine.ProxyImage,
			Address: "127.0.0.1", SQLPort: normal, AdminPort: admin, Backends: []protocol.ProxyEndpoint{{Host: "127.0.0.1", Port: 9}},
			Connection: protocol.DatabaseConnection{Kind: "proxysql", Address: fmt.Sprintf("127.0.0.1:%d", admin), Username: "admin", Password: "opsd-i3-test"},
		}
		d.Directory = filepath.Join(a, "volume", "databases", d.ID)
		action := protocol.DatabaseAction{Kind: "deploy_database", Deployment: &d}
		boot, _, e := operations.SystemClock()
		must(e)
		sqlManager := databaseengine.Manager{Docker: docker}
		for _, step := range []string{"deploy_files", "deploy_create", "deploy_start"} {
			must(sqlManager.Apply(t.Context(), action, step))
		}
		c, e := sql.Open("mysql", "admin:opsd-i3-test@tcp("+d.Connection.Address+")/?timeout=1s&readTimeout=1s&writeTimeout=1s&interpolateParams=true")
		must(e)
		c.SetMaxIdleConns(0)
		t.Cleanup(func() { _ = c.Close() })
		waitProxy(c)
		start, _, e := store.Receive(t.Context(), d.ID, protocol.ContractVersion, protocol.OperationAction{Kind: "database", Database: &action}, boot)
		must(e)
		start.Record.State = protocol.Succeeded
		must(store.Save(t.Context(), &start))
		t.Cleanup(func() { cleanupManaged(protocol.StorageDatabase, d.ID, d.ContainerName()) })
		writeSQL(c, "opsd_i3_before")
		return d, c
	}
	cli := func(ok bool, fault, action, id string, extra ...string) string {
		t.Helper()
		return command(ok, fault, bin, append([]string{"storage-root", action, "--id", id, "--config", config}, extra...)...)
	}
	status := func(id string) storageroot.Status {
		t.Helper()
		var s storageroot.Status
		must(json.Unmarshal([]byte(cli(true, "", "status", id)), &s))
		return s
	}
	move := func(d protocol.DatabaseDeployment) string {
		t.Helper()
		id := submit("move-"+d.ID, protocol.StorageRootInput{Action: "move", Component: protocol.StorageDatabase, ComponentID: d.ID, TargetRootID: bID})
		v, _ := get()
		if v.Pending == nil || v.Pending.ID != id || v.Pending.Phase != "pending_restart" {
			t.Fatal("申请未处于待应用状态", v.Pending)
		}
		return id
	}
	assertBinding := func(d protocol.DatabaseDeployment, s storageroot.Status, phase, directory string) {
		t.Helper()
		if s.Phase != phase || s.Active == nil || s.Active.Directory != directory || len(s.Containers) != 1 || !s.Containers[0].Running {
			t.Fatal("阶段或实际绑定不符", s)
		}
		raw := inspect(d.ContainerName())
		if !strings.Contains(raw, `"Source":"`+directory+`"`) || !strings.Contains(raw, `"Destination":"/var/lib/proxysql"`) {
			t.Fatal("实际 Docker 绑定不符", raw)
		}
	}
	proveWriterOrder := func(d protocol.DatabaseDeployment, oldID string, since time.Time) {
		t.Helper()
		newID := strings.Fields(inspect(d.ContainerName()))[0]
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
		defer cancel()
		args := []string{"events", "--since", since.Add(-time.Minute).UTC().Format(time.RFC3339Nano),
			"--filter", "type=container", "--filter", "label=com.opsd.database-deployment=" + d.ID, "--format", "{{json .}}"}
		t.Logf("docker %s", strings.Join(args, " "))
		watch := exec.CommandContext(ctx, "docker", args...)
		raw, e := watch.StdoutPipe()
		must(e)
		must(watch.Start())
		defer func() { cancel(); _ = watch.Wait() }()
		events := bufio.NewScanner(raw)
		running := ""
		stopped := false
		for events.Scan() {
			var e struct {
				Action   string
				Actor    struct{ ID string }
				TimeNano int64 `json:"timeNano"`
			}
			must(json.Unmarshal(events.Bytes(), &e))
			switch e.Action {
			case "die":
				if running == e.Actor.ID {
					running = ""
					stopped = e.Actor.ID == oldID
					t.Logf("Docker die · id=%s timeNano=%d", e.Actor.ID, e.TimeNano)
				}
			case "start":
				if running != "" {
					t.Fatal("写入方的 Docker 停启事件重叠", e)
				}
				t.Logf("Docker start · id=%s timeNano=%d", e.Actor.ID, e.TimeNano)
				running = e.Actor.ID
				if running == newID && stopped {
					t.Logf("单写入方 · old=%s new=%s", oldID, newID)
					return
				}
			}
		}
		t.Fatal("未观察到旧写入方退出后新写入方启动", running, events.Err())
	}
	must(os.Mkdir(filepath.Join(lab, "faults"), 0700))
	must(os.WriteFile(filepath.Join(lab, "faults", "rsync"), []byte("#!/bin/sh\nif [ \"$OPSD_I3_FAULT\" = copy ]; then echo 'I3 copy interruption'; exit 77; fi\nexec /usr/bin/rsync \"$@\"\n"), 0700))
	must(os.WriteFile(filepath.Join(lab, "faults", "systemctl"), []byte(`#!/bin/sh
printf '%s\n' "$*" >>"$OPSD_I3_SYSTEMD"
case "$OPSD_I3_FAULT:$1:$2" in
  after:start:opsd-storage-root-*) /usr/bin/systemctl "$@" || exit $?; echo 'I3 start reply lost'; exit 79 ;;
  reload:daemon-reload:) exec sleep 13 ;;
esac
exec /usr/bin/systemctl "$@"
`), 0700))

	d, c := newProxy("forward")
	original := filepath.Join(d.Directory, "data")
	target := filepath.Join(b, "volume", "databases", d.ID, "data")
	before := readSQL(c)
	oldContainer := inspect(d.ContainerName())
	id := move(d)
	conflict := namespace + "-conflict"
	run("docker", "run", "--name", conflict, "--mount", "type=bind,src="+filepath.Join(b, "volume")+",dst=/proof,readonly", "busybox:1.37.0", "true")
	t.Cleanup(func() { _, _ = exec.Command("docker", "rm", "-f", conflict).CombinedOutput() })
	conflictBefore := inspect(conflict)
	if out := cli(false, "", "apply", id); !strings.Contains(out, strings.Fields(conflictBefore)[0]) {
		t.Fatal("冲突未返回非受管容器 ID", out)
	}
	if inspect(conflict) != conflictBefore || inspect(d.ContainerName()) != oldContainer {
		t.Fatal("冲突时容器状态发生变化")
	}
	run("docker", "rm", conflict)
	run("umount", b)
	if out := cli(false, "", "apply", id); !strings.Contains(out, "缺失") {
		t.Fatal("未拒绝未挂载目录", out)
	}
	run("mount", "-o", "noatime", aDev, b)
	if out := cli(false, "", "apply", id); !strings.Contains(out, "身份不同") {
		t.Fatal("未拒绝错误 UUID", out)
	}
	run("umount", b)
	run("mount", "-o", "noatime", bDev, b)
	if inspect(d.ContainerName()) != oldContainer {
		t.Fatal("挂载拒绝期间源组件被触碰")
	}
	startMove := time.Now()
	cli(true, "", "apply", id)
	proveWriterOrder(d, strings.Fields(oldContainer)[0], startMove)
	s := status(id)
	assertBinding(d, s, "complete", target)
	if !s.WriteIntent || s.Copied.Files < 1 || s.Copied.Bytes < 1 || len(s.Copied.SHA) != 64 || before != readSQL(c) {
		t.Fatal("正向数据校验失败", s)
	}
	writeSQL(c, "opsd_i3_after")
	after := readSQL(c)
	run("systemctl", "stop", s.Unit)
	run("umount", b)
	unsafeStart := command(false, "", "systemctl", "start", s.Unit)
	t.Logf("运行期缺盘启动拒绝 · %s", unsafeStart)
	ls, e := os.ReadDir(b)
	must(e)
	if len(ls) != 0 || !strings.Contains(inspect(d.ContainerName()), " exited ") {
		t.Fatal("缺盘时容器启动或向系统盘写入", ls)
	}
	run("mount", "-o", "noatime", bDev, b)
	run("systemctl", "start", s.Unit)
	startMove = time.Now()
	cli(true, "", "rollback", id)
	proveWriterOrder(d, s.Containers[0].ID, startMove)
	assertBinding(d, status(id), "rolled_back", original)
	if after.count != before.count+1 || after != readSQL(c) {
		t.Fatal("一键回退遗失新写入")
	}

	p, c2 := newProxy("before")
	id = move(p)
	cli(false, "copy", "apply", id)
	s = status(id)
	assertBinding(p, s, "rolled_back", filepath.Join(p.Directory, "data"))
	if s.WriteIntent || readSQL(c2).count != 1 {
		t.Fatal("开放前自动回退错误", s)
	}

	q, c3 := newProxy("after")
	id = move(q)
	cli(false, "after", "apply", id)
	s = status(id)
	assertBinding(q, s, "needs_recovery", filepath.Join(b, "volume", "databases", q.ID, "data"))
	if !s.WriteIntent {
		t.Fatal("未知结果未保留开放意图")
	}
	writeSQL(c3, "opsd_i3_reverse")
	post := readSQL(c3)
	startMove = time.Now()
	cli(true, "", "recover", id, "--mode", "reverse")
	proveWriterOrder(q, s.Containers[0].ID, startMove)
	assertBinding(q, status(id), "rolled_back", filepath.Join(q.Directory, "data"))
	if post.count != 2 || post != readSQL(c3) {
		t.Fatal("写入后的反迁遗失数据")
	}

	h, c4 := newProxy("repair")
	id = move(h)
	cli(false, "after", "apply", id)
	writeSQL(c4, "opsd_i3_repair")
	post = readSQL(c4)
	cli(true, "", "recover", id, "--mode", "repair-target")
	assertBinding(h, status(id), "complete", filepath.Join(b, "volume", "databases", h.ID, "data"))
	if post.count != 2 || post != readSQL(c4) {
		t.Fatal("目标修复遗失数据")
	}
	cli(true, "", "rollback", id)

	j, c5 := newProxy("timeout")
	id = move(j)
	must(os.WriteFile(filepath.Join(lab, "systemd-actions"), nil, 0600))
	cli(false, "reload", "apply", id)
	s = status(id)
	log, e := os.ReadFile(filepath.Join(lab, "systemd-actions"))
	must(e)
	if s.Phase != "needs_recovery" || s.WriteIntent || !strings.HasSuffix(string(log), "daemon-reload\n") {
		t.Fatal("systemd 超时后继续操作", s, string(log))
	}
	t.Logf("超时窗口 systemd 调用\n%s", log)
	cli(true, "", "recover", id, "--mode", "reverse")
	assertBinding(j, status(id), "rolled_back", filepath.Join(j.Directory, "data"))
	if readSQL(c5).count != 1 {
		t.Fatal("超时恢复遗失源数据")
	}

	poolID, poolName := namespace+"-pool", namespace+"-volume"
	poolDir := filepath.Join(a, "volume", "pools", poolID)
	poolTarget := filepath.Join(b, "volume", "pools", poolID)
	image := filepath.Join(poolDir, "data.img")
	must(os.MkdirAll(filepath.Join(poolDir, "data"), 0700))
	run("truncate", "-s", "48M", image)
	must(os.Chmod(image, 0600))
	run("mkfs.ext4", "-Fq", image)
	run("fallocate", "--length", "48M", "--", image)
	uuid := run("blkid", "-p", "-s", "UUID", "-o", "value", "--", image)
	device := run("losetup", "--find", "--show", image)
	t.Cleanup(func() {
		cleanupManaged(protocol.StoragePoolData, poolID, poolName)
		for _, dir := range []string{poolDir, poolTarget} {
			point := filepath.Join(dir, "data")
			if raw, e := exec.Command("findmnt", "-no", "SOURCE", "--mountpoint", point).Output(); e == nil {
				if err := exec.Command("umount", point).Run(); err != nil {
					t.Errorf("卸载池测试卷：%v", err)
				} else {
					_ = exec.Command("losetup", "-d", strings.TrimSpace(string(raw))).Run()
				}
			}
		}
	})
	must(os.WriteFile(filepath.Join("/sys/class/block", filepath.Base(device), "queue", "discard_max_bytes"), []byte("0\n"), 0600))
	run("mount", "-t", "ext4", "-o", "nodiscard", device, filepath.Join(poolDir, "data"))
	pinned := run("docker", "image", "inspect", "--format", "{{.Id}}", "busybox:1.37.0")
	data, e := json.Marshal(map[string]any{
		"name": poolName, "config": map[string]any{"Image": pinned, "Cmd": []string{"sh", "-c", "trap 'exit 0' TERM INT; while true; do sleep 1 & wait $!; done"}},
		"host": map[string]any{"NetworkMode": "host", "Mounts": []map[string]any{{"Type": "bind", "Source": "data", "Target": "/pool-data"}}},
	})
	must(e)
	var template dockerengine.StoragePoolTemplate
	must(json.Unmarshal(data, &template))
	must(localstate.Write(filepath.Join(execDir, "pools", poolID, "storage-data.json"), map[string]any{
		"id": poolID, "directory": poolDir, "image_uuid": uuid, "data_containers": []dockerengine.StoragePoolTemplate{template},
	}, 0600))
	must(docker.StageStoragePool(t.Context(), poolID, poolDir, []dockerengine.StoragePoolTemplate{template}))
	run("docker", "start", poolName)
	run("docker", "exec", poolName, "/bin/sh", "-c", "printf 'pool-before' >/pool-data/before && sync")
	poolId := submit("move-"+poolID, protocol.StorageRootInput{Action: "move", Component: protocol.StoragePoolData, ComponentID: poolID, TargetRootID: bID})
	cli(true, "", "apply", poolId)
	s = status(poolId)
	if s.Phase != "complete" || s.Active == nil || s.Active.Directory != poolTarget || s.Copied.Bytes != 48<<20 ||
		run("docker", "exec", poolName, "cat", "/pool-data/before") != "pool-before" {
		t.Fatal("池镜像或业务文件读回不同", s)
	}
	run("findmnt", "-no", "SOURCE,UUID,OPTIONS", "--mountpoint", filepath.Join(poolTarget, "data"))
	if got := run("blkid", "-p", "-s", "UUID", "-o", "value", "--", filepath.Join(poolTarget, "data.img")); got != uuid {
		t.Fatal("池镜像 UUID 改变", got)
	}
	run("stat", "-c", "size=%s blocks=%b", filepath.Join(poolTarget, "data.img"))
	run("docker", "exec", poolName, "/bin/sh", "-c", "printf 'pool-after' >/pool-data/after && sync")
	cli(true, "", "rollback", poolId)
	s = status(poolId)
	if s.Phase != "rolled_back" || s.Active == nil || s.Active.Directory != poolDir ||
		run("docker", "exec", poolName, "cat", "/pool-data/after") != "pool-after" {
		t.Fatal("池镜像回退未保留新文件", s)
	}
	if s3bin := os.Getenv("OPSD_TEST_MINIO"); s3bin != "" {
		peerHost := run("docker", "network", "inspect", "bridge", "--format", "{{(index .IPAM.Config 0).Gateway}}")
		p := port()
		s3Address := fmt.Sprintf("127.0.0.1:%d", p)
		const s3User, s3Pass, bucket = "opsdi3fixture", "opsd-i3-local-fixture-password", "opsd-i3"
		s3 := launch(t, "env", "MINIO_ROOT_USER="+s3User, "MINIO_ROOT_PASSWORD="+s3Pass, s3bin,
			"server", "--address", "0.0.0.0:"+strconv.Itoa(p), "--console-address", freeAddress(t), filepath.Join(lab, "s3"))
		waitLocal(t, s3, func() bool {
			res, e := http.Get("http://" + s3Address + "/minio/health/ready")
			if e != nil {
				return false
			}
			_ = res.Body.Close()
			return res.StatusCode == 200
		})
		objects, e := minio.New(s3Address, &minio.Options{Creds: credentials.NewStaticV4(s3User, s3Pass, "")})
		must(e)
		must(objects.MakeBucket(t.Context(), bucket, minio.MakeBucketOptions{}))
		message := "opsd-i3-archive-proof"
		key := archiveConfig.Prefix + "i3-proof"
		_, e = objects.PutObject(t.Context(), bucket, key, strings.NewReader(message), int64(len(message)), minio.PutObjectOptions{})
		must(e)
		archiveID := strings.TrimSuffix(strings.TrimPrefix(archiveConfig.Prefix, "opsd/metrics/"), "/")
		view, _ := get()
		var aID string
		for _, r := range view.Roots {
			if r.MountPoint == a {
				aID = r.ID
			}
		}
		if aID == "" {
			t.Fatal("归档源根未登记")
		}
		secret := protocol.S3Credential{AccessKey: s3User, SecretKey: s3Pass}
		archiveDir := filepath.Join(a, "volume", "archive")
		d := protocol.MonitoringDeployment{ClusterID: archiveID, NodeID: nodeID, Enabled: true, Directory: archiveDir, StorageRootID: aID,
			Prefix: archiveConfig.Prefix, DiskBudgetBytes: 1 << 30, Storage: protocol.S3Connection{Endpoint: fmt.Sprintf("http://%s:%d", peerHost, p),
				Bucket: bucket, Region: "us-east-1", AccessStyle: "path", DedicatedBucket: true},
			Credentials: protocol.S3Credentials{Receive: secret, Store: secret, Compact: secret}}
		owned, e := docker.StorageArchiveSpec(d)
		must(e)
		t.Cleanup(func() {
			for _, c := range owned {
				cleanupManaged(protocol.StorageArchive, archiveID, c.Name)
			}
			_, _ = exec.Command("docker", "network", "rm", strings.TrimSuffix(owned[0].Name, "-receive")+"_default").CombinedOutput()
		})
		start := time.Now()
		root.stop()
		boot, _, e := operations.SystemClock()
		must(e)
		deployID := namespace + "-archive-deployment"
		_, _, e = store.Receive(t.Context(), deployID, protocol.ContractVersion, protocol.OperationAction{
			Kind: "monitoring_deploy", Monitoring: &d}, boot)
		must(e)
		operationUnit := "opsd-operation@" + deployID + ".service"
		unitPath := filepath.Join("/etc/systemd/system", operationUnit)
		unit, e := os.OpenFile(unitPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		must(e)
		t.Cleanup(func() {
			_, _ = exec.Command("systemctl", "stop", operationUnit).CombinedOutput()
			_ = os.Remove(unitPath)
			_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
		})
		_, e = fmt.Fprintf(unit, "[Service]\nType=oneshot\nExecStart=%q operation --config %q --id %s\n", bin, config, deployID)
		must(e)
		must(unit.Close())
		run("systemctl", "daemon-reload")
		run("systemctl", "start", operationUnit)
		deployed, e := store.Get(t.Context(), deployID)
		must(e)
		if deployed.Record.State != protocol.Succeeded {
			t.Fatal("归档首次部署未就绪", deployed.Record)
		}
		root = launch(t, bin, "executor", "--config", config)
		waitLocal(t, root, func() bool { return socket(file.Socket) })
		t.Logf("登记根首次归档启动 · %s · %s", time.Since(start).Round(time.Millisecond), archiveDir)
		must(os.WriteFile(filepath.Join(archiveDir, "i3-local-proof"), []byte("从归档源复制"), 0600))
		id := submit("move-archive", protocol.StorageRootInput{Action: "move", Component: protocol.StorageArchive, ComponentID: archiveID, TargetRootID: bID})
		cli(true, "", "apply", id)
		s := status(id)
		newArchive := filepath.Join(b, "volume", "archive")
		if s.Phase != "complete" || s.Active == nil || s.Active.Directory != newArchive || len(s.Containers) != 4 {
			t.Fatal("实际四组件归档未到达新根", s)
		}
		preserved, e := os.ReadFile(filepath.Join(newArchive, "i3-local-proof"))
		must(e)
		if string(preserved) != "从归档源复制" {
			t.Fatal("工作目录数据未完成迁移")
		}
		obj, e := objects.GetObject(t.Context(), bucket, key, minio.GetObjectOptions{})
		must(e)
		body, e := io.ReadAll(obj)
		must(e)
		must(obj.Close())
		if string(body) != message {
			t.Fatal("归档源桶或前缀内容改变")
		}
		t.Logf("归档原桶读回 · prefix=%s bytes=%d sha256=%x", archiveConfig.Prefix, len(body), sha256.Sum256(body))
		must(os.WriteFile(filepath.Join(newArchive, "i3-reverse-proof"), []byte("新根加入"), 0600))
		cli(true, "", "rollback", id)
		body, e = os.ReadFile(filepath.Join(archiveDir, "i3-reverse-proof"))
		must(e)
		if string(body) != "新根加入" {
			t.Fatal("归档回退遗失新写入")
		}
	}
	if inspect(foreign) != foreignBefore {
		t.Fatal("隔离范围外的容器发生变化")
	}
	if events := run("docker", "events", "--since", started, "--until", time.Now().UTC().Format(time.RFC3339Nano), "--filter", "container="+foreign); events != "" {
		t.Fatal("非受管容器被操作", events)
	}
	if pid := run("pgrep", "-f", "node --config "+config); !strings.Contains(pid, strconv.Itoa(node.cmd.Process.Pid)) {
		t.Fatal("普通节点在切换期间退出", pid)
	}
	_, _ = get()
}
