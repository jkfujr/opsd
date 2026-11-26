//go:build linux

package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
	"github.com/jkfujr/opsd/internal/storage"
	"github.com/jkfujr/opsd/internal/transport"
	"github.com/minio/minio-go/v7"
	"go.yaml.in/yaml/v3"
)

type archiveNativeExecutor struct {
	docker    *dockerengine.Manager
	engine    *operations.Engine
	collector monitoring.Collector
	ctx       context.Context
	wg        sync.WaitGroup
	mu        sync.Mutex
}

func newArchiveNativeExecutor(t *testing.T, n *Node) *archiveNativeExecutor {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "executor.sqlite"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	d := dockerengine.New()
	e := &archiveNativeExecutor{docker: d, engine: &operations.Engine{Store: operations.Store{DB: db}, Containers: d}, ctx: ctx}
	t.Cleanup(func() { n.Close(); cancel(); e.wg.Wait(); db.Close() })
	return e
}
func (e *archiveNativeExecutor) Call(ctx context.Context, payload protocol.Payload) (protocol.Payload, error) {
	switch p := payload.(type) {
	case *protocol.MonitoringRead:
		return &protocol.MonitoringResult{Metrics: e.collector.Collect(p.Stream)}, nil
	case *protocol.ResourceMetricsRead:
		state := e.docker.List(ctx)
		out := &protocol.ResourceMetrics{Docker: &state, At: time.Now().UTC(), Metrics: []protocol.ResourceMetric{}, Errors: []string{}}
		if state.State != "ready" {
			out.Errors = append(out.Errors, "Docker: "+state.State)
			return out, nil
		}
		out.Metrics = append(out.Metrics, protocol.ResourceMetric{Metric: "opsd_docker_up", Value: 1})
		for _, c := range state.Containers {
			v := 0.0
			if c.State == "running" {
				v = 1
			}
			out.Metrics = append(out.Metrics, protocol.ResourceMetric{Metric: "opsd_container_running", ResourceID: c.ContainerID, Value: v})
		}
		return out, nil
	case *protocol.MonitoringStateRead:
		state, err := e.docker.MonitoringState(ctx, p.Deployment)
		return &state, err
	case *protocol.OperationSubmit:
		boot, _, err := operations.SystemClock()
		if err != nil {
			return nil, err
		}
		_, created, err := e.engine.Store.Receive(ctx, p.OperationID, p.ContractVersion, p.Action, boot)
		if err != nil {
			return nil, err
		}
		if created {
			e.start(p.OperationID)
		}
		r, err := e.engine.Record(ctx, p.OperationID)
		return &protocol.OperationUpdated{Record: r}, err
	case *protocol.OperationQuery:
		r, err := e.engine.Record(ctx, p.OperationID)
		if errors.Is(err, sql.ErrNoRows) {
			return &protocol.OperationQueryResult{OperationID: p.OperationID}, nil
		}
		return &protocol.OperationQueryResult{OperationID: p.OperationID, Found: err == nil, Record: &r}, err
	case *protocol.OperationRecover:
		if _, err := e.engine.Store.Request(ctx, p.OperationID, "recover", p.ExpectedRevision); err != nil {
			return nil, err
		}
		e.start(p.OperationID)
		r, err := e.engine.Record(ctx, p.OperationID)
		return &protocol.OperationUpdated{Record: r}, err
	}
	return nil, fmt.Errorf("此原生用例未使用 %s", payload.Kind())
}
func (e *archiveNativeExecutor) start(id string) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.mu.Lock()
		defer e.mu.Unlock()
		_ = e.engine.Run(e.ctx, id)
	}()
}
func (*archiveNativeExecutor) Storage(context.Context, StorageConfig) (Credentials, error) {
	return Credentials{}, sharedsql.ErrUnavailable
}

type archiveNative struct {
	root      string
	bin       string
	endpoint  string
	ca        string
	password  string
	bucket    string
	prefix    string
	rootS3    *minio.Client
	keys      protocol.S3Credentials
	sqlName   string
	component string
	s3        *exec.Cmd
	seed      string
}

func (f *archiveNative) command(t *testing.T, program string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	data, err := exec.CommandContext(ctx, program, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s 执行失败：%v %s", filepath.Base(program), err, strings.ReplaceAll(string(data), f.password, "[已隐藏]"))
	}
	return strings.TrimSpace(string(data))
}
func archiveWait(t *testing.T, label string, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	t.Fatal(label + "超时")
}

func (f *archiveNative) certificate(t *testing.T, name, address string) string {
	t.Helper()
	dir := filepath.Join(f.root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f.command(t, "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
		"-config", "/dev/null", "-subj", "/CN="+name, "-addext", "subjectAltName=IP:"+address,
		"-addext", "basicConstraints=critical,CA:TRUE", "-keyout", filepath.Join(dir, "private.key"), "-out", filepath.Join(dir, "public.crt"))
	body, err := os.ReadFile(filepath.Join(dir, "public.crt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func startArchiveNative(t *testing.T, n *Node) *archiveNative {
	t.Helper()
	if os.Getenv("OPSD_MONITORING_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("设置 OPSD_MONITORING_TEST=1 并以 root 运行 S3 与 Thanos 验收")
	}
	bin, err := filepath.Abs("../../../artifacts/monitoring-native/bin")
	if err != nil {
		t.Fatal(err)
	}
	projectSum := sha256.Sum256([]byte(n.cluster))
	// mc 把以 "-" 开头的参数当作开关；base64url 随机串可能以 "-" 开头，
	// 这里替换成等长等价字符，密码仍不可预测。
	password := strings.ReplaceAll(identity.Random(), "-", "x")
	f := &archiveNative{root: t.TempDir(), bin: bin, password: password, bucket: "opsd-monitoring", prefix: "opsd/metrics/" + n.cluster + "/", sqlName: "opsd-monitoring-sql-" + n.ID[:10], component: "opsd-monitoring-" + hex.EncodeToString(projectSum[:6])}
	for _, name := range []string{"minio", "mc"} {
		if _, err = os.Stat(filepath.Join(bin, name)); err != nil {
			t.Fatal("缺少 S3 验收程序，请先执行 scripts/check-monitoring.sh", err)
		}
	}
	f.command(t, filepath.Join(bin, "minio"), "--version")
	bridge := f.command(t, "docker", "network", "inspect", "bridge", "--format", "{{(index .IPAM.Config 0).Gateway}}")
	f.endpoint = "https://" + bridge + ":19000"
	f.ca = f.certificate(t, "s3-certs", bridge)
	log, err := os.Create(filepath.Join(f.root, "minio.log"))
	if err != nil {
		t.Fatal(err)
	}
	minioCmd := exec.Command(filepath.Join(bin, "minio"), "server", filepath.Join(f.root, "s3"), "--certs-dir", filepath.Join(f.root, "s3-certs"), "--address", "0.0.0.0:19000", "--console-address", "127.0.0.1:19001")
	minioCmd.Env = append(os.Environ(), "MINIO_ROOT_USER=opsd-native-root", "MINIO_ROOT_PASSWORD="+f.password)
	minioCmd.Stdout, minioCmd.Stderr = log, log
	if err = minioCmd.Start(); err != nil {
		t.Fatal(err)
	}
	f.s3 = minioCmd
	t.Cleanup(func() { minioCmd.Process.Kill(); minioCmd.Wait(); log.Close() })
	connection := protocol.S3Connection{Endpoint: f.endpoint, Bucket: f.bucket, Region: "us-east-1", AccessStyle: "path", CAPEM: f.ca}
	f.rootS3, err = objectstore.New(connection, protocol.S3Credential{AccessKey: "opsd-native-root", SecretKey: f.password})
	if err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "S3 启动", 20*time.Second, func() bool {
		_, err := f.rootS3.ListBuckets(t.Context())
		return err == nil
	})
	if err = f.rootS3.MakeBucket(t.Context(), f.bucket, minio.MakeBucketOptions{Region: connection.Region}); err != nil {
		t.Fatal(err)
	}
	mc := func(args ...string) string {
		return f.command(t, filepath.Join(bin, "mc"), append([]string{"--config-dir", filepath.Join(f.root, "mc")}, args...)...)
	}
	mcTrust := filepath.Join(f.root, "mc", "certs", "CAs")
	if err = os.MkdirAll(mcTrust, 0700); err == nil {
		err = os.WriteFile(filepath.Join(mcTrust, "opsd-ca.crt"), []byte(f.ca), 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
	mc("alias", "set", "native", f.endpoint, "opsd-native-root", f.password)
	for _, role := range []string{"receive", "store", "compact"} {
		key := protocol.S3Credential{AccessKey: "opsd-" + role, SecretKey: f.password + role}
		objectActions := []string{"s3:GetObject"}
		if role != "store" {
			objectActions = append(objectActions, "s3:PutObject")
		}
		if role == "compact" {
			objectActions = append(objectActions, "s3:DeleteObject")
		}
		statements := []any{
			map[string]any{"Effect": "Allow", "Action": objectActions, "Resource": []string{"arn:aws:s3:::" + f.bucket + "/" + f.prefix + "*"}},
			map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": []string{"arn:aws:s3:::" + f.bucket}, "Condition": map[string]any{"StringLike": map[string]any{"s3:prefix": []string{f.prefix + "*"}}}},
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetBucketLocation", "s3:GetLifecycleConfiguration"}, "Resource": []string{"arn:aws:s3:::" + f.bucket}},
		}
		policy, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": statements})
		filename := filepath.Join(f.root, role+".json")
		if err = os.WriteFile(filename, policy, 0600); err != nil {
			t.Fatal(err)
		}
		mc("admin", "user", "add", "native", key.AccessKey, key.SecretKey)
		mc("admin", "policy", "create", "native", role, filename)
		mc("admin", "policy", "attach", "native", role, "--user", key.AccessKey)
		switch role {
		case "receive":
			f.keys.Receive = key
		case "store":
			f.keys.Store = key
		case "compact":
			f.keys.Compact = key
		}
	}
	t.Cleanup(func() {
		for _, role := range []string{"receive", "query", "store", "compact"} {
			name := f.component + "-" + role
			if t.Failed() {
				logs, _ := exec.Command("docker", "logs", "--tail", "35", name).CombinedOutput()
				t.Logf("%s：%s", role, strings.ReplaceAll(string(logs), f.password, "[已隐藏]"))
			}
			_ = exec.Command("docker", "rm", "-f", name).Run()
		}
		_ = exec.Command("docker", "network", "rm", f.component+"_default").Run()
		_ = exec.Command("docker", "rm", "-f", f.sqlName).Run()
		_ = exec.Command("docker", "rm", "-f", f.component+"-control").Run()
	})
	f.command(t, "docker", "run", "-d", "--name", f.sqlName, "-e", "MARIADB_ROOT_PASSWORD="+f.password, "-e", "MARIADB_DATABASE=opsd_core", "-p", "127.0.0.1:19306:3306", "mariadb:11.8.9")
	config := mysql.NewConfig()
	config.User, config.Passwd, config.Net, config.Addr, config.DBName = "root", f.password, "tcp", "127.0.0.1:19306", "opsd_core"
	config.ParseTime = true
	config.Timeout, config.ReadTimeout, config.WriteTimeout = time.Second, 2*time.Second, 2*time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	archiveWait(t, "监控配置数据库启动", 60*time.Second, func() bool { return db.PingContext(t.Context()) == nil })
	if err = sharedsql.Initialize(t.Context(), db, n.cluster); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE shared_metadata SET phase='active' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	n.shared = &sharedsql.Store{DB: db, Cluster: n.cluster}
	self, err := n.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = n.shared.Transact(t.Context(), "", false, func(tx *sharedsql.Tx) error { return tx.Put(t.Context(), sharedObject("member", n.ID, 1, self), 0) }); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *archiveNative) put(t *testing.T, key string, body []byte) {
	t.Helper()
	if _, err := f.rootS3.PutObject(t.Context(), f.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringNativeS3ThanosAndLoad(t *testing.T) {
	if os.Getenv("OPSD_MONITORING_TEST") != "1" {
		t.Skip("设置 OPSD_MONITORING_TEST=1 运行真实 S3、Thanos 和负载验收")
	}
	n, _ := openTestNode(t)
	f := startArchiveNative(t, n)
	worker := newArchiveNativeExecutor(t, n)
	n.Executor = worker
	t.Log("验证 S3 前缀、凭据与配置持久操作")
	write := func(key string) *requeststate.Write {
		return &requeststate.Write{Actor: "account:" + n.ID + ":1", Key: key, Digest: strings.Repeat("a", 64)}
	}
	input := objectstore.Input{Target: objectstore.Target{ID: "s3-native", NodeID: n.ID, Name: "S3 验收", Enabled: true, S3Connection: protocol.S3Connection{Endpoint: f.endpoint, Bucket: f.bucket, Region: "us-east-1", AccessStyle: "path"}}, Credentials: &f.keys, CAPEM: &f.ca}
	target, err := n.saveStorageTarget(t.Context(), input, write("save-target"))
	if err != nil {
		t.Fatal(err)
	}
	f.probeCases(t, n, input, target, write)
	f.put(t, "files/keep.txt", []byte("保留普通文件"))
	check, err := n.checkStorageTarget(t.Context(), target.ID, f.prefix, target.Revision, write("check-target"))
	if err != nil || check.State != "ready" || !check.PrefixRestricted || !check.LifecycleChecked || check.ObjectKey != "" {
		t.Fatal("S3 受限连接检查失败", check, err)
	}
	if repeated, err := n.checkStorageTarget(t.Context(), target.ID, f.prefix, target.Revision, write("check-target")); err != nil || !repeated.CheckedAt.Equal(check.CheckedAt) {
		t.Fatal("同一连接检查重新执行", repeated, err)
	}
	compact, _ := objectstore.New(target.S3Connection, f.keys.Compact)
	if err = objectstore.VerifyOwner(t.Context(), compact, f.bucket, f.prefix, n.cluster); err != nil {
		t.Fatal(err)
	}
	if err = objectstore.VerifyOwner(t.Context(), compact, f.bucket, f.prefix, strings.Repeat("e", 32)); err == nil {
		t.Fatal("其他集群取得原监控目录")
	}
	local := metricsstore.DefaultConfig(n.cluster)
	local.Enabled, local.NodeID, local.StorageTargetID, local.Directory, local.DiskBudgetBytes = true, n.ID, target.ID, filepath.Join(f.root, "monitoring"), 1<<30
	n.Start()
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "deploy-monitoring", Kind: "monitoring_config", Payload: payload(local)})
	if err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "四个监控组件部署", 2*time.Minute, func() bool {
		current, e := n.GetOperation(t.Context(), op.ID)
		if e == nil && current.State == protocol.AttentionRequired {
			t.Fatal(current.Error)
		}
		return e == nil && current.State == protocol.Succeeded
	})
	archiveWait(t, "真实一秒采样与五秒上传", 20*time.Second, func() bool {
		status, e := n.ArchiveStatus(t.Context())
		return e == nil && status.LastSample != nil && status.LastReceive != nil && status.Deployment.State == "ready"
	})
	currentStatus, statusErr := n.ArchiveStatus(t.Context())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	c := currentStatus.Config
	if _, err = n.saveStorageTarget(t.Context(), objectstore.Input{Target: target, Credentials: &f.keys}, write("replace-active-credentials")); err == nil {
		t.Fatal("运行中替换了组件凭据")
	}
	emptyCA := ""
	if _, err = n.saveStorageTarget(t.Context(), objectstore.Input{Target: target, CAPEM: &emptyCA}, write("reset-active-ca")); err == nil {
		t.Fatal("运行中修改了归档信任链")
	}
	changed := target
	changed.Bucket = "another-bucket"
	if _, err = n.saveStorageTarget(t.Context(), objectstore.Input{Target: changed}, write("replace-active-bucket")); err == nil {
		t.Fatal("监控引用未保护存储目标")
	}
	q := metricsstore.Query{Metric: "opsd_node_cpu_usage_percent", From: time.Now().Add(-time.Minute), To: time.Now(), StepSeconds: 1, Targets: []metricsstore.QueryTarget{{NodeID: n.ID}}}
	archiveWait(t, "实时样本查询", 30*time.Second, func() bool {
		result, e := n.archiveSeries(t.Context(), q)
		return e == nil && len(result.Items) > 0
	})
	t.Log("验证 Receive、Compactor 与共享数据库故障")
	f.command(t, "docker", "stop", f.component+"-receive")
	before, _ := n.ArchiveStatus(t.Context())
	archiveWait(t, "接收失败后的持久缓冲", 12*time.Second, func() bool {
		after, e := n.ArchiveStatus(t.Context())
		return e == nil && after.Buffer.Batches >= before.Buffer.Batches+3
	})
	if err = n.Call(t.Context(), n.ID, "ping", nil, nil); err != nil {
		t.Fatal("监控故障中断了控制请求", err)
	}
	f.command(t, "docker", "start", f.component+"-receive")
	archiveWait(t, "Receive 重启后原批次补传", 30*time.Second, func() bool {
		after, e := n.ArchiveStatus(t.Context())
		return e == nil && after.Buffer.Batches < 6 && after.LastReceive != nil && after.LastReceive.After(*before.LastReceive)
	})
	f.command(t, "docker", "stop", f.component+"-compact", f.sqlName)
	before, _ = n.ArchiveStatus(t.Context())
	archiveWait(t, "共享数据库停用后继续接收", 12*time.Second, func() bool {
		after, e := n.ArchiveStatus(t.Context())
		return e == nil && !after.Paused && after.LastReceive != nil && after.LastReceive.After(*before.LastReceive)
	})
	f.command(t, "docker", "start", f.sqlName, f.component+"-compact")
	t.Log("验证磁盘 90% 暂停及 80% 恢复")
	pressurePath := filepath.Join(c.Directory, ".test-pressure")
	pressure, err := os.Create(pressurePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = pressure.Truncate(c.DiskBudgetBytes * 91 / 100); err != nil {
		t.Fatal(err)
	}
	pressure.Close()
	archiveWait(t, "磁盘达到暂停阈值", 12*time.Second, func() bool { s, _ := n.ArchiveStatus(t.Context()); return s.Paused })
	if err = os.Remove(pressurePath); err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "磁盘释放后恢复接收", 12*time.Second, func() bool { s, _ := n.ArchiveStatus(t.Context()); return !s.Paused })
	history := f.history(t, n, c)
	measurements := f.load(t, n)
	measurements["history"] = history
	for _, role := range []string{"receive", "query", "store", "compact"} {
		measurements[role] = f.command(t, "docker", "stats", "--no-stream", "--format", "{{json .}}", f.component+"-"+role)
	}
	measurements["status"], _ = n.ArchiveStatus(t.Context())
	t.Log("验证监控角色迁移的旧停新启与原操作")
	next, _ := openTestNode(t)
	joinTest(t, next, n)
	next.shared = &sharedsql.Store{DB: n.shared.DB, Cluster: n.cluster}
	self, err := next.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = n.shared.Transact(t.Context(), "", false, func(tx *sharedsql.Tx) error { return tx.Put(t.Context(), sharedObject("member", next.ID, 1, self), 0) }); err != nil {
		t.Fatal(err)
	}
	next.Executor = newArchiveNativeExecutor(t, next)
	nextTarget := target
	nextTarget.ID, nextTarget.NodeID, nextTarget.Revision = "s3-next", next.ID, 0
	nextTarget, err = next.saveStorageTarget(t.Context(), objectstore.Input{Target: nextTarget, Credentials: &f.keys, CAPEM: &f.ca}, write("save-next-target"))
	if err != nil {
		t.Fatal(err)
	}
	if check, err := next.checkStorageTarget(t.Context(), nextTarget.ID, f.prefix, nextTarget.Revision, write("check-next-target")); err != nil || check.State != "ready" {
		t.Fatal(check, err)
	}
	next.Start()
	moving, err := n.ArchiveConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	moving.NodeID, moving.StorageTargetID, moving.Directory = next.ID, nextTarget.ID, filepath.Join(f.root, "next-monitoring")
	move, err := n.Submit(t.Context(), next.ID, Request{Key: "move-monitoring", Kind: "monitoring_config", Payload: payload(moving)})
	if err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "监控角色迁移", 2*time.Minute, func() bool {
		o, e := next.GetOperation(t.Context(), move.ID)
		if e == nil && o.State == protocol.AttentionRequired {
			t.Fatal(o.Error)
		}
		return e == nil && o.State == protocol.Succeeded
	})
	stopped, err := n.GetOperation(t.Context(), stepID(n.ID, fmt.Sprintf("%s/monitoring/1", move.ID)))
	started, startErr := next.GetOperation(t.Context(), stepID(next.ID, fmt.Sprintf("%s/monitoring/2", move.ID)))
	if err != nil || startErr != nil || stopped.State != protocol.Succeeded || started.CreatedAt.Before(stopped.UpdatedAt) {
		t.Fatal("新实例在旧实例停止前启动", stopped, started, err, startErr)
	}
	if active := f.command(t, "docker", "ps", "--filter", "label=com.opsd.monitoring="+n.cluster, "--filter", "name=compact", "--format", "{{.ID}}"); active == "" || strings.Contains(active, "\n") {
		t.Fatal("活动 Compactor 数量无效", active)
	}
	measurements["role_migration"] = map[string]any{"from": n.ID, "to": next.ID, "old_stop_before_new_start": true, "active_compactors": 1}
	data, _ := json.MarshalIndent(measurements, "", "  ")
	output, _ := filepath.Abs("../../../artifacts/monitoring-native-result.json")
	if err = os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
	if kept, e := objectstore.Read(t.Context(), f.rootS3, f.bucket, "files/keep.txt"); e != nil || string(kept) != "保留普通文件" {
		t.Fatal("监控处理影响普通文件", e)
	}
	now, err := n.ArchiveConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now.Enabled = false
	stop, err := n.Submit(t.Context(), now.NodeID, Request{Key: "disable-monitoring", Kind: "monitoring_config", Payload: payload(now)})
	if err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "停用监控并保留数据", time.Minute, func() bool {
		o, e := n.GetOperation(t.Context(), stop.ID)
		return e == nil && o.State == protocol.Succeeded
	})
	if _, err = os.Stat(filepath.Join(c.Directory, "receive", "opsd", "wal")); err != nil {
		t.Fatal("停用时删除了监控数据", err)
	}
}

func (f *archiveNative) probeCases(t *testing.T, n *Node, input objectstore.Input, target objectstore.Target, write func(string) *requeststate.Write) {
	t.Helper()
	ctx := t.Context()
	probe, err := n.probeStorageTarget(ctx, target.ID, f.prefix, target.Revision, write("empty-bucket"))
	if err != nil || probe.State != "ready" {
		t.Fatal("空桶探测失败", probe, err)
	}
	denied, err := n.probeStorageTarget(ctx, target.ID, "files/", target.Revision, write("denied-prefix"))
	if err != nil || denied.State != "failed" || denied.Error != "S3 拒绝访问" {
		t.Fatal("探测未报告列举权限不足", denied, err)
	}
	otherCA := f.certificate(t, "other-ca", "127.0.0.1")
	input.ID, input.CAPEM = "s3-probe", &otherCA
	listOnly := protocol.S3Credentials{Receive: f.keys.Store, Store: f.keys.Store, Compact: f.keys.Store}
	input.Credentials = &listOnly
	separate, err := n.saveStorageTarget(ctx, input, write("save-list-only"))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := n.probeStorageTarget(ctx, separate.ID, f.prefix, separate.Revision, write("wrong-ca"))
	if err != nil || failed.State != "failed" || failed.Error != "S3 TLS 证书验证失败" {
		t.Fatal("错误 CA 的探测结果无效", failed, err)
	}
	separate, err = n.saveStorageTarget(ctx, objectstore.Input{Target: separate, CAPEM: &f.ca}, write("fix-ca"))
	if err != nil || separate.CAPEM != f.ca {
		t.Fatal("修复 CA 失败", err)
	}
	separate, err = n.saveStorageTarget(ctx, objectstore.Input{Target: separate}, write("preserve-ca"))
	if err != nil || separate.CAPEM != f.ca {
		t.Fatal("省略 CA 应保留原值", err)
	}
	ready, err := n.probeStorageTarget(ctx, separate.ID, f.prefix, separate.Revision, write("list-only"))
	if err != nil || ready.State != "ready" {
		t.Fatal("只读列举凭据未通过探测", ready, err)
	}
	archive, err := n.checkStorageTarget(ctx, separate.ID, f.prefix, separate.Revision, write("list-only-archive"))
	if err != nil || archive.State != "failed" || archive.Error != "S3 拒绝访问" {
		t.Fatal("只读列举凭据错误地通过了归档检查", archive, err)
	}
	t.Log("HTTPS S3 空桶列举通过，错误 CA 与越权前缀失败，只读凭据归档检查失败")
}

func (f *archiveNative) history(t *testing.T, n *Node, c metricsstore.Config) map[string]any {
	t.Helper()
	t.Log("验证 30 天、365 天、秒级原始历史和 S3 故障")
	f.command(t, "docker", "stop", f.component+"-compact", f.component+"-receive")
	type historicalBlock struct {
		id, directory string
		at            time.Time
		days          int
	}
	blocks := []historicalBlock{}
	// promtool 的块边界有一半取决于时钟对齐，同一用例在不同日期可能切出不同块数；
	// 所以按用例断言至少一块，而不是断言总数为 4。
	perCase := map[int]int{}
	for _, days := range []int{29, 31, 364, 366} {
		root := filepath.Join(f.root, fmt.Sprint("history-", days))
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Truncate(time.Hour).Add(10 * time.Minute)
		text := fmt.Sprintf("# TYPE opsd_node_cpu_usage_percent gauge\nopsd_node_cpu_usage_percent{node_id=%q} 42.125 %d\nopsd_node_cpu_usage_percent{node_id=%q} 43.125 %d\n# EOF\n", n.ID, at.Unix(), n.ID, at.Add(time.Second).Unix())
		// Prometheus TSDB 的块档位上限是 486h；跨度超过当前窗口就会被切成多个单样本块，
		// 那样 Thanos 既不会生成 5 分钟汇总，也不会生成 1 小时汇总。
		// Prometheus TSDB 的块档位上限是 486h；超过当前窗口的样本会被切成多个短块。
		blockDuration := 486 * time.Hour
		if days > 31 {
			// 边界样本保持真实年龄：364/366 天用于保留策略与查询断言。
			first := at.Add(-12 * 24 * time.Hour)
			text = fmt.Sprintf("# TYPE opsd_node_cpu_usage_percent gauge\nopsd_node_cpu_usage_percent{node_id=%q} 40 %d\nopsd_node_cpu_usage_percent{node_id=%q} 42.125 %d\nopsd_node_cpu_usage_percent{node_id=%q} 43.125 %d\n# EOF\n", n.ID, first.Unix(), n.ID, at.Unix(), n.ID, at.Add(time.Second).Unix())
			if days == 366 {
				// 边界样本的 12 天跨度会跨窗口被切开（实测切成两块、跨度 0），
				// Thanos 因此不会生成 5 分钟与 1 小时汇总。这里另造一个锚在 486h
				// 对齐窗口内的 12 天长块（实测 1 块、跨度 288h）；锚点比 at 再前移
				// 489h，保证它同样早于 365 天保留线，参与过期与删除标记断言。
				longRoot := filepath.Join(f.root, "history-long-366")
				if err := os.MkdirAll(longRoot, 0755); err != nil {
					t.Fatal(err)
				}
				// 注意：Go 的 Time.Truncate 相对零值时间取整，promtool 的块边界相对
				// Unix 纪元取整，两者相位差 302400 秒（84 小时），直接用 Truncate 会让两点跨窗口。
				// 这里按纪元取整，得到与 promtool 一致的 486h 窗口起点。
				rangeSeconds := int64(blockDuration / time.Second)
				shifted := at.Add(-489 * time.Hour).Unix()
				anchor := time.Unix(shifted-shifted%rangeSeconds, 0).UTC()
				longFirst := anchor.Add(time.Hour)
				longText := fmt.Sprintf("# TYPE opsd_node_cpu_usage_percent gauge\nopsd_node_cpu_usage_percent{node_id=%q} 40 %d\nopsd_node_cpu_usage_percent{node_id=%q} 42.125 %d\n# EOF\n", n.ID, longFirst.Unix(), n.ID, longFirst.Add(288*time.Hour).Unix())
				if err := os.WriteFile(filepath.Join(longRoot, "input.prom"), []byte(longText), 0644); err != nil {
					t.Fatal(err)
				}
				f.command(t, "docker", "run", "--rm", "--user", "0:0", "--entrypoint", "/bin/promtool", "-v", longRoot+":/work", "prom/prometheus:v3.5.0", "tsdb", "create-blocks-from", "openmetrics", "--max-block-duration="+blockDuration.String(), "/work/input.prom", "/work/blocks")
				longEntries, err := os.ReadDir(filepath.Join(longRoot, "blocks"))
				if err != nil {
					t.Fatal(err)
				}
				// 主用例的 promtool 还没跑，blocks 目录尚不存在；先建好再并入。
				if err = os.MkdirAll(filepath.Join(root, "blocks"), 0755); err != nil {
					t.Fatal(err)
				}
				for _, entry := range longEntries {
					if !entry.IsDir() || len(entry.Name()) != 26 {
						continue
					}
					if err = os.Rename(filepath.Join(longRoot, "blocks", entry.Name()), filepath.Join(root, "blocks", entry.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if err := os.WriteFile(filepath.Join(root, "input.prom"), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		f.command(t, "docker", "run", "--rm", "--user", "0:0", "--entrypoint", "/bin/promtool", "-v", root+":/work", "prom/prometheus:v3.5.0", "tsdb", "create-blocks-from", "openmetrics", "--max-block-duration="+blockDuration.String(), "/work/input.prom", "/work/blocks")
		entries, err := os.ReadDir(filepath.Join(root, "blocks"))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || len(entry.Name()) != 26 {
				continue
			}
			block := historicalBlock{entry.Name(), filepath.Join(root, "blocks", entry.Name()), at, days}
			metaPath := filepath.Join(block.directory, "meta.json")
			data, err := os.ReadFile(metaPath)
			if err != nil {
				t.Fatal(err)
			}
			var meta map[string]any
			if err = json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			labels := map[string]string{"cluster_id": n.cluster, "receive_replica": n.ID}
			if days == 366 {
				// 测试专用标签：让 366 组与 364 组保持不同标签集，避免压实把它们合并，
				// 否则过期删除会连带影响 364 天样本。
				labels["test_group"] = "retention"
			}
			meta["thanos"] = map[string]any{"version": 1, "labels": labels, "downsample": map[string]int{"resolution": 0}, "source": "receive"}
			data, _ = json.Marshal(meta)
			if err = os.WriteFile(metaPath, data, 0644); err != nil {
				t.Fatal(err)
			}
			blocks = append(blocks, block)
			perCase[days]++
			if days > 31 {
				if err = filepath.WalkDir(block.directory, func(path string, entry fs.DirEntry, err error) error {
					if err != nil || entry.IsDir() {
						return err
					}
					data, err := os.ReadFile(path)
					if err == nil {
						rel, _ := filepath.Rel(block.directory, path)
						f.put(t, f.prefix+block.id+"/"+filepath.ToSlash(rel), data)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				continue
			}
			target := filepath.Join(c.Directory, "receive", "opsd", block.id)
			if err = filepath.WalkDir(block.directory, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(block.directory, path)
				p := filepath.Join(target, rel)
				if entry.IsDir() {
					err = os.MkdirAll(p, 0755)
				} else {
					var data []byte
					data, err = os.ReadFile(path)
					if err == nil {
						err = os.WriteFile(p, data, 0644)
					}
				}
				if err == nil {
					err = os.Chown(p, 65534, 65534)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, days := range []int{29, 31, 364, 366} {
		if perCase[days] == 0 {
			t.Fatal("历史数据块缺失", days, "天")
		}
	}
	t.Logf("历史数据块：共 %d 块，按用例 %v", len(blocks), perCase)
	f.seed = blocks[0].directory
	if err := f.s3.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.s3.Process.Signal(syscall.SIGCONT) })
	f.command(t, "docker", "start", f.component+"-receive")
	archiveWait(t, "S3 停用期间保留未上传旧块", 45*time.Second, func() bool {
		s, err := n.ArchiveStatus(t.Context())
		return err == nil && s.Deployment.ArchivePendingBlocks > 0 && s.LastReceive != nil && time.Since(*s.LastReceive) < 7*time.Second
	})
	for _, block := range blocks {
		if block.days == 31 {
			if _, err := os.Stat(filepath.Join(c.Directory, "receive", "opsd", block.id, "meta.json")); err != nil {
				t.Fatal("S3 失败期间删除了未归档旧块", err)
			}
		}
	}
	if err := f.s3.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	archiveWait(t, "恢复 S3 后补齐历史块", 90*time.Second, func() bool {
		for _, block := range blocks {
			if _, err := f.rootS3.StatObject(t.Context(), f.bucket, f.prefix+block.id+"/meta.json", minio.StatObjectOptions{}); err != nil {
				return false
			}
		}
		return true
	})
	d, err := n.archiveDeployment(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	files, err := dockerengine.MonitoringFiles(d)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
		} `yaml:"services"`
	}
	if err = yaml.Unmarshal(files["compose.yml"], &compose); err != nil {
		t.Fatal(err)
	}
	compact := func(retention bool) {
		t.Helper()
		args := []string{"run", "--rm", "--user", "65534:65534", "--memory", "2g",
			"-v", filepath.Join(c.Directory, "compact") + ":/data",
			"-v", filepath.Join(c.Directory, "compact.yml") + ":/config/objstore.yml:ro",
			"-v", filepath.Join(c.Directory, "s3-ca.pem") + ":/config/s3-ca.pem:ro",
			"quay.io/thanos/thanos:" + metricsstore.ThanosVersion}
		for _, arg := range compose.Services["compact"].Command {
			if arg == "--wait" || !retention && strings.HasPrefix(arg, "--retention.") {
				continue
			}
			args = append(args, arg)
		}
		args = append(args, "--consistency-delay=0s")
		f.command(t, "docker", args...)
	}
	// 先由同版本 Compactor 生成历史汇总，再检查正式保留参数。
	compact(false)
	resolutions := map[int64]int{}
	for object := range f.rootS3.ListObjects(t.Context(), f.bucket, minio.ListObjectsOptions{Prefix: f.prefix, Recursive: true}) {
		if object.Err != nil {
			t.Fatal(object.Err)
		}
		if !strings.HasSuffix(object.Key, "/meta.json") {
			continue
		}
		data, err := objectstore.Read(t.Context(), f.rootS3, f.bucket, object.Key)
		if err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Thanos struct {
				Downsample struct {
					Resolution int64 `json:"resolution"`
				} `json:"downsample"`
			} `json:"thanos"`
		}
		if json.Unmarshal(data, &meta) != nil {
			t.Fatal("历史块元数据无效")
		}
		resolutions[meta.Thanos.Downsample.Resolution]++
	}
	if resolutions[0] == 0 || resolutions[300000] == 0 || resolutions[3600000] == 0 {
		t.Fatal("原始、五分钟与小时汇总未完整生成", resolutions)
	}
	f.command(t, "docker", "restart", f.component+"-store")
	for _, block := range blocks {
		if block.days != 31 && block.days != 364 {
			continue
		}
		q := metricsstore.Query{Metric: "opsd_node_cpu_usage_percent", From: block.at.Add(-time.Second), To: block.at.Add(3 * time.Second), StepSeconds: 1, Targets: []metricsstore.QueryTarget{{NodeID: n.ID}}}
		archiveWait(t, fmt.Sprint("S3 原始历史查询 ", block.days, " 天"), 45*time.Second, func() bool {
			result, err := n.archiveSeries(t.Context(), q)
			if err != nil {
				return false
			}
			for _, item := range result.Items {
				for _, point := range item.Points {
					if point.Value != nil && *point.Value == 42.125 {
						return true
					}
				}
			}
			return false
		})
	}
	compact(true)
	deleted := 0
	type retentionMark struct{ key, id, details string }
	var retentionMarks []retentionMark
	for object := range f.rootS3.ListObjects(t.Context(), f.bucket, minio.ListObjectsOptions{Prefix: f.prefix, Recursive: true}) {
		if object.Err != nil {
			t.Fatal(object.Err)
		}
		if !strings.HasSuffix(object.Key, "/deletion-mark.json") {
			continue
		}
		data, err := objectstore.Read(t.Context(), f.rootS3, f.bucket, object.Key)
		if err != nil {
			t.Fatal(err)
		}
		var marker map[string]any
		if json.Unmarshal(data, &marker) != nil {
			t.Fatal("删除标记无效")
		}
		// 压实替换写的是 "source of compacted block"，只有保留策略写的
		// "block exceeding retention of 8760h0m0s" 才能证明过期删除生效。
		details, _ := marker["details"].(string)
		if !strings.HasPrefix(details, "block exceeding retention") {
			continue
		}
		retentionMarks = append(retentionMarks, retentionMark{
			key:     object.Key,
			id:      strings.TrimSuffix(strings.TrimPrefix(object.Key, f.prefix), "/deletion-mark.json"),
			details: details,
		})
		marker["deletion_time"] = time.Now().Add(-49 * time.Hour).Unix()
		data, _ = json.Marshal(marker)
		f.put(t, object.Key, data)
		deleted++
	}
	// 过期块必须在原始、5 分钟、1 小时三档都被保留策略标记删除。
	// 标记原因里必须写明确切的 365 天，防止保留期被改大后仍然通过。
	resolutionMarks := map[int64]int{}
	for _, mark := range retentionMarks {
		if !strings.Contains(mark.details, "8760h0m0s") {
			t.Fatalf("保留原因不是 365 天：%s", mark.details)
		}
		data, err := objectstore.Read(t.Context(), f.rootS3, f.bucket, f.prefix+mark.id+"/meta.json")
		if err != nil {
			t.Fatalf("保留标记对应的块元数据不可读：%s %v", mark.id, err)
		}
		var meta struct {
			Thanos struct {
				Downsample struct {
					Resolution int64 `json:"resolution"`
				} `json:"downsample"`
			} `json:"thanos"`
		}
		if json.Unmarshal(data, &meta) != nil {
			t.Fatal("历史块元数据无效")
		}
		resolutionMarks[meta.Thanos.Downsample.Resolution]++
	}
	for _, resolution := range []int64{0, 300000, 3600000} {
		if resolutionMarks[resolution] == 0 {
			t.Fatalf("保留标记未覆盖分辨率 %d：%v", resolution, resolutionMarks)
		}
	}
	t.Logf("保留策略删除标记：共 %d 个，按分辨率 %v", len(retentionMarks), resolutionMarks)
	compact(true)
	// 被保留策略标记的块必须被物理清理。这里按标记本身核对，而不是要求原始块 ID
	// 全部消失——压实合并会合法地把多个原始块替换成新 ID。
	// 按块前缀递归列举：任何剩余对象都算未清理，任何列举错误都直接失败，
	// 避免把 403 或超时误当成"已删除"。
	for _, mark := range retentionMarks {
		prefix := f.prefix + mark.id + "/"
		for object := range f.rootS3.ListObjects(t.Context(), f.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if object.Err != nil {
				t.Fatalf("列举过期块失败：%s %v", prefix, object.Err)
			}
			t.Fatalf("删除延迟过后仍保留过期块对象：%s", object.Key)
		}
	}
	// 保留清理之后再查一次 364 天样本：此前的查询发生在启用保留策略之前，
	// 不能证明边界数据存活。
	for _, block := range blocks {
		if block.days != 364 {
			continue
		}
		q := metricsstore.Query{Metric: "opsd_node_cpu_usage_percent", From: block.at.Add(-time.Second), To: block.at.Add(3 * time.Second), StepSeconds: 1, Targets: []metricsstore.QueryTarget{{NodeID: n.ID}}}
		archiveWait(t, "保留清理后 364 天仍可读", 45*time.Second, func() bool {
			result, err := n.archiveSeries(t.Context(), q)
			if err != nil {
				return false
			}
			for _, item := range result.Items {
				for _, point := range item.Points {
					if point.Value != nil && *point.Value == 42.125 {
						return true
					}
				}
			}
			return false
		})
	}
	f.command(t, "docker", "start", f.component+"-compact")
	f.command(t, "docker", "restart", f.component+"-receive")
	archiveWait(t, "已归档旧块的本地清理", 2*time.Minute, func() bool {
		for _, block := range blocks {
			if block.days == 31 {
				if _, err := os.Stat(filepath.Join(c.Directory, "receive", "opsd", block.id)); !os.IsNotExist(err) {
					return false
				}
			}
			if block.days == 29 {
				if _, err := os.Stat(filepath.Join(c.Directory, "receive", "opsd", block.id)); err != nil {
					t.Fatal("清理删除了最近 30 天的数据", err)
				}
			}
		}
		return true
	})
	return map[string]any{"raw_and_downsampled_blocks": resolutions, "expired_marks": deleted, "raw_precision": 1, "local_retention_days": 30, "archive_retention_days": 365, "s3_outage_retained_unshipped_blocks": true}
}

func (f *archiveNative) load(t *testing.T, monitor *Node) map[string]any {
	t.Helper()
	measurements := map[string]any{}
	controlID := f.command(t, "docker", "create", "--name", f.component+"-control", "busybox:1.37.0", "sleep", "600")
	var controlRef protocol.ContainerRef
	for _, container := range dockerengine.New().List(t.Context()).Containers {
		if container.ContainerID == controlID {
			controlRef = container.ContainerRef
		}
	}
	if controlRef.ContainerID == "" {
		t.Fatal("本机执行测试容器不存在")
	}
	type source struct {
		node    *Node
		channel *transport.Channel
		buffer  *metricsstore.Buffer
	}
	sources := []source{}
	t.Cleanup(func() {
		for _, s := range sources {
			s.channel.Close()
		}
	})
	for _, count := range []int{20, 100} {
		for len(sources) < count {
			n, _ := openTestNode(t)
			joinTest(t, n, monitor)
			buffer, err := n.metricsBuffer(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			channel, err := n.openArchiveChannel(t.Context(), monitor.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { channel.Close() })
			sources = append(sources, source{n, channel, buffer})
		}
		for i := range sources {
			if err := archiveCall(t.Context(), sources[i].channel, "ping", nil, nil); err != nil {
				sources[i].channel.Close()
				channel, e := sources[i].node.openArchiveChannel(t.Context(), monitor.ID)
				if e != nil {
					t.Fatal(e)
				}
				sources[i].channel = channel
			}
		}
		for role, cpus := range map[string]string{"receive": "2", "query": "0.5", "store": "0.5", "compact": "1"} {
			f.command(t, "docker", "update", "--cpus", cpus, f.component+"-"+role)
		}
		if count == 100 {
			for _, suffix := range []string{"0", "1"} {
				original := filepath.Base(f.seed)
				id := "0000000000" + original[10:25] + suffix
				if err := filepath.WalkDir(f.seed, func(path string, entry fs.DirEntry, err error) error {
					if err != nil || entry.IsDir() {
						return err
					}
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					rel, _ := filepath.Rel(f.seed, path)
					if rel == "meta.json" {
						data = bytes.ReplaceAll(data, []byte(original), []byte(id))
					}
					f.put(t, f.prefix+id+"/"+filepath.ToSlash(rel), data)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
		}
		t.Logf("负载验收：%d 个认证节点，%d 样本/秒", count, count*50)
		latencies, queryLatencies := []int64{}, []int64{}
		var controlMax int64
		started := time.Now()
		sampleStart := started.UTC().Truncate(time.Second).Add(-5 * time.Second)
		for round := range 6 {
			at := sampleStart.Add(time.Duration(round*5) * time.Second)
			var wg sync.WaitGroup
			var mu sync.Mutex
			errors := make(chan error, count+2)
			if count == 100 && round == 0 {
				wg.Add(1)
				go func() { defer wg.Done(); f.command(t, "docker", "restart", f.component+"-compact") }()
			}
			for _, source := range sources {
				wg.Add(1)
				go func() {
					defer wg.Done()
					batches := []metricsstore.Batch{}
					for i := range 5 {
						stamp := at.Add(time.Duration(i) * time.Second).UnixMilli()
						b := metricsstore.Batch{At: stamp, Samples: []metricsstore.Sample{}}
						for metric, module := range metricsstore.MetricModules {
							if module == "monitoring" {
								b.Samples = append(b.Samples, metricsstore.Sample{Metric: metric, At: stamp, Value: float64(round + i + 1)})
							}
						}
						for len(b.Samples) < 50 {
							b.Samples = append(b.Samples, metricsstore.Sample{Metric: "opsd_container_running", ResourceID: fmt.Sprintf("%064d", len(b.Samples)), At: stamp, Value: 1})
						}
						b, err := source.buffer.Append(t.Context(), b, source.node.ID, time.Now(), metricsstore.BufferBytes)
						if err != nil {
							errors <- err
							return
						}
						if b.Sequence == 0 {
							errors <- fmt.Errorf("负载采样时间重复：节点 %s，轮次 %d，时间 %d", source.node.ID, round, stamp)
							return
						}
						batches = append(batches, b)
					}
					begin := time.Now()
					ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
					defer cancel()
					var ack []archiveAck
					if err := archiveCall(ctx, source.channel, "upload", archiveUpload{Batches: batches}, &ack); err != nil {
						errors <- err
						return
					}
					if len(ack) != 5 {
						errors <- fmt.Errorf("批次确认数量为 %d", len(ack))
						return
					}
					for _, a := range ack {
						if a.Error != "" {
							errors <- fmt.Errorf("批次被拒绝：%s", a.Error)
							return
						}
					}
					mu.Lock()
					latencies = append(latencies, time.Since(begin).Milliseconds())
					mu.Unlock()
				}()
			}
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					queryStarted := time.Now()
					q := metricsstore.Query{Metric: "opsd_node_cpu_usage_percent", From: time.Now().Add(-time.Hour), To: time.Now(), StepSeconds: 5}
					for _, s := range sources {
						q.Targets = append(q.Targets, metricsstore.QueryTarget{NodeID: s.node.ID})
					}
					_, err := monitor.archiveSeries(t.Context(), q)
					if err != nil {
						errors <- err
					}
					mu.Lock()
					queryLatencies = append(queryLatencies, time.Since(queryStarted).Milliseconds())
					mu.Unlock()
				}()
			}
			controlStart := time.Now()
			if err := sources[0].node.Call(t.Context(), monitor.ID, "ping", nil, nil); err != nil || time.Since(controlStart) > 3*time.Second {
				t.Fatal("负载期间控制请求失败或超过三秒", err, time.Since(controlStart))
			}
			controlMax = max(controlMax, time.Since(controlStart).Milliseconds())
			if count == 100 && round == 2 {
				op, err := monitor.Submit(t.Context(), monitor.ID, Request{Key: "native-control-during-load", Kind: "container", Payload: payload(protocol.ContainerAction{ContainerRef: controlRef, Action: "start"})})
				if err != nil {
					t.Fatal(err)
				}
				archiveWait(t, "负载期间本机持久操作", 5*time.Second, func() bool {
					o, e := monitor.GetOperation(t.Context(), op.ID)
					if e == nil && operations.Terminal(o.State) && o.State != protocol.Succeeded {
						t.Fatalf("负载期间本机持久操作失败：%s，%+v", o.State, o.Error)
					}
					return e == nil && o.State == protocol.Succeeded
				})
				measurements["local_operation_during_load"] = op.ID
				measurements["components_during_load"] = f.command(t, "docker", "stats", "--no-stream", "--format", "{{json .}}", f.component+"-receive", f.component+"-query", f.component+"-store", f.component+"-compact")
				state, err := monitor.ArchiveStatus(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				measurements["disk_bytes_during_load"] = state.Deployment.DiskUsedBytes
			}
			wg.Wait()
			close(errors)
			for err := range errors {
				t.Fatal(err)
			}
			deadline := started.Add(time.Duration(round+1) * 5 * time.Second)
			if left := time.Until(deadline); left > 0 {
				select {
				case <-time.After(left):
				case <-t.Context().Done():
					t.Fatal(t.Context().Err())
				}
			}
		}
		var maxLatency int64
		for _, latency := range latencies {
			maxLatency = max(maxLatency, latency)
		}
		var queryMax int64
		for _, latency := range queryLatencies {
			queryMax = max(queryMax, latency)
		}
		measurements[strconv.Itoa(count)+"_nodes"] = map[string]any{"duration_seconds": time.Since(started).Seconds(), "samples": count * 50 * 30, "upload_max_ms": maxLatency, "query_max_ms": queryMax, "control_max_ms": controlMax, "upload_batches": len(latencies) * 5, "concurrent_queries": 2}
		if time.Since(started) > 40*time.Second {
			t.Fatal("上传吞吐未达到采样速率", count, time.Since(started))
		}
		for _, s := range sources {
			_ = archiveCall(t.Context(), s.channel, "ping", nil, nil)
		}
	}
	response, err := http.Get("http://127.0.0.1:19295/metrics")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	compactions := 0.0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "thanos_compact_group_compactions_total{") || strings.HasPrefix(line, "thanos_compact_group_compactions_total ") {
			fields := strings.Fields(line)
			value, _ := strconv.ParseFloat(fields[len(fields)-1], 64)
			compactions += value
		}
	}
	if compactions < 1 {
		t.Fatal("负载期间未执行实际数据块归并")
	}
	measurements["compactions_during_load"] = compactions
	measurements["component_cpu_quota_total"] = 4
	return measurements
}
