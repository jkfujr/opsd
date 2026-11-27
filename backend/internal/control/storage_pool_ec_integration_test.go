//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"github.com/jkfujr/opsd/internal/storageroot"
	"github.com/minio/minio-go/v7"
	"golang.org/x/sys/unix"
)

type testECMember = protocol.PoolECMember
type testECRequest = protocol.PoolECWorkerInput

func TestStoragePoolECConservativeNative(t *testing.T) {
	bin := os.Getenv("OPSD_TEST_BINARY")
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC") != "1" || bin == "" || os.Geteuid() != 0 {
		t.Skip("需要 OPSD-CI、四个隔离成员及固定 EC 镜像")
	}
	view, _, hosts, keys := makeECFixture(t, bin, nil)
	checkECConservative(t, view, hosts, keys)
}

func makeECFixture(t *testing.T, bin string, clock func() time.Time) (protocol.StoragePool, []*apiNode, map[string]*storageroot.Manager, protocol.PoolCredentials) {
	t.Helper()
	lab := poolTestLab(t, "i6")
	octet, portOffset := 101, 12503
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_REALTIME") == "1" {
		octet, portOffset = 105, 12543
	}
	entries, hosts, nodes := poolTestNodes(t, lab, bin, octet, clock, []int64{12, 12, 12, 7})
	image, err := hosts[entries[0].node.ID].Docker.StoragePoolECImage(t.Context())
	if err != nil || image == "" {
		t.Fatalf("本机固定 EC 镜像预检失败：%s %v", image, err)
	}
	t.Logf("本机固定 EC 镜像 %s", image)
	client, csrf := apiAdmin(t, entries[0])
	pool := func(path string, method string, input, output any, expected int) {
		t.Helper()
		key := ""
		if method == http.MethodPost && path == "/storage/pools" {
			key = "i6-create"
		}
		if status := apiCall(t, client, entries[0].web.URL, method, "/api/v2"+path, "", key, csrf, input, output); status != expected {
			t.Fatalf("%s %s 返回 %d，期望 %d：%+v", method, path, status, expected, output)
		}
	}
	var plan protocol.PoolPlan
	pool("/storage/pools/preview", http.MethodPost, protocol.PoolInput{
		Name: "OPSD EC 隔离池", Nodes: nodes, PortOffset: portOffset,
	}, &plan, http.StatusOK)
	if len(plan.Nodes) != 4 || len(plan.Nodes[3].Roles) != 3 {
		t.Fatal("隔离环境需要三控制成员和一普通成员", plan.Nodes)
	}
	for _, m := range plan.Nodes {
		if !strings.HasPrefix(m.ECImage, "sha256:") {
			t.Fatal("预览没有固定衍生镜像", m.NodeID)
		}
	}
	t.Cleanup(func() { cleanupECFixture(t, plan, hosts) })
	keys := protocol.PoolCredentials{AccessKey: "opsd-ci-" + identity.Random()[:8], SecretKey: identity.Random()}
	var operation cluster.Operation
	pool("/storage/pools", http.MethodPost, protocol.PoolCreate{PlanID: plan.PlanID, Credentials: keys}, &operation, http.StatusAccepted)
	waitDatabase(t, "四域池部署", 220*time.Second, func() bool {
		var op cluster.Operation
		pool("/operations/"+operation.ID, http.MethodGet, nil, &op, http.StatusOK)
		switch op.State {
		case protocol.AttentionRequired, protocol.Rejected, protocol.Failed:
			t.Fatal(op.State, op.Error)
		}
		return op.State == protocol.Succeeded
	})
	var view protocol.StoragePool
	pool("/storage/pools/"+plan.PoolID, http.MethodGet, nil, &view, http.StatusOK)
	if view.State != "ready" || len(view.Nodes) != 4 {
		t.Fatal("四域池未就绪", view.State, view.Nodes)
	}
	return view, entries, hosts, keys
}

func cleanupECFixture(t *testing.T, plan protocol.PoolPlan, hosts map[string]*storageroot.Manager) {
	t.Helper()
	ids, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=com.opsd.storage.pool="+plan.PoolID).Output()
	if t.Failed() {
		for _, id := range strings.Fields(string(ids)) {
			meta, _ := exec.Command("docker", "inspect", "-f", "{{.Name}} {{.State.Status}} {{json .Config.Cmd}}", id).Output()
			logs, _ := exec.Command("docker", "logs", "--tail", "28", id).CombinedOutput()
			t.Logf("%s %s", meta, logs)
		}
	}
	var units []string
	for _, host := range hosts {
		units = append(units, poolTestUnit(protocol.StoragePoolData, plan.PoolID, host.Config), poolTestUnit("pool_control", plan.PoolID, host.Config))
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
}

func TestStoragePoolECLifecycleNative(t *testing.T) {
	bin := os.Getenv("OPSD_TEST_BINARY")
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_LIFECYCLE") != "1" || bin == "" || os.Geteuid() != 0 {
		t.Skip("需要 OPSD-CI 原生四域池及固定 EC 镜像")
	}
	var shift atomic.Int64
	pool, entries, hosts, keys := makeECFixture(t, bin, func() time.Time {
		return time.Now().Add(time.Duration(shift.Load()))
	})
	paths := ecFixturePaths(t, pool, hosts)
	clients, readAll := seedECObjects(t, pool, keys, 10<<20)
	readAll("HTTP 转换前", 0)
	var name string
	var original []protocol.PoolMemberStatus
	var lastWrite time.Time
	for _, node := range pool.Nodes {
		files, err := os.ReadDir(filepath.Join(paths[node.NodeID], "replica"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".dat") {
				continue
			}
			info, err := file.Info()
			if err != nil {
				t.Fatal(err)
			}
			if !storagepool.ECEligible(time.Now().Add(61*time.Minute), info.ModTime(), info.Size(), 1024*protocol.PoolMiB) {
				continue
			}
			if name != "" && name != file.Name() {
				t.Fatal("存在多个达到触发条件的源卷", name, file.Name())
			}
			name = file.Name()
			original = append(original, node)
			if info.ModTime().After(lastWrite) {
				lastWrite = info.ModTime()
			}
			t.Logf("真实大小源 %s，占 %d / %d 字节", node.FailureDomain, info.Size(), 1024*protocol.PoolMiB)
		}
	}
	if len(original) != 2 {
		t.Fatal("真实大小源卷须有两个跨域副本", name, original)
	}
	assertFiles := func(expected bool) {
		t.Helper()
		for _, node := range original {
			path := filepath.Join(paths[node.NodeID], "replica", name)
			_, err := os.Stat(path)
			if expected && err != nil || !expected && !os.IsNotExist(err) {
				t.Fatalf("普通源卷 %s，应存在=%v：%v", path, expected, err)
			}
		}
	}
	var victim protocol.PoolMemberStatus
	for _, node := range pool.Nodes {
		if !slices.ContainsFunc(original, func(m protocol.PoolMemberStatus) bool { return m.NodeID == node.NodeID }) {
			victim = node
			break
		}
	}
	ownerClient, ownerCSRF := apiAdmin(t, entries[0])
	peerClient, peerCSRF := apiAdmin(t, entries[1])
	path := "/api/v2/storage/pools/" + pool.ID
	call := func(peer bool, method, path, tag, key, body string, want int, out any) http.Header {
		t.Helper()
		client, csrf, endpoint := ownerClient, ownerCSRF, entries[0].web.URL
		if peer {
			client, csrf, endpoint = peerClient, peerCSRF, entries[1].web.URL
		}
		req, err := http.NewRequestWithContext(t.Context(), method, endpoint+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", endpoint)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("If-Match", tag)
		req.Header.Set("Idempotency-Key", key)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			raw, _ := io.ReadAll(res.Body)
			t.Fatalf("%s %s 返回 %d，期望 %d：%s", method, path, res.StatusCode, want, raw)
		}
		validateTestResponse(t, req, res)
		if out != nil {
			if err := json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return res.Header
	}
	get := func(peer bool) (protocol.StoragePool, string) {
		var view protocol.StoragePool
		h := call(peer, http.MethodGet, path, "", "", "", 200, &view)
		return view, h.Get("ETag")
	}
	waitOperation := func(id string) {
		waitDatabase(t, "HTTP EC 操作完成", 5*time.Minute, func() bool {
			var op cluster.Operation
			call(true, http.MethodGet, "/api/v2/operations/"+id, "", "", "", 200, &op)
			if slices.Contains([]protocol.OperationState{protocol.Failed, protocol.Rejected, protocol.AttentionRequired}, op.State) {
				t.Fatal(op.State, op.Error)
			}
			return op.State == protocol.Succeeded
		})
	}
	initial, tag := get(false)
	_, peerTag := get(true)
	if !initial.ECSupported || initial.ECEnabled || tag == "" || tag != peerTag {
		t.Fatal("两入口池标记或默认 EC 策略不符", initial.ECSupported, initial.ECEnabled, tag, peerTag)
	}
	enable := fmt.Sprintf(`{"enabled":true,"parity_preferred_node_ids":[%q]}`, victim.NodeID)
	call(true, http.MethodPut, path+"/ec", "", "missing-match", enable, 428, nil)
	var op, replay cluster.Operation
	h := call(true, http.MethodPut, path+"/ec", tag, "enable-ec", enable, 202, &op)
	if !strings.HasSuffix(h.Get("Location"), "/operations/"+op.ID) || op.ID == "" {
		t.Fatal("缺少原操作位置", h.Get("Location"), op.ID)
	}
	waitOperation(op.ID)
	call(true, http.MethodPut, path+"/ec", tag, "enable-ec", enable, 202, &replay)
	call(true, http.MethodPut, path+"/ec", tag, "stale-tag", enable, 412, nil)
	before, _ := get(true)
	if replay.ID != op.ID || !before.ECEnabled || !slices.Equal(before.ParityNodes, []string{victim.NodeID}) || len(before.ECVolumes) != 0 {
		t.Fatal("EC 设置、幂等回执或静默条件不符", op.ID, replay.ID, before.ECVolumes)
	}
	assertFiles(true)
	t.Log("HTTP 跨入口版本、202、同键读回、旧版本 412 通过；卷未静默一小时，维持双副本")
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_REALTIME") == "1" {
		deadline := lastWrite.Add(time.Hour).Truncate(time.Second).Add(-time.Second)
		for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
			view, _ := get(true)
			if len(view.ECVolumes) != 0 && time.Now().Before(deadline) {
				t.Fatal("在真实静默阈值前已启动转换", view.ECVolumes)
			}
			assertFiles(true)
			t.Logf("静默等待 %s，双副本仍在，距一小时 %s", time.Since(lastWrite).Round(time.Second), remaining.Round(time.Second))
			select {
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			case <-time.After(min(time.Minute, remaining)):
			}
		}
	} else {
		shift.Store(int64(61 * time.Minute))
	}
	waitLayout := func(phase string, deadline time.Duration, match func(protocol.PoolECStatus) bool) protocol.PoolECStatus {
		t.Helper()
		var row protocol.PoolECStatus
		waitDatabase(t, phase, deadline, func() bool {
			view, _ := get(true)
			if len(view.ECVolumes) == 0 {
				return false
			}
			row = view.ECVolumes[0]
			if row.Conversion == "failed" && row.Error != "" {
				t.Fatal(row)
			}
			return match(row)
		})
		t.Logf("%s：卷 %d %s %s %s cleanup_pending=%v %s", phase, row.Volume, row.Generation, row.Layout, row.Conversion, row.CleanupPending, row.Error)
		return row
	}
	waitLayout("普通卷封存后进入编码", 90*time.Second, func(r protocol.PoolECStatus) bool { return r.Conversion == "encoding" })
	t.Logf("从最后一次实际写入到进入编码 %s", time.Since(lastWrite).Round(time.Millisecond))
	newData := []byte("封存后走其他可写卷")
	poolPutCheck(t, clients[0], clients[3], ecTestBucket, "new-during-ec", newData, false)
	waitLayout("EC 持久切换后待清理", 5*time.Minute, func(r protocol.PoolECStatus) bool { return r.Layout == "ec" && r.CleanupPending })
	assertFiles(true)
	container := poolTestContainer(pool.ID, victim.NodeID, "volume")
	poolTestCommand(t, "docker", "kill", "--signal", "KILL", container)
	waitLayout("目标失联时原清理未完成", 60*time.Second, func(r protocol.PoolECStatus) bool { return r.CleanupPending && r.Error != "" })
	assertFiles(true)
	poolTestCommand(t, "docker", "start", container)
	waitECVolume(t, victim)
	waitLayout("按原任务读回并清理双副本", 5*time.Minute, func(r protocol.PoolECStatus) bool { return r.Layout == "ec" && !r.CleanupPending })
	assertFiles(false)
	readAll("HTTP 删源后的真实大小 EC 读取", 1)
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_REPAIR") == "1" {
		shards, err := filepath.Glob(filepath.Join(paths[victim.NodeID], "ec", strings.TrimSuffix(name, ".dat")+".ec[0-9][0-9]"))
		if err != nil || len(shards) != 4 {
			t.Fatal("标记节点应承载四个校验片", shards, err)
		}
		shard := shards[0]
		want := testECFileHash(t, shard)
		before, _ := get(true)
		gen := before.ECVolumes[0].Generation
		poolTestCommand(t, "docker", "kill", "--signal", "KILL", container)
		if err := os.Rename(shard, shard+".held"); err != nil {
			t.Fatal(err)
		}
		poolTestCommand(t, "docker", "start", container)
		waitECVolume(t, victim)
		waitLayout("同代际少一片，卷降级", time.Minute, func(r protocol.PoolECStatus) bool { return r.State == "degraded" })
		waitLayout("按原分配修复后卷恢复", 3*time.Minute, func(r protocol.PoolECStatus) bool {
			_, err := os.Stat(shard)
			return err == nil && r.State == "ready" && r.Generation == gen
		})
		if got := testECFileHash(t, shard); got != want {
			t.Fatalf("协调任务修复的分片摘要不同：%x != %x", got, want)
		}
		if err := os.Remove(shard + ".held"); err != nil {
			t.Fatal(err)
		}
	}
	const restoreBody = `{"enabled":false,"restore_replicas":true}`
	var fills []string
	for _, member := range pool.Nodes[:3] {
		mount := filepath.Dir(paths[member.NodeID])
		var stat unix.Statfs_t
		if err := unix.Statfs(mount, &stat); err != nil {
			t.Fatal(err)
		}
		fill := filepath.Join(mount, "opsd-ci-ec-restore-capacity")
		size := int64(stat.Bavail)*int64(stat.Frsize) - max(member.DataBytes/10, 128*protocol.PoolMiB) - 64*protocol.PoolMiB
		if size <= 0 {
			t.Fatal("反向转换的测试池缺少预留空间", member.FailureDomain, size)
		}
		t.Cleanup(func() { _ = os.Remove(fill) })
		poolTestCommand(t, "fallocate", "-l", strconv.FormatInt(size, 10), fill)
		fills = append(fills, fill)
	}
	before, currentTag := get(true)
	var problem struct{ Code string }
	call(false, http.MethodPut, path+"/ec", currentTag, "restore-capacity", restoreBody, 507, &problem)
	after, _ := get(true)
	if problem.Code != "insufficient_storage" || after.ECRestore || !slices.Equal(before.ECVolumes, after.ECVolumes) {
		t.Fatal("容量不足改变了原 EC 或留下待处理的反向请求", problem.Code, before.ECVolumes, after.ECVolumes)
	}
	shardTotal := 0
	for _, dir := range paths {
		shards, err := filepath.Glob(filepath.Join(dir, "ec", strings.TrimSuffix(name, ".dat")+".ec[0-9][0-9]"))
		if err != nil {
			t.Fatal(err)
		}
		shardTotal += len(shards)
	}
	if shardTotal != storagepool.ECShardCount {
		t.Fatal("容量拒绝后原分片减少", shardTotal)
	}
	readAll("反向转换容量 507 后原 EC 读取", 2)
	for _, fill := range fills {
		if err := os.Remove(fill); err != nil {
			t.Fatal(err)
		}
	}
	_, currentTag = get(true)
	call(false, http.MethodPut, path+"/ec", currentTag, "restore-ec", restoreBody, 202, &op)
	waitLayout("双副本读回后待清理 EC", 5*time.Minute, func(r protocol.PoolECStatus) bool {
		return r.Layout == "replicated" && r.Direction == "restore" && r.CleanupPending
	})
	waitOperation(op.ID)
	waitLayout("HTTP 反向转换完成", 5*time.Minute, func(r protocol.PoolECStatus) bool {
		return r.Layout == "replicated" && r.Direction == "restore" && !r.CleanupPending
	})
	var restored [][32]byte
	for _, node := range pool.Nodes {
		replica := filepath.Join(paths[node.NodeID], "replica", name)
		if _, err := os.Stat(replica); err == nil {
			assertECReplicaOpen(t, pool, node, strings.TrimSuffix(name, ".dat"), true)
			restored = append(restored, testECFileHash(t, replica))
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		shards, err := filepath.Glob(filepath.Join(paths[node.NodeID], "ec", strings.TrimSuffix(name, ".dat")+".ec[0-9][0-9]"))
		if err != nil || len(shards) != 0 {
			t.Fatal("仍有原分片", shards, err)
		}
	}
	if len(restored) != 2 || restored[0] != restored[1] {
		t.Fatal("双副本摘要或数量不符", restored)
	}
	readAll("HTTP 反向转换后的真实大小双副本读取", 2)
	if got, err := poolObjectHash(t.Context(), clients[2], ecTestBucket, "new-during-ec"); err != nil || got != sha256.Sum256(newData) {
		t.Fatal("转换期间的新写入丢失", err)
	}
}

const ecTestBucket = "i6-pool-proof"

func seedECObjects(t *testing.T, pool protocol.StoragePool, keys protocol.PoolCredentials, bytesPerObject int) ([]*minio.Client, func(string, int)) {
	t.Helper()
	ctx := t.Context()
	var clients []*minio.Client
	for _, m := range pool.Nodes {
		c, err := storagepool.S3(m.Endpoint, pool.CAPEM, keys)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	if err := clients[0].MakeBucket(ctx, ecTestBucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, bytesPerObject)
	hashes := make(map[string][32]byte, 100)
	for i := range 100 {
		key := fmt.Sprintf("object-%03d", i)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		if _, err := clients[i%4].PutObject(ctx, ecTestBucket, key, bytes.NewReader(data), int64(len(data)),
			minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
			t.Fatal(key, err)
		}
		hashes[key] = sha256.Sum256(data)
	}
	readAll := func(phase string, offset int) {
		t.Helper()
		start := time.Now()
		recovering := strings.HasPrefix(phase, "失联 ")
		for i := range 100 {
			key := fmt.Sprintf("object-%03d", i)
			for {
				read, stop := context.WithTimeout(ctx, 10*time.Second)
				got, err := poolObjectHash(read, clients[(i+offset)%4], ecTestBucket, key)
				stop()
				if err == nil && got == hashes[key] {
					break
				}
				if err == nil || !recovering || time.Since(start) > 35*time.Second {
					t.Fatalf("%s 读对象 %s：%x %v", phase, key, got, err)
				}
			}
		}
		t.Logf("%s，100/100 个 %d 字节对象经四入口读回相同，%s", phase, bytesPerObject, time.Since(start).Round(time.Millisecond))
	}
	return clients, readAll
}

func ecFixturePaths(t *testing.T, pool protocol.StoragePool, hosts map[string]*storageroot.Manager) map[string]string {
	t.Helper()
	out := make(map[string]string, len(pool.Nodes))
	for _, m := range pool.Nodes {
		v, err := hosts[m.NodeID].Read(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, binding := range v.Bindings {
			if binding.Component == protocol.StoragePoolData && binding.ComponentID == pool.ID {
				out[m.NodeID] = filepath.Join(binding.Directory, "data", "volume")
			}
		}
		if out[m.NodeID] == "" {
			t.Fatal("没有池数据绑定", m.NodeID)
		}
	}
	return out
}

func checkECConservative(t *testing.T, pool protocol.StoragePool, hosts map[string]*storageroot.Manager, keys protocol.PoolCredentials) {
	t.Helper()
	ctx := t.Context()
	clients, readAll := seedECObjects(t, pool, keys, 352<<10)
	readAll("编码前", 0)
	var ec testECRequest
	var volumeName, worker, victim string
	var candidates []storagepool.ECMember
	paths := map[string]string{}
	byNode := ecFixturePaths(t, pool, hosts)
	byAddress := map[string]protocol.PoolMemberStatus{}
	original := map[string][32]byte{}
	for _, m := range pool.Nodes {
		address := fmt.Sprintf("%s.%d", storagepool.Address(m.PrivateIP, m.Ports.Volume), m.Ports.VolumeGRPC)
		byAddress[address] = m
		ec.Members = append(ec.Members, testECMember{Address: address, Domain: m.FailureDomain})
		candidates = append(candidates, storagepool.ECMember{NodeID: address, FailureDomain: m.FailureDomain, Available: m.DataBytes - m.DataUsedBytes, Parity: len(candidates) == 1})
		paths[address] = byNode[m.NodeID]
		files, err := os.ReadDir(filepath.Join(paths[address], "replica"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name, ok := strings.CutSuffix(file.Name(), ".dat")
			if !ok {
				continue
			}
			info, err := file.Info()
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() < 30*protocol.PoolMiB {
				continue
			}
			if volumeName != "" && name != volumeName {
				t.Fatal("测试对象分散在不同卷", name, volumeName)
			}
			volumeName = name
			ec.Sources = append(ec.Sources, address)
			path := filepath.Join(paths[address], "replica", file.Name())
			original[path] = testECFileHash(t, path)
		}
	}
	if len(ec.Sources) != 2 {
		t.Fatalf("原卷 %s 的副本为 %v，预期两份", volumeName, ec.Sources)
	}
	id := volumeName
	if i := strings.LastIndex(volumeName, "_"); i >= 0 {
		id = volumeName[i+1:]
		ec.Collection = volumeName[:i]
	}
	parsed, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	ec.Volume, ec.Generation = uint32(parsed), time.Now().UnixNano()
	places, err := storagepool.PlaceEC(candidates, 4*protocol.PoolMiB)
	if err != nil {
		t.Fatal(err)
	}
	for i := range ec.Members {
		for _, p := range places {
			if ec.Members[i].Address == p.NodeID {
				ec.Members[i].Shards = p.ShardIDs
			}
		}
		m := ec.Members[i]
		if len(m.Shards) > 0 && m.Address != ec.Sources[0] && m.Address != ec.Sources[1] {
			victim = m.Address
		}
		t.Logf("放置 %s %s 片号 %v", m.Address, m.Domain, m.Shards)
	}
	worker = poolTestContainer(pool.ID, byAddress[ec.Sources[0]].NodeID, "volume")
	if victim == "" {
		t.Fatal("需要一个原副本以外的分片承载节点")
	}
	t.Logf("源卷 %s，源副本 %v，代际 %d", volumeName, ec.Sources, ec.Generation)
	run := func(action string, wantError string) {
		t.Helper()
		ec.Action = action
		raw, err := json.Marshal(ec)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "docker", "exec", "-i", worker, "weed", "opsd-ec")
		cmd.Stdin = bytes.NewReader(raw)
		var logs bytes.Buffer
		cmd.Stderr = &logs
		out, err := cmd.Output()
		if wantError != "" {
			if err == nil || !strings.Contains(logs.String(), wantError) {
				t.Fatalf("%s 应拒绝 %s：%s %v", action, wantError, logs.String(), err)
			}
			t.Logf("%s 拒绝：%s", action, strings.TrimSpace(logs.String()))
			return
		}
		if err != nil {
			t.Fatalf("%s 失败：%s %v", action, logs.String(), err)
		}
		t.Logf("%s 完成：%s", action, strings.TrimSpace(string(out)))
	}
	unchanged := func() {
		t.Helper()
		for path, expected := range original {
			if got := testECFileHash(t, path); got != expected {
				t.Fatalf("原副本 %s 内容变化：%x", path, got)
			}
		}
		t.Log("原两份 .dat 的完整 SHA-256 和编码前一致")
	}
	run("seal", "")
	run("prepare", "")
	run("verify", "")
	unchanged()
	member := byAddress[victim]
	container := poolTestContainer(pool.ID, member.NodeID, "volume")
	base := filepath.Join(paths[victim], "ec", volumeName)
	var shardID int
	for _, p := range places {
		if p.NodeID == victim {
			shardID = p.ShardIDs[0]
		}
	}
	shard := fmt.Sprintf("%s.ec%02d", base, shardID)
	mutate := func(change func()) {
		t.Helper()
		poolTestCommand(t, "docker", "kill", "--signal", "KILL", container)
		change()
		poolTestCommand(t, "docker", "start", container)
	}
	move := func(from, to string) {
		t.Helper()
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	mutate(func() { move(shard, shard+".held") })
	waitECVolume(t, member)
	run("verify", "缺少分片")
	unchanged()
	mutate(func() { move(shard+".held", shard) })
	waitECVolume(t, member)
	run("verify", "")
	vif := base + ".vif"
	raw, err := os.ReadFile(vif)
	if err != nil {
		t.Fatal(err)
	}
	nv := bytes.Replace(raw, []byte(strconv.FormatInt(ec.Generation, 10)), []byte(strconv.FormatInt(ec.Generation+1, 10)), 1)
	if bytes.Equal(nv, raw) {
		t.Fatalf("实际卷信息缺少代际：%s", raw)
	}
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(vif, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mutate(func() { write(nv) })
	waitECVolume(t, member)
	run("verify", "代际")
	unchanged()
	mutate(func() { write(raw) })
	waitECVolume(t, member)
	run("verify", "")
	poolRegularMount(t, pool, worker, ec, false)
	for _, address := range ec.Sources {
		assertECReplicaOpen(t, pool, byAddress[address], volumeName, false)
	}
	run("verify", "")
	readAll("原副本卸载后的 EC 数据读取", 1)
	unchanged()
	poolRegularMount(t, pool, worker, ec, true)
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_TRANSITION") != "1" {
		return
	}
	for _, address := range ec.Sources {
		assertECReplicaOpen(t, pool, byAddress[address], volumeName, true)
	}
	run("clean-replicas", "")
	for path := range original {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("清理后原普通卷仍在 %s：%v", path, err)
		}
	}
	t.Log("切换后两份普通卷已实际清理，四域分片继续提供数据")
	readAll("删源后的 EC 数据读取", 2)
	after := []byte("切换到纠删码后新增的对象")
	poolPutCheck(t, clients[0], clients[3], ecTestBucket, "new-after-ec", after, false)
	for i, node := range pool.Nodes {
		container := poolTestContainer(pool.ID, node.NodeID, "volume")
		poolTestCommand(t, "docker", "kill", "--signal", "KILL", container)
		readAll("失联 "+node.FailureDomain, i)
		poolTestCommand(t, "docker", "start", container)
		waitECVolume(t, node)
	}
	if os.Getenv("OPSD_TEST_STORAGE_POOL_EC_REPAIR") == "1" {
		first, second := pool.Nodes[0], pool.Nodes[1]
		for _, node := range []protocol.PoolMemberStatus{first, second} {
			poolTestCommand(t, "docker", "kill", "--signal", "KILL", poolTestContainer(pool.ID, node.NodeID, "volume"))
		}
		read, stop := context.WithTimeout(ctx, 12*time.Second)
		var readErr error
		for i := 0; i < 20 && readErr == nil; i++ {
			_, readErr = poolObjectHash(read, clients[2], ecTestBucket, fmt.Sprintf("object-%03d", i))
		}
		stop()
		if readErr == nil {
			t.Fatal("少于 10 片时产生了错误的可读状态")
		}
		t.Logf("少于 10 个分片时 S3 返回读取失败：%v", readErr)
		for _, node := range []protocol.PoolMemberStatus{first, second} {
			poolTestCommand(t, "docker", "start", poolTestContainer(pool.ID, node.NodeID, "volume"))
			waitECVolume(t, node)
		}
		run("verify", "")
		want := testECFileHash(t, shard)
		mutate(func() { move(shard, shard+".held") })
		waitECVolume(t, member)
		run("repair", "")
		if got := testECFileHash(t, shard); got != want {
			t.Fatalf("同代际重建分片 %d 摘要不同：%x != %x", shardID, got, want)
		}
		if err := os.Remove(shard + ".held"); err != nil {
			t.Fatal(err)
		}
		readAll("原代际分片重建后的读取", 1)
	}
	ec.Restore = append([]string(nil), ec.Sources...)
	restoreDir := filepath.Join(paths[ec.Restore[1]], "replica")
	if err := os.Chmod(restoreDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(restoreDir, 0700) })
	run("restore-replicas", "暂存")
	for path := range original {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("第二副本暂存失败时普通卷不应已上架 %s：%v", path, err)
		}
	}
	run("verify", "")
	readAll("恢复第二份副本失败时", 1)
	if err := os.Chmod(restoreDir, 0700); err != nil {
		t.Fatal(err)
	}
	run("restore-replicas", "")
	var restored [32]byte
	for i, address := range ec.Restore {
		assertECReplicaOpen(t, pool, byAddress[address], volumeName, true)
		digest := testECFileHash(t, filepath.Join(paths[address], "replica", volumeName+".dat"))
		if i != 0 && digest != restored {
			t.Fatalf("两个跨域普通卷数据不同：%x %x", digest, restored)
		}
		restored = digest
	}
	t.Logf("两个跨域普通卷均已挂载，SHA-256=%x", restored)
	run("clean-shards", "")
	for _, m := range ec.Members {
		for _, shard := range m.Shards {
			path := filepath.Join(paths[m.Address], "ec", fmt.Sprintf("%s.ec%02d", volumeName, shard))
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("原分片仍在 %s：%v", path, err)
			}
		}
	}
	readAll("双副本恢复且分片清理后", 3)
	if got, err := poolObjectHash(ctx, clients[2], ecTestBucket, "new-after-ec"); err != nil || got != sha256.Sum256(after) {
		t.Fatal("反向转换期间新写入读取失败", err)
	}
}

func assertECReplicaOpen(t *testing.T, pool protocol.StoragePool, member protocol.PoolMemberStatus, name string, expected bool) {
	t.Helper()
	container := poolTestContainer(pool.ID, member.NodeID, "volume")
	pid := poolTestCommand(t, "docker", "inspect", "--format", "{{.State.Pid}}", container)
	fds, err := os.ReadDir(filepath.Join("/proc", pid, "fd"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, fd := range fds {
		path, _ := os.Readlink(filepath.Join("/proc", pid, "fd", fd.Name()))
		found = found || strings.HasSuffix(path, "/replica/"+name+".dat")
	}
	if found != expected {
		t.Fatalf("普通卷运行状态 %s=%v，预期 %v", member.FailureDomain, found, expected)
	}
}

func testECFileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := sha256.New()
	if _, err = io.Copy(dst, f); err != nil {
		t.Fatal(err)
	}
	return [32]byte(dst.Sum(nil))
}

func waitECVolume(t *testing.T, node protocol.PoolMemberStatus) {
	t.Helper()
	address := storagepool.Address(node.PrivateIP, node.Ports.VolumeGRPC)
	waitDatabase(t, "分片成员重新监听 "+address, 30*time.Second, func() bool {
		// 隔离成员只在自己的私网地址监听。
		out, err := exec.Command("ss", "-H", "-lnt", "sport", "=", fmt.Sprintf(":%d", node.Ports.VolumeGRPC)).Output()
		return err == nil && strings.Contains(string(out), address)
	})
}

func poolRegularMount(t *testing.T, p protocol.StoragePool, worker string, ec testECRequest, mount bool) {
	t.Helper()
	action := "unmount"
	if mount {
		action = "mount"
	}
	var masters []string
	for _, n := range p.Nodes {
		if n.Control() {
			masters = append(masters, storagepool.Address(n.PrivateIP, n.Ports.Master))
		}
	}
	var commands strings.Builder
	commands.WriteString("lock\n")
	for _, s := range ec.Sources {
		fmt.Fprintf(&commands, "volume.%s -node=%s -volumeId=%d\n", action, s, ec.Volume)
	}
	commands.WriteString("unlock\n")
	cmd := exec.CommandContext(t.Context(), "docker", "exec", "-i", worker, "weed", "shell", "-master="+strings.Join(masters, ","), "-connectTimeout=5s")
	cmd.Stdin = strings.NewReader(commands.String())
	text, err := cmd.CombinedOutput()
	if err != nil || strings.Contains(string(text), "error:") || strings.Contains(string(text), "not found") {
		t.Fatalf("volume.%s：%s %v", action, text, err)
	}
	t.Logf("volume.%s 原副本状态：%s", action, strings.TrimSpace(string(text)))
}
