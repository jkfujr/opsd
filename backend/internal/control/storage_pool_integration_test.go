//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
	"github.com/jkfujr/opsd/internal/storagepool"
	"github.com/jkfujr/opsd/internal/storageroot"
	"github.com/minio/minio-go/v7"
	"golang.org/x/sys/unix"
)

func poolTestCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	start := time.Now()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s · %s · %v · %s", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond), err, out)
	}
	msg := strings.TrimSpace(string(out))
	if name != "nft" && name != "iptables-save" {
		t.Logf("%s %s · %s · %s", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond), msg)
	}
	return msg
}

func poolTestUnit(component, id, config string) string {
	sum := sha256.Sum256([]byte(component + "/" + id + "/" + config))
	return fmt.Sprintf("opsd-storage-root-%x.service", sum[:10])
}

func poolFirewallRules(t *testing.T) string {
	nft := poolTestCommand(t, "nft", "-s", "list", "ruleset")
	legacy := poolTestCommand(t, "iptables-save")
	parts := slices.DeleteFunc(strings.Split(legacy, "\n"), func(line string) bool { return strings.HasPrefix(line, "#") })
	counters := regexp.MustCompile(`\[[0-9]+:[0-9]+\]`)
	return nft + "\n" + counters.ReplaceAllString(strings.Join(parts, "\n"), "[0:0]")
}

func monitorPoolFirewall(t *testing.T) func() []byte {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var captured bytes.Buffer
	cmd := exec.CommandContext(ctx, "nft", "monitor", "ruleset")
	cmd.Stdout = &captured
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return func() []byte {
		cancel()
		_ = cmd.Wait()
		return bytes.TrimSpace(captured.Bytes())
	}
}

func poolTestLab(t *testing.T, phase string) string {
	t.Helper()
	lab, err := os.MkdirTemp("/srv", "opsd-"+phase+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		body, _ := exec.Command("findmnt", "-nlo", "TARGET").Output()
		if strings.Contains(string(body), lab+"/") {
			t.Errorf("保留仍有挂载的隔离目录 %s", lab)
			return
		}
		_ = os.RemoveAll(lab)
	})
	return lab
}

func poolTestNodes(t *testing.T, lab, bin string, octet int, now func() time.Time, capacities []int64) ([]*apiNode, map[string]*storageroot.Manager, []protocol.PoolMemberInput) {
	t.Helper()
	entries := make([]*apiNode, len(capacities))
	hosts := make(map[string]*storageroot.Manager)
	var members []protocol.PoolMemberInput
	for i := range entries {
		n := openAPINode(t, fmt.Sprintf("记录节点 %d", i+1), "隔离", 1, 1)
		if now != nil {
			n.node.Now = now
		}
		entries[i] = n
		dir := filepath.Join(lab, fmt.Sprintf("node-%d", i+1))
		b := poolTestExt4(t, dir, "B", "12G")
		ip := fmt.Sprintf("10.253.89.%d", octet+i)
		poolTestCommand(t, "ip", "address", "add", ip+"/32", "dev", "lo")
		t.Cleanup(func() { _, _ = exec.Command("ip", "address", "del", ip+"/32", "dev", "lo").CombinedOutput() })
		config := filepath.Join(dir, "config", "instance.json")
		f := installation.NodeFile{Binary: bin, DataDir: filepath.Join(dir, "meta"), ExecutorDir: filepath.Join(dir, "executor"),
			Socket: filepath.Join(dir, "executor.sock")}
		if err := localstate.Write(config, f, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(f.ExecutorDir, 0700); err != nil {
			t.Fatal(err)
		}
		db, err := storage.Open(t.Context(), filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		host := &storageroot.Manager{Config: config, Binary: bin, NodeDir: f.DataDir, ExecutorDir: f.ExecutorDir,
			Store: operations.Store{DB: db}, Docker: dockerengine.New()}
		hosts[n.node.ID] = host
		reg, err := host.Write(t.Context(), protocol.StorageRootWrite{ID: cluster.NewOperationID(n.node.ID),
			Input: protocol.StorageRootInput{Action: "register", ExpectedRevision: 1, MountPoint: b, RelativeDir: "datasets"}})
		if err != nil || reg.State != "registered" {
			t.Fatal(reg, err)
		}
		view, err := host.Read(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		root := ""
		for _, r := range view.Roots {
			if r.MountPoint == b {
				root = r.ID
			}
		}
		members = append(members, protocol.PoolMemberInput{NodeID: n.node.ID, PrivateIP: ip, CapacityGiB: capacities[i],
			StorageRootID: root, FailureDomain: fmt.Sprintf("rack-%d", i+1)})
		services := executor.NewServices(db)
		services.StoragePool = host.PoolHost
		listener, err := executor.Listen(f.Socket)
		if err != nil {
			t.Fatal(err)
		}
		service := &http.Server{Handler: (&executor.Server{Services: services, Directory: f.ExecutorDir}).Handler()}
		go func() { _ = service.Serve(listener) }()
		t.Cleanup(func() { _ = service.Close() })
		n.node.Executor = executor.NewClient(f.Socket)
	}
	joinAPINodes(t, entries...)
	for _, e := range entries {
		e.node.Start()
	}
	return entries, hosts, members
}

func TestStoragePoolNative(t *testing.T) {
	bin := os.Getenv("OPSD_TEST_BINARY")
	if os.Getenv("OPSD_TEST_STORAGE_POOL") != "1" || bin == "" || os.Geteuid() != 0 {
		t.Skip("需要 OPSD-CI、真实 Docker、systemd、loop 磁盘和 Linux 工件")
	}
	lab := poolTestLab(t, "i5")
	moveTarget := poolTestExt4(t, lab, "move-target", "10G")
	var shift atomic.Int64
	shift.Store(int64(-65 * 24 * time.Hour))
	before := time.Now()
	entries, hosts, members := poolTestNodes(t, lab, bin, 81, func() time.Time {
		return time.Now().Add(time.Duration(shift.Load()))
	}, []int64{12, 12, 12})
	in := protocol.PoolInput{Name: "OPSD 隔离池", PortOffset: 12103, Nodes: members}
	clients, csrf := make([]*http.Client, len(entries)), make([]string, len(entries))
	for i, entry := range entries {
		clients[i], csrf[i] = apiAdmin(t, entry)
	}
	nftEvents := monitorPoolFirewall(t)
	initialFirewall := poolFirewallRules(t)
	request := func(entry int, method, path, key string, body, out any, want int) {
		t.Helper()
		start := time.Now()
		if got := apiCall(t, clients[entry], entries[entry].web.URL, method, "/api/v2"+path, "", key, csrf[entry], body, out); got != want {
			t.Fatalf("%s %s 返回 %d，期望 %d，读回 %+v", method, path, got, want, out)
		}
		t.Logf("%s %s → %d，%s", method, path, want, time.Since(start).Round(time.Millisecond))
	}
	small := in
	small.Nodes = slices.Clone(in.Nodes)
	for i := range small.Nodes {
		small.Nodes[i].CapacityGiB = 10
	}
	var shortage struct{ Detail string }
	request(0, "POST", "/storage/pools/preview", "", small, &shortage, 507)
	if !strings.Contains(shortage.Detail, "12 GiB") || !strings.Contains(shortage.Detail, "B 数据卷至少 6 GiB") {
		t.Fatal("预览未说明控制成员最小数据配额", shortage.Detail)
	}
	t.Log(shortage.Detail)
	small.Nodes[0].CapacityGiB = 24
	small.Nodes[1].CapacityGiB, small.Nodes[2].CapacityGiB = 12, 12
	request(0, "POST", "/storage/pools/preview", "", small, &shortage, 507)
	if !strings.Contains(shortage.Detail, small.Nodes[0].NodeID) || !strings.Contains(shortage.Detail, "B 盘需可用") || !strings.Contains(shortage.Detail, "当前") {
		t.Fatal("预览未返回成员的 B 盘所需及实际余量", shortage.Detail)
	}
	t.Log(shortage.Detail)
	var plan protocol.PoolPlan
	request(0, "POST", "/storage/pools/preview", "", in, &plan, 200)
	for _, m := range plan.Nodes {
		t.Logf("成员 %s · %s · A %s %d · B %s %d · M=%d D=%d", m.NodeID, m.FailureDomain,
			m.ControlDisk.UUID, m.ControlDisk.AvailableBytes, m.DataDisk.UUID, m.DataDisk.AvailableBytes, m.ControlBytes, m.DataBytes)
		if m.ControlDisk.UUID == m.DataDisk.UUID || !m.Control() || m.ControlBytes != 6*protocol.PoolGiB || m.DataBytes != 6*protocol.PoolGiB {
			t.Fatal("预览角色或分别核算不符", m)
		}
	}
	units := []string{}
	for _, host := range hosts {
		units = append(units, poolTestUnit(protocol.StoragePoolData, plan.PoolID, host.Config), poolTestUnit("pool_control", plan.PoolID, host.Config))
	}
	t.Cleanup(func() {
		ids, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=com.opsd.storage.pool="+plan.PoolID).Output()
		if t.Failed() {
			for _, id := range strings.Fields(string(ids)) {
				meta, _ := exec.Command("docker", "inspect", "-f", "{{.Name}} {{.State.Status}} {{json .Config.Cmd}}", id).Output()
				logs, _ := exec.Command("docker", "logs", "--tail", "40", id).CombinedOutput()
				t.Logf("%s %s", meta, logs)
			}
		}
		_, _ = exec.Command("systemctl", append([]string{"stop"}, units...)...).CombinedOutput()
		for _, id := range strings.Fields(string(ids)) {
			_, _ = exec.Command("docker", "rm", "-f", id).CombinedOutput()
		}
		for _, unit := range units {
			_ = os.Remove(filepath.Join("/etc/systemd/system", unit))
			for _, parent := range []string{"multi-user.target.wants", "docker.service.wants"} {
				_ = os.Remove(filepath.Join("/etc/systemd/system", parent, unit))
			}
		}
		_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	})
	secret := protocol.PoolCredentials{AccessKey: "opsd-ci-" + identity.Random()[:8], SecretKey: identity.Random()}
	create := protocol.PoolCreate{PlanID: plan.PlanID, Credentials: secret}
	var op, repeat cluster.Operation
	request(0, "POST", "/storage/pools", "i5-create", create, &op, 202)
	request(0, "POST", "/storage/pools", "i5-create", create, &repeat, 202)
	if op.ID != repeat.ID || op.ResourceID != plan.PoolID || op.PlanID != plan.PlanID {
		t.Fatal("原回执与计划不一致", op.ID, repeat.ID)
	}
	request(0, "POST", "/storage/pools", "i5-other-key", create, nil, 409)
	waitDatabase(t, "池部署", 220*time.Second, func() bool {
		var current cluster.Operation
		status := apiCall(t, clients[0], entries[0].web.URL, "GET", "/api/v2/operations/"+op.ID, "", "", csrf[0], nil, &current)
		if status != 200 {
			t.Fatalf("操作查询返回 %d", status)
		}
		if current.Revision != op.Revision {
			t.Logf("操作 %s rev=%d %s %+v %+v", current.ID, current.Revision, current.State, current.Steps, current.Error)
			op = current
		}
		if current.State == protocol.AttentionRequired || current.State == protocol.Rejected || current.State == protocol.Failed {
			t.Fatal(current.State, current.Error)
		}
		return current.State == protocol.Succeeded
	})
	var pool protocol.StoragePool
	request(1, "GET", "/storage/pools/"+plan.PoolID, "", nil, &pool, 200)
	var list protocol.PoolList
	request(2, "GET", "/storage/pools?limit=1", "", nil, &list, 200)
	if pool.State != "ready" || len(pool.Nodes) != 3 || len(list.Items) != 1 || list.Items[0].ID != pool.ID {
		t.Fatal("池跨入口读回无效", pool.State, len(pool.Nodes), list.Items)
	}
	t.Logf("池 ready：%s，建池总耗时 %s", pool.ID, time.Since(before).Round(time.Millisecond))
	current := func() protocol.StoragePool {
		var current protocol.StoragePool
		request(1, "GET", "/storage/pools/"+plan.PoolID, "", nil, &current, 200)
		return current
	}
	poolTestOperations(t, pool, hosts, secret, current)
	poolTestMove(t, pool, hosts[pool.Nodes[0].NodeID], moveTarget, secret, current)
	poolTestTLS(t, pool, hosts, secret, current, &shift)
	poolTestRoleOwners(t, pool, hosts)
	if after := poolFirewallRules(t); after != initialFirewall {
		t.Fatal("建池前后的防火墙规则不同")
	}
	if events := nftEvents(); len(events) != 0 {
		t.Fatalf("检测到 nftables 变更事件：%s", events)
	}
	t.Logf("防火墙规则前后一致 SHA-256=%x，nft monitor 写入事件 0", sha256.Sum256([]byte(initialFirewall)))
	if duration := os.Getenv("OPSD_TEST_STORAGE_POOL_SOAK"); duration != "" {
		window, err := time.ParseDuration(duration)
		if err != nil || window <= 0 {
			t.Fatal("观察窗口无效", duration, err)
		}
		poolTestSoak(t, current, secret, window)
	}
}

func poolTestRoleOwners(t *testing.T, pool protocol.StoragePool, hosts map[string]*storageroot.Manager) {
	t.Helper()
	check := func(path, expected string) {
		t.Helper()
		if got := poolTestCommand(t, "stat", "-Lc", "%u:%g %a", path); got != expected {
			t.Fatal("池文件属主或权限不同", path, got, expected)
		}
	}
	volumes := 0
	for _, member := range pool.Nodes {
		host := hosts[member.NodeID]
		root := filepath.Join(host.ExecutorDir, "pools", pool.ID)
		data, err := host.Directory(t.Context(), protocol.StoragePoolData, pool.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range member.Roles {
			container := poolTestContainer(pool.ID, member.NodeID, role)
			status := strings.Fields(poolTestCommand(t, "docker", "inspect", "-f", "{{.Config.User}} {{.State.Pid}}", container))
			if len(status) != 2 || status[0] != "65534:65534" || status[1] == "0" {
				t.Fatal("池组件未以约定身份运行", role, status)
			}
			process, err := os.ReadFile(filepath.Join("/proc", status[1], "status"))
			want := "\t65534\t65534\t65534\t65534"
			if err != nil || !strings.Contains(string(process), "\nUid:"+want+"\n") || !strings.Contains(string(process), "\nGid:"+want+"\n") {
				t.Fatal("池组件实际进程身份不同", role, err)
			}
			if role == "volume" {
				check(filepath.Join(data, "data", role), "65534:65534 700")
			} else {
				check(filepath.Join(root, "control", role), "65534:65534 700")
			}
		}
		for _, image := range []string{filepath.Join(root, "control.img"), filepath.Join(data, "data.img")} {
			check(image, "0:0 600")
		}
		check(filepath.Join(root, "runtime", "active", "tls.key"), "65534:65534 600")
		if member.Control() {
			check(filepath.Join(root, "runtime-etcd", "active", "etcd-peer.key"), "65534:65534 600")
			check(filepath.Join(root, "runtime-etcd", "active", "ca.sealed"), "0:0 600")
		}
		files, err := filepath.Glob(filepath.Join(data, "data", "volume", "*.dat"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) > 0 {
			volumes++
			if owner := poolTestCommand(t, "stat", "-c", "%u:%g", files[0]); owner != "65534:65534" {
				t.Fatal("实际卷文件属主不同", files[0], owner)
			}
		}
	}
	if volumes < 2 {
		t.Fatal("缺少可核对实际写入属主的卷副本", volumes)
	}
	t.Log("迁移、重挂载和轮换后五类组件进程、卷文件及 TLS 密钥属主检查通过")
}

func TestStoragePoolSharedDiskCapacityNative(t *testing.T) {
	if os.Getenv("OPSD_TEST_STORAGE_POOL") != "1" || os.Geteuid() != 0 {
		t.Skip("需要 OPSD-CI、真实 Docker 和 loop 磁盘")
	}
	lab := poolTestLab(t, "i5")
	mount := poolTestExt4(t, lab, "shared", "12G")
	const offset = 15100
	ip := "10.253.89.91"
	poolTestCommand(t, "ip", "address", "add", ip+"/32", "dev", "lo")
	t.Cleanup(func() { _, _ = exec.Command("ip", "address", "del", ip+"/32", "dev", "lo").CombinedOutput() })
	type candidate struct {
		host *storageroot.Manager
		req  protocol.PoolHostRequest
		data string
	}
	var candidates [2]candidate
	for i := range candidates {
		root := filepath.Join(mount, fmt.Sprintf("node-%d", i))
		host := &storageroot.Manager{Config: filepath.Join(root, "config", "instance.json"),
			NodeDir: filepath.Join(root, "node"), ExecutorDir: filepath.Join(root, "executor"), Docker: dockerengine.New()}
		if err := os.MkdirAll(host.ExecutorDir, 0700); err != nil {
			t.Fatal(err)
		}
		nodeID := fmt.Sprintf("capacity-node-%d", i)
		ctx := t.Context()
		db, err := storage.Open(ctx, filepath.Join(host.ExecutorDir, "executor.db"), storage.Executor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		host.Store = operations.Store{DB: db}
		_, err = host.Write(ctx, protocol.StorageRootWrite{ID: cluster.NewOperationID(nodeID), Input: protocol.StorageRootInput{
			Action: "register", ExpectedRevision: 1, MountPoint: mount, RelativeDir: fmt.Sprintf("node-%d/datasets", i)}})
		if err != nil {
			t.Fatal(err)
		}
		view, err := host.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		input := protocol.PoolMemberInput{NodeID: nodeID, PrivateIP: ip, CapacityGiB: protocol.PoolMinCapacityGiB, FailureDomain: "ordinary"}
		for _, disk := range view.Roots {
			if disk.MountPoint == mount {
				input.StorageRootID = disk.ID
			}
		}
		probe, err := host.PoolHost(ctx, protocol.PoolHostRequest{Action: "inspect", Member: input, PortOffset: offset})
		d, m := storagepool.Quotas(protocol.PoolMinCapacityGiB*protocol.PoolGiB, false)
		if err != nil || probe.ControlDisk.UUID != probe.DataDisk.UUID ||
			!storagepool.Enough(probe.ControlDisk, probe.DataDisk, m, d) || storagepool.Enough(probe.ControlDisk, probe.DataDisk, 2*m, 2*d) {
			t.Fatal("共享文件系统须恰好容纳一笔申请", probe.ControlDisk, probe.DataDisk, err)
		}
		member := protocol.PoolNodePlan{PoolMemberInput: input, Roles: []string{"volume", "filer", "s3"},
			Ports: storagepool.Ports(offset), ControlDisk: probe.ControlDisk, DataDisk: probe.DataDisk, ControlBytes: m, DataBytes: d}
		members := []protocol.PoolNodePlan{member}
		for j := 1; j <= 3; j++ {
			control := member
			control.NodeID, control.FailureDomain, control.PrivateIP = fmt.Sprintf("control-%d", j), fmt.Sprintf("rack-%d", j), fmt.Sprintf("10.253.89.%d", 91+j)
			control.CapacityGiB, control.ControlBytes, control.DataBytes = protocol.PoolMinControlCapacityGiB, 6*protocol.PoolGiB, protocol.PoolMinDataBytes
			control.Roles = []string{"master", "etcd", "volume", "filer", "s3"}
			members = append(members, control)
		}
		id := cluster.NewOperationID(nodeID)
		auth, err := storagepool.NewAuthority(id, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		material, err := auth.Issue(nodeID, ip, fmt.Sprintf("%064x", i+1), "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		deploy := &protocol.PoolDeploy{PoolID: id, OperationID: cluster.NewOperationID(nodeID), Member: member, Members: members,
			Material: material, Credentials: protocol.PoolCredentials{AccessKey: "opsd-i5-capacity", SecretKey: identity.Random()}}
		candidates[i] = candidate{host, protocol.PoolHostRequest{Action: "prepare", PoolID: id, Deploy: deploy},
			filepath.Join(root, "datasets", "pools", id, "data.img")}
		t.Logf("并发提交候选 %s，A/B UUID=%s，可用=%d，预留=%d，M=%d D=%d",
			id, probe.DataDisk.UUID, probe.DataDisk.AvailableBytes, probe.DataDisk.ReserveBytes, m, d)
	}
	type result struct {
		index int
		phase string
		err   error
	}
	start, done := make(chan struct{}), make(chan result, len(candidates))
	for i, c := range candidates {
		go func() {
			<-start
			out, err := c.host.PoolHost(t.Context(), c.req)
			done <- result{i, out.Phase, err}
		}()
	}
	close(start)
	var results [2]result
	for i := range results {
		results[i] = <-done
	}
	succeeded := 0
	for _, r := range results {
		c := candidates[r.index]
		if r.err == nil {
			if r.phase != "prepared" {
				t.Fatal("预分配状态", r.phase)
			}
			succeeded++
			poolTestCommand(t, "stat", "-c", "%s %b", c.data)
			continue
		}
		var fail *firewall.Fault
		if errors.As(r.err, &fail) && fail.Code == "node_busy" {
			// 先等两笔都返回，确认未分配的一笔按最新容量重新核算。
			continue
		}
		if !errors.As(r.err, &fail) || fail.Code != "insufficient_storage" {
			t.Fatal("未预期的并发分配错误", r.err)
		}
	}
	if succeeded != 1 {
		t.Fatal("共享文件系统应只完成一笔", succeeded)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c.data); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		_, err := c.host.PoolHost(t.Context(), c.req)
		var fail *firewall.Fault
		if !errors.As(err, &fail) || fail.Code != "insufficient_storage" {
			t.Fatal("剩余配额应在分配前拒绝", err)
		}
		if _, err = os.Stat(filepath.Join(c.host.ExecutorDir, "pools", c.req.PoolID, "control.img")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("被拒绝的申请产生了控制卷", err)
		}
		t.Logf("同盘并发一笔完成，另一笔重新核算返回 %s，未建立镜像", fail.Code)
	}
}

func poolTestExt4(t *testing.T, dir, name, size string) string {
	t.Helper()
	mount := filepath.Join(dir, name)
	if err := os.MkdirAll(mount, 0755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, name+"-device.img")
	poolTestCommand(t, "truncate", "-s", size, disk)
	dev := poolTestCommand(t, "losetup", "--find", "--show", disk)
	t.Cleanup(func() {
		if exec.Command("mountpoint", "-q", mount).Run() == nil {
			if text, err := exec.Command("umount", mount).CombinedOutput(); err != nil {
				t.Errorf("隔离 B 卸载失败：%v %s", err, text)
				return
			}
		}
		_, _ = exec.Command("losetup", "-d", dev).CombinedOutput()
	})
	poolTestCommand(t, "mkfs.ext4", "-Fqm", "0", dev)
	poolTestCommand(t, "mount", "-o", "nodiscard", dev, mount)
	poolTestCommand(t, "findmnt", "-no", "TARGET,SOURCE,FSTYPE,UUID", "--mountpoint", mount)
	return mount
}

func poolTestMove(t *testing.T, pool protocol.StoragePool, host *storageroot.Manager, target string, keys protocol.PoolCredentials,
	current func() protocol.StoragePool) {
	t.Helper()
	member := pool.Nodes[0]
	view, err := host.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	register, err := host.Write(t.Context(), protocol.StorageRootWrite{ID: cluster.NewOperationID(member.NodeID),
		Input: protocol.StorageRootInput{Action: "register", ExpectedRevision: view.Revision, MountPoint: target, RelativeDir: "datasets"}})
	if err != nil || register.State != "registered" {
		t.Fatal("登记迁移目标失败", register, err)
	}
	view, err = host.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	idx := slices.IndexFunc(view.Roots, func(r protocol.StorageRoot) bool { return r.MountPoint == target })
	if idx < 0 {
		t.Fatal("登记后读回缺少迁移目标")
	}
	dst := view.Roots[idx]
	src, err := host.Directory(t.Context(), protocol.StoragePoolData, pool.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	moveID := cluster.NewOperationID(member.NodeID)
	move, err := host.Write(t.Context(), protocol.StorageRootWrite{ID: moveID, Input: protocol.StorageRootInput{
		Action: "move", Component: protocol.StoragePoolData, ComponentID: pool.ID, ExpectedRevision: view.Revision, TargetRootID: dst.ID,
	}})
	if err != nil || move.State != "pending_restart" {
		t.Fatal("申请池数据迁移失败", move, err)
	}
	master := poolTestContainer(pool.ID, member.NodeID, "master")
	etcd := poolTestContainer(pool.ID, member.NodeID, "etcd")
	control := poolTestCommand(t, "docker", "inspect", "-f", "{{.Id}} {{.State.StartedAt}}", master, etcd)
	start := time.Now()
	poolTestCommand(t, host.Binary, "storage-root", "apply", "--id", moveID, "--config", host.Config)
	status, err := host.Status(t.Context(), moveID)
	if err != nil || status.Phase != "complete" || status.Active == nil || status.Active.RootID != dst.ID {
		t.Fatal("迁移完成读回不同", status, err)
	}
	if exec.Command("mountpoint", "-q", filepath.Join(src, "data")).Run() == nil {
		t.Fatal("池迁移后旧数据镜像仍挂载")
	}
	if got := poolTestCommand(t, "docker", "inspect", "-f", "{{.Id}} {{.State.StartedAt}}", master, etcd); got != control {
		t.Fatal("搬 D 时改变了 Master 或 etcd 进程身份")
	}
	after := current().Nodes[0]
	if after.DataDisk.UUID != dst.UUID || after.DataUUID != member.DataUUID || after.ControlUUID != member.ControlUUID {
		t.Fatal("整根迁移后的 B、D 或 A 身份不同", after)
	}
	client, err := storagepool.S3(member.Endpoint, pool.CAPEM, keys)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := poolObjectHash(t.Context(), client, "i5-proof", "cross-ingress")
	if err != nil || digest != sha256.Sum256([]byte("OPSD CI · 跨入口写读和回退数据\n")) {
		t.Fatal("池迁移后 S3 对象摘要不同", digest, err)
	}
	t.Logf("I3 搬真实池 D 完成 · %s · phases=%v · B=%s D=%s 摘要=%x", time.Since(start).Round(time.Millisecond),
		status.StepTimes, dst.UUID, after.DataUUID, digest)
}

func poolTestOperations(t *testing.T, pool protocol.StoragePool, hosts map[string]*storageroot.Manager, keys protocol.PoolCredentials, current func() protocol.StoragePool) {
	t.Helper()
	ctx := t.Context()
	clients := []*minio.Client{}
	for _, m := range pool.Nodes {
		c, err := storagepool.S3(m.Endpoint, pool.CAPEM, keys)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	const bucket, key = "i5-proof", "cross-ingress"
	if err := clients[0].MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	body := []byte("OPSD CI · 跨入口写读和回退数据\n")
	if _, err := clients[0].PutObject(ctx, bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	read := func(client *minio.Client) bool {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		got, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if err != nil {
			return false
		}
		defer got.Close()
		raw, err := io.ReadAll(got)
		return err == nil && bytes.Equal(raw, body)
	}
	if !read(clients[1]) || !read(clients[2]) {
		t.Fatal("跨入口写入后未立即读到相同对象")
	}
	t.Logf("跨入口 A PUT B/C GET [%d 字节 SHA-256=%x]", len(body), sha256.Sum256(body))
	for i := range 120 {
		key := fmt.Sprintf("small-%03d", i)
		piece := bytes.Repeat([]byte{byte(i)}, 64<<10)
		poolPutCheck(t, clients[i%3], clients[(i+1)%3], bucket, key, piece, false)
	}
	t.Log("签名 PUT 后另一入口立即 GET，120/120 × 64 KiB 摘要一致")
	large := bytes.Repeat([]byte("OPSD"), 32<<20)
	for i := range 10 {
		poolPutCheck(t, clients[i%3], clients[(i+1)%3], bucket, fmt.Sprintf("multipart-%02d", i), large, true)
	}
	t.Log("签名 multipart，10/10 × 128 MiB 跨入口摘要一致")

	for trial := range 3 {
		poolTestLeaderLoss(t, pool, current, hosts, clients, trial+1, bucket, key, body)
	}
	poolTestQuota(t, pool, hosts[pool.Nodes[0].NodeID], current, read, clients[1])
}

func poolObjectHash(ctx context.Context, c *minio.Client, bucket, key string) ([32]byte, error) {
	var digest [32]byte
	obj, err := c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return digest, err
	}
	defer obj.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, obj); err == nil {
		copy(digest[:], hash.Sum(nil))
	}
	return digest, err
}

func poolPutCheck(t *testing.T, write, read *minio.Client, bucket, key string, data []byte, multipart bool) {
	t.Helper()
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	options := minio.PutObjectOptions{ContentType: "application/octet-stream"}
	if multipart {
		options.PartSize = 16 << 20
	}
	upload, err := write.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		t.Fatal(key, err)
	}
	if multipart && !strings.HasSuffix(strings.Trim(upload.ETag, "\""), "-8") {
		t.Fatal("应有八片的 multipart ETag", upload.ETag)
	}
	got, err := poolObjectHash(ctx, read, bucket, key)
	if err != nil || got != sha256.Sum256(data) {
		t.Fatal("对象跨入口摘要不一致", key, got, err)
	}
}

func poolTestContainer(pool, node, role string) string {
	s := sha256.Sum256([]byte(pool + "\x00" + node))
	return fmt.Sprintf("opsd-pool-%x-%s", s[:12], role)
}

func poolTestEtcdLeader(t *testing.T, pool string, member protocol.PoolMemberStatus, all []protocol.PoolMemberStatus) {
	t.Helper()
	container := poolTestContainer(pool, member.NodeID, "etcd")
	args := []string{"exec", container, "/usr/local/bin/etcdctl",
		"--endpoints=https://" + storagepool.Address(member.PrivateIP, member.Ports.EtcdClient),
		"--cacert=/etc/seaweedfs/active/ca.crt", "--cert=/etc/seaweedfs/active/tls.crt", "--key=/etc/seaweedfs/active/tls.key"}
	type endpoint struct {
		Status struct {
			Header struct {
				MemberID uint64 `json:"member_id"`
			}
			Leader uint64
		}
	}
	read := func() endpoint {
		var status []endpoint
		raw := poolTestCommand(t, "docker", append(slices.Clone(args), "endpoint", "status", "-w", "json")...)
		if err := json.Unmarshal([]byte(raw), &status); err != nil || len(status) != 1 || status[0].Status.Header.MemberID == 0 {
			t.Fatal("etcd endpoint status 无效", err)
		}
		return status[0]
	}
	state := read()
	if state.Status.Leader != state.Status.Header.MemberID {
		endpoints := make([]string, 0, len(all))
		for _, m := range all {
			endpoints = append(endpoints, "https://"+storagepool.Address(m.PrivateIP, m.Ports.EtcdClient))
		}
		move := slices.Clone(args)
		move[3] = "--endpoints=" + strings.Join(endpoints, ",")
		poolTestCommand(t, "docker", append(move, "move-leader", strconv.FormatUint(state.Status.Header.MemberID, 16))...)
		state = read()
	}
	if state.Status.Leader != state.Status.Header.MemberID {
		t.Fatal("故障目标没有持有 etcd 领导")
	}
	t.Logf("故障目标 etcd 成员=%x 领导=%x", state.Status.Header.MemberID, state.Status.Leader)
}

func poolTestLeaderLoss(t *testing.T, pool protocol.StoragePool, current func() protocol.StoragePool,
	hosts map[string]*storageroot.Manager, clients []*minio.Client, trial int, bucket, key string, data []byte) {
	t.Helper()
	state := current()
	leader := ""
	for _, m := range state.Nodes {
		if m.MasterLeader != "" {
			leader = m.MasterLeader
			break
		}
	}
	index := slices.IndexFunc(state.Nodes, func(m protocol.PoolMemberStatus) bool {
		addr := storagepool.Address(m.PrivateIP, m.Ports.Master)
		return leader == addr || strings.HasPrefix(leader, addr+".")
	})
	if index < 0 || pool.Nodes[index].NodeID != state.Nodes[index].NodeID {
		t.Fatal("没有确认当前 Master 领导", leader)
	}
	m := state.Nodes[index]
	poolTestEtcdLeader(t, pool.ID, m, state.Nodes)
	target := []string{"kill", "--signal", "KILL"}
	for _, role := range m.Roles {
		target = append(target, poolTestContainer(pool.ID, m.NodeID, role))
	}
	start := time.Now()
	poolTestCommand(t, "docker", target...)
	a, b := clients[(index+1)%3], clients[(index+2)%3]
	probe := func(c *minio.Client, name string) bool {
		ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
		defer cancel()
		got, err := poolObjectHash(ctx, c, bucket, name)
		return err == nil && got == sha256.Sum256(data)
	}
	waitDatabase(t, "保留旧对象读取", 10*time.Second, func() bool { return probe(b, key) })
	readRTO := time.Since(start)
	newKey := fmt.Sprintf("outage-%d", trial)
	unknown := 0
	waitDatabase(t, "恢复新写入与跨入口读取", 45*time.Second-readRTO, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
		defer cancel()
		got, err := poolObjectHash(ctx, a, bucket, newKey)
		if err == nil {
			return got == sha256.Sum256(data) && probe(b, newKey)
		}
		if minio.ToErrorResponse(err).Code != "NoSuchKey" {
			return false
		}
		if _, err := a.PutObject(ctx, bucket, newKey, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{}); err != nil {
			unknown++
		}
		return probe(b, newKey)
	})
	writeRTO := time.Since(start)
	t.Logf("第 %d 次停止 Master/etcd/S3 领导所在节点 %s，旧对象 RTO=%s，新写跨入口 RTO=%s，未知应答=%d",
		trial, m.PrivateIP, readRTO.Round(time.Millisecond), writeRTO.Round(time.Millisecond), unknown)
	if readRTO > 10*time.Second || writeRTO > 45*time.Second {
		t.Fatal("故障恢复超过计划预算", readRTO, writeRTO)
	}
	waitDatabase(t, "池降级", 25*time.Second, func() bool { return current().State == "degraded" })
	config := hosts[m.NodeID].Config
	units := []string{poolTestUnit(protocol.StoragePoolData, pool.ID, config), poolTestUnit("pool_control", pool.ID, config)}
	poolTestCommand(t, "systemctl", append([]string{"stop"}, units...)...)
	poolTestCommand(t, "systemctl", "start", units[1], units[0])
	waitDatabase(t, "成员恢复后池就绪", 45*time.Second, func() bool { return current().State == "ready" })
}

func poolTestTLS(t *testing.T, pool protocol.StoragePool, hosts map[string]*storageroot.Manager, keys protocol.PoolCredentials,
	current func() protocol.StoragePool, shift *atomic.Int64) {
	t.Helper()
	var before [3][32]byte
	for i, member := range pool.Nodes {
		if days := time.Until(member.CertExpiry).Hours() / 24; days < 24 || days > 26 {
			t.Fatalf("轮换前叶证书剩余 %.2f 天", days)
		}
		before[i] = poolTLSLeaf(t, member, pool.CAPEM)
	}
	shift.Store(0)
	trace := time.Now()
	waitDatabase(t, "近到期证书自动轮换", 180*time.Second, func() bool {
		s := current()
		return s.State == "ready" && !slices.ContainsFunc(s.Nodes, func(n protocol.PoolMemberStatus) bool {
			return time.Until(n.CertExpiry) < 89*24*time.Hour || !n.S3Ready
		})
	})
	for i, member := range pool.Nodes {
		if after := poolTLSLeaf(t, member, pool.CAPEM); after == before[i] {
			t.Fatal("叶证书的实际握手指纹未更新", member.PrivateIP)
		}
	}
	t.Logf("叶证书剩余约 25 天触发逐台自动轮换，三个入口完成实际 TLS 握手，耗时 %s", time.Since(trace).Round(time.Millisecond))

	next, err := storagepool.NewAuthority(pool.ID+"-ci-next", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bundle := pool.CAPEM + next.Cert
	old := make([]protocol.PoolMaterial, len(pool.Nodes))
	newLeaves := make([]protocol.PoolMaterial, len(pool.Nodes))
	apply := func(i int, material protocol.PoolMaterial) {
		t.Helper()
		member := pool.Nodes[i]
		call, cancel := context.WithTimeout(t.Context(), 100*time.Second)
		defer cancel()
		start := time.Now()
		out, err := hosts[member.NodeID].PoolHost(call, protocol.PoolHostRequest{
			Action: "rotate", PoolID: pool.ID, Deploy: &protocol.PoolDeploy{PoolID: pool.ID, OperationID: pool.OperationID, Material: material}})
		if err != nil || out.TLSGeneration != storagepool.TLSGeneration(material) {
			t.Fatal("隔离 CA 轮换节点失败", member.PrivateIP, err)
		}
		t.Logf("TLS %s 代次 %s，耗时 %s", member.PrivateIP, out.TLSGeneration, time.Since(start).Round(time.Millisecond))
	}
	for i, member := range pool.Nodes {
		dir := filepath.Join(hosts[member.NodeID].ExecutorDir, "pools", pool.ID, "runtime")
		read := func(p string) string {
			body, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			return string(body)
		}
		old[i] = protocol.PoolMaterial{CA: pool.CAPEM, Cert: read(filepath.Join(dir, "active", "tls.crt")),
			Key: read(filepath.Join(dir, "active", "tls.key")), PeerCert: read(filepath.Join(dir+"-etcd", "active", "etcd-peer.crt")),
			PeerKey: read(filepath.Join(dir+"-etcd", "active", "etcd-peer.key")), CASeal: read(filepath.Join(dir+"-etcd", "active", "ca.sealed"))}
		material := old[i]
		material.CA = bundle
		apply(i, material)
	}
	for i, member := range pool.Nodes {
		material, err := next.Issue(member.NodeID, member.PrivateIP, "", old[i].CASeal, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		newLeaves[i] = material
		material.CA = bundle
		apply(i, material)
		for j, other := range pool.Nodes {
			_ = poolTLSLeaf(t, other, bundle)
			if j > i {
				_ = poolTLSLeaf(t, other, pool.CAPEM)
			}
		}
	}
	t.Log("双信任窗口实际使用新证书和仍在有效期内的旧证书，三个入口均可完成校验")
	for i, member := range pool.Nodes {
		apply(i, newLeaves[i])
		_ = poolTLSLeaf(t, member, next.Cert)
		c, err := storagepool.S3(member.Endpoint, next.Cert, keys)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ListBuckets(t.Context()); err != nil {
			t.Fatal("退出旧根后签名 S3 请求失败", err)
		}
	}
	t.Log("退役旧 CA 后，三个新 CA 入口均通过 SigV4 读取")
	// 保持隔离工作流后续观察使用原发布的 CA。
	for i := range newLeaves {
		body := newLeaves[i]
		body.CA = bundle
		apply(i, body)
	}
	for i := range old {
		body := old[i]
		body.CA = bundle
		apply(i, body)
	}
	for i := range old {
		apply(i, old[i])
	}
}

func poolTestSoak(t *testing.T, current func() protocol.StoragePool, creds protocol.PoolCredentials, window time.Duration) {
	t.Helper()
	waitDatabase(t, "进入持续观察前确认全部入口就绪", 25*time.Second, func() bool { return current().State == "ready" })
	start := time.Now()
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	digest := sha256.Sum256([]byte("OPSD CI · 跨入口写读和回退数据\n"))
	passes := 0
	t.Logf("持续连续观察开始，窗口 %s", window)
	for {
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-deadline.C:
			t.Logf("持续连续观察完成，耗时 %s，共 %d 轮三入口签名读取", time.Since(start).Round(time.Millisecond), passes)
			return
		case <-tick.C:
			p := current()
			if p.State != "ready" || len(p.Nodes) != 3 {
				t.Fatal("观察期间池失去就绪", p.State)
			}
			for _, member := range p.Nodes {
				c, err := storagepool.S3(member.Endpoint, p.CAPEM, creds)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				got, err := poolObjectHash(ctx, c, "i5-proof", "cross-ingress")
				cancel()
				if err != nil || got != digest {
					t.Fatal("观察期间签名读取失败", member.PrivateIP, got, err)
				}
			}
			passes++
			if passes%30 == 0 {
				t.Logf("持续观察已通过 %d 轮，经过 %s", passes, time.Since(start).Round(time.Second))
			}
		}
	}
}

func poolTLSLeaf(t *testing.T, member protocol.PoolMemberStatus, ca string) [32]byte {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ca)) {
		t.Fatal("证书验证池为空")
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 4 * time.Second}, "tcp", storagepool.Address(member.PrivateIP, member.Ports.S3), config)
	if err != nil {
		t.Fatal("TLS 握手失败", member.PrivateIP, err)
	}
	defer connection.Close()
	return sha256.Sum256(connection.ConnectionState().PeerCertificates[0].Raw)
}

func poolTestQuota(t *testing.T, pool protocol.StoragePool, host *storageroot.Manager, current func() protocol.StoragePool,
	read func(*minio.Client) bool, other *minio.Client) {
	t.Helper()
	member := pool.Nodes[0]
	data, err := host.Directory(t.Context(), protocol.StoragePoolData, pool.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(data, "data")
	control := filepath.Join(host.ExecutorDir, "pools", pool.ID, "control")
	before := poolTestCommand(t, "findmnt", "-no", "UUID", "--mountpoint", mount)
	beforeControl := poolTestCommand(t, "findmnt", "-no", "UUID", "--mountpoint", control)
	blocks := poolTestCommand(t, "stat", "-c", "%s %b", filepath.Join(data, "data.img"))
	if before != member.DataUUID || beforeControl != member.ControlUUID {
		t.Fatal("镜像挂载 UUID 与操作读回不同")
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(mount, &stat); err != nil {
		t.Fatal(err)
	}
	fill := filepath.Join(mount, "opsd-ci-quota-fill")
	free := int64(stat.Bavail) * int64(stat.Frsize)
	poolTestCommand(t, "fallocate", "-l", strconv.FormatInt(free-(32<<10), 10), fill)
	over := filepath.Join(mount, "opsd-ci-quota-over")
	if stderr, err := exec.Command("dd", "if=/dev/zero", "of="+over, "bs=1M", "count=1").CombinedOutput(); err == nil || !strings.Contains(string(stderr), "No space left on device") {
		t.Fatal("数据卷越限未返回 ENOSPC", err, string(stderr))
	}
	waitDatabase(t, "配额读回降级", 30*time.Second, func() bool {
		p := current()
		return p.State == "degraded" && !p.Nodes[0].Writable && p.Nodes[0].S3Ready
	})
	if !read(other) || poolTestCommand(t, "findmnt", "-no", "UUID", "--mountpoint", control) != beforeControl {
		t.Fatal("数据卷写满后旧对象或控制卷不可用")
	}
	t.Logf("D 配额写满返回 ENOSPC，M 挂载仍为 %s，另一个 S3 入口读取成功", beforeControl)
	for _, name := range []string{fill, over} {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	dev := poolTestCommand(t, "findmnt", "-no", "SOURCE", "--mountpoint", mount)
	device, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(dev), "queue/discard_max_bytes"))
	if err != nil || strings.TrimSpace(string(device)) != "0" {
		t.Fatal("loop 仍可 discard", err)
	}
	raw, err := exec.Command("fstrim", mount).CombinedOutput()
	if err == nil || !strings.Contains(string(raw), "discard operation is not supported") {
		t.Fatal("fstrim 未被预期禁用", err, string(raw))
	}
	poolTestCommand(t, "systemctl", "restart", poolTestUnit(protocol.StoragePoolData, pool.ID, host.Config))
	if got := poolTestCommand(t, "findmnt", "-no", "UUID", "--mountpoint", mount); got != before ||
		poolTestCommand(t, "stat", "-c", "%s %b", filepath.Join(data, "data.img")) != blocks {
		t.Fatal("重挂载后 UUID 或预分配块数不同")
	}
	waitDatabase(t, "数据重挂载读回", 20*time.Second, func() bool { return read(other) && current().State == "ready" })
	t.Logf("重挂载 UUID=%s，镜像 bytes/512-byte blocks=%s，discard=0，内容 SHA-256 一致", before, blocks)
}
