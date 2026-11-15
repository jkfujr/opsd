package cluster

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type deploymentExecutor struct {
	mu            sync.Mutex
	d             protocol.DatabaseDeployment
	records       map[string]protocol.OperationRecord
	submits       int
	seedUpdates   int
	lost          bool
	failed        bool
	waiting       bool
	problems      []string
	dockerState   string
	unhealthy     bool
	candidates    []protocol.DatabaseCandidate
	databaseReads int
}

func (*deploymentExecutor) Storage(context.Context, StorageConfig) (Credentials, error) {
	return Credentials{}, nil
}
func (e *deploymentExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := protocol.DatabaseState{State: "ready", Version: "11.8.9", ClusterID: "test-galera", ClusterName: "业务集群", Primary: true, Ready: true, Synced: true, MemberAddress: "192.0.2.10:3306", ActualMembers: "192.0.2.10:3306", CheckedAt: time.Now().UTC()}
	state.Ready = !e.unhealthy
	switch p := p.(type) {
	case *protocol.DatabaseDeploymentCheck:
		return &protocol.DatabaseDeploymentEnvironment{DockerState: e.dockerState, FreeBytes: 10 << 30, Fingerprint: "initial", Problems: append([]string{}, e.problems...)}, nil
	case *protocol.DatabaseNetworkCheck:
		return &protocol.DatabaseNetworkChecked{Checked: true}, nil
	case *protocol.DatabaseDeploymentStateRead:
		return &protocol.DatabaseDeploymentState{ContainerID: strings.Repeat("a", 64), State: "running"}, nil
	case *protocol.OperationSubmit:
		if record, ok := e.records[p.OperationID]; ok {
			return &protocol.OperationUpdated{Record: record}, nil
		}
		if p.Action.Database.Kind == "deployment_seeds" {
			e.seedUpdates++
			record := protocol.OperationRecord{ID: p.OperationID, Revision: 2, State: protocol.Succeeded}
			e.records[p.OperationID] = record
			return &protocol.OperationUpdated{Record: record}, nil
		}
		e.submits++
		e.d = *p.Action.Database.Deployment
		record := protocol.OperationRecord{ID: p.OperationID, Revision: 2, State: protocol.Succeeded, Steps: []protocol.OperationStep{{Name: "等待数据同步", State: "completed"}}}
		if e.waiting {
			record.State = protocol.Applying
			record.Steps[0].State = "applying"
		}
		if e.failed {
			record.State = protocol.AttentionRequired
			record.Steps[0].State = "unknown"
			record.Error = &protocol.Error{Code: "dependency_unavailable", Message: "数据同步中断"}
		}
		e.records[p.OperationID] = record
		if e.lost {
			e.lost = false
			return nil, errors.New("部署应答丢失")
		}
		return &protocol.OperationUpdated{Record: record}, nil
	case *protocol.OperationQuery:
		record, found := e.records[p.OperationID]
		return &protocol.OperationQueryResult{OperationID: p.OperationID, Found: found, Record: &record}, nil
	case *protocol.OperationRecover:
		record, found := e.records[p.OperationID]
		if !found || record.Revision != p.ExpectedRevision {
			return nil, errors.New("原部署步骤版本已经变化")
		}
		record.Revision++
		record.State = protocol.Succeeded
		record.Error = nil
		record.Steps[0].State = "completed"
		e.records[p.OperationID] = record
		return &protocol.OperationUpdated{Record: record}, nil
	case *protocol.DatabaseRead:
		e.databaseReads++
		if p.Connection.Password == "wrong" {
			return nil, errors.New("管理凭据错误")
		}
		return &protocol.DatabaseReadResult{State: state}, nil
	case *protocol.DockerRead:
		return &protocol.DockerReadResult{State: protocol.DockerState{State: "ready", Containers: []protocol.ContainerSummary{{Name: e.d.ContainerName(), ContainerRef: protocol.ContainerRef{EngineID: "engine", ContainerID: strings.Repeat("a", 64)}}}}}, nil
	case *protocol.DatabaseDeploymentProfileRead:
		return &protocol.DatabaseDeploymentProfile{Deployment: e.d, State: state, DataBytes: 1 << 20, Problems: []string{}, Managed: e.d.ID != "", ContainerID: strings.Repeat("a", 64)}, nil
	case *protocol.DatabaseDiscovery:
		if e.candidates != nil {
			return &protocol.DatabaseDiscovered{Instances: append([]protocol.DatabaseCandidate{}, e.candidates...)}, nil
		}
		return &protocol.DatabaseDiscovered{Instances: []protocol.DatabaseCandidate{{ID: "legacy", Name: "已有 MariaDB", Running: true, Connection: protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "root", Password: "private-secret"}}}}, nil
	}
	return nil, fmt.Errorf("未处理的数据库检查消息：%T", p)
}
func deploymentNode(t *testing.T) (*Node, *deploymentExecutor) {
	t.Helper()
	n, _ := openTestNode(t)
	e := &deploymentExecutor{records: map[string]protocol.OperationRecord{}, dockerState: "ready"}
	n.Executor = e
	body := `{"backend":"none","state":"in_sync","revision":1,"rules":[],"default_policies":[]}`
	if _, err := n.DB.Exec("INSERT INTO firewall_config(id,body,confirmed) VALUES(1,?,'null')", body); err != nil {
		t.Fatal(err)
	}
	return n, e
}
func commitDeployment(t *testing.T, n *Node, preview DatabaseDeploymentPreview, key, mode string) Operation {
	t.Helper()
	op, err := n.SubmitDatabaseDeployment(t.Context(), DatabaseDeploymentCommit{Coordinator: preview.Coordinator, Token: preview.Token, Revision: preview.Revision, Actor: n.ID, Key: key, Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(key))), ExpectedMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func advanceDeploymentTest(t *testing.T, owner *Node, id string, nodes ...*Node) Operation {
	t.Helper()
	var op Operation
	for range 30 {
		for _, n := range nodes {
			n.advance()
		}
		var err error
		op, err = owner.GetOperation(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if op.State == protocol.Succeeded || op.State == protocol.AttentionRequired || op.State == protocol.Rejected {
			return op
		}
	}
	return op
}
func TestDatabaseDeploymentSingleNodeExpansionAndLostReply(t *testing.T) {
	a, x := deploymentNode(t)
	b, y := deploymentNode(t)
	joinTest(t, b, a)
	x.dockerState = "not_installed"
	x.lost = true
	input := DatabaseDeploymentInput{Mode: "create", Name: "单节点起步", Targets: []DatabaseDeploymentTargetInput{{NodeID: a.ID, Address: "192.0.2.10"}}}
	p, err := a.PreviewDatabaseDeployment(t.Context(), input, a.ID)
	if err != nil || len(p.Problems) > 0 || p.Token == "" || len(p.Targets) != 1 || p.Targets[0].DockerState != "not_installed" {
		t.Fatal(p.Problems, err)
	}
	op := commitDeployment(t, a, p, "create", "create_or_import")
	if duplicate := commitDeployment(t, a, p, "create", "create_or_import"); duplicate.ID != op.ID {
		t.Fatal("重复提交生成了新操作")
	}
	op = advanceDeploymentTest(t, a, op.ID, a, b)
	if op.State != protocol.AttentionRequired || x.submits != 1 {
		t.Fatal("未知应答未保留原步骤", op.State, x.submits)
	}
	if _, err = a.decideDatabase(t.Context(), "operation.recover", OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}, op); err != nil {
		t.Fatal(err)
	}
	op = advanceDeploymentTest(t, a, op.ID, a, b)
	if op.State != protocol.Succeeded || x.submits != 1 {
		t.Fatal("恢复重复部署", op.State, op.Error, x.submits)
	}
	c, err := a.DatabaseCluster(t.Context(), p.ClusterID)
	if err != nil || len(c.Members) != 1 || c.ClusterID != "test-galera" {
		t.Fatal("单节点未登记", c, err)
	}
	input = DatabaseDeploymentInput{Mode: "members", ClusterID: c.ID, ExpectedRevision: c.Revision, Targets: []DatabaseDeploymentTargetInput{{NodeID: b.ID, Address: "192.0.2.11"}}}
	preview, err := a.PreviewDatabaseDeployment(t.Context(), input, a.ID)
	if err != nil || len(preview.Problems) > 0 {
		t.Fatal(preview.Problems, err)
	}
	expand := commitDeployment(t, a, preview, "expand", "members")
	expand = advanceDeploymentTest(t, a, expand.ID, a, b)
	if expand.State != protocol.Succeeded || y.submits != 1 || y.d.Bootstrap || y.d.ClusterID != c.ClusterID || y.d.SSTPassword != x.d.SSTPassword {
		t.Fatal("扩容未沿用原集群", expand.State, expand.Error)
	}
	if x.seedUpdates != 2 || y.seedUpdates != 1 {
		t.Fatal("扩容未更新受管实例启动地址", x.seedUpdates, y.seedUpdates)
	}
	c, err = a.DatabaseCluster(t.Context(), c.ID)
	if err != nil || len(c.Members) != 2 {
		t.Fatal("扩容成员未登记", err)
	}
	var body string
	if err = a.DB.QueryRow("SELECT body FROM database_workflows WHERE id=?", op.ID).Scan(&body); err != nil || strings.Contains(body, x.d.SSTPassword) {
		t.Fatal("协调进度暴露凭据", err)
	}
}

func TestDatabaseDeploymentDiscoveryIsReadOnlyAndImportIsRepeatable(t *testing.T) {
	n, _ := deploymentNode(t)
	scan, err := n.ScanDatabases(t.Context(), DatabaseScanInput{Nodes: []string{n.ID}})
	if err != nil || len(scan.Groups) != 1 {
		t.Fatal(scan, err)
	}
	var count int
	if err = n.DB.QueryRow("SELECT count(*) FROM managed_database_instances").Scan(&count); err != nil || count != 0 {
		t.Fatal("扫描保存了实例", count, err)
	}
	item := scan.Groups[0].Members[0].Instance
	input := DatabaseDeploymentInput{Mode: "import", Name: "接入集群", GaleraID: scan.Groups[0].ClusterID, Imports: []DatabaseInstanceInput{{DatabaseInstance: item}}}
	p, err := n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(p.Problems, err)
	}
	if err = n.DB.QueryRow("SELECT count(*) FROM managed_database_instances").Scan(&count); err != nil || count != 0 {
		t.Fatal("预览保存了实例", err)
	}
	op := commitDeployment(t, n, p, "import", "create_or_import")
	op = advanceDeploymentTest(t, n, op.ID, n)
	if op.State != protocol.Succeeded {
		t.Fatal(op.State, op.Error)
	}
	c, err := n.DatabaseCluster(t.Context(), p.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
	if err != nil || len(p.Problems) > 0 {
		t.Fatal(p.Problems, err)
	}
	op = commitDeployment(t, n, p, "import-again", "create_or_import")
	op = advanceDeploymentTest(t, n, op.ID, n)
	if op.State != protocol.Succeeded {
		t.Fatal(op.State, op.Error)
	}
	after, err := n.DatabaseCluster(t.Context(), c.ID)
	if err != nil || len(after.Members) != 1 || after.Revision != c.Revision {
		t.Fatal("重复接入改变了集群", after.Revision, c.Revision, err)
	}
	input.GaleraID = "different"
	input.ClusterID = ""
	p, err = n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
	if err == nil && len(p.Problems) == 0 {
		t.Fatal("接受了不同 Galera 身份")
	}
}

func TestDatabaseDeploymentDiscoveryRejectsStoppedAndMissingContainers(t *testing.T) {
	n, e := deploymentNode(t)
	connection := protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "root", Password: "private-secret"}
	e.candidates = []protocol.DatabaseCandidate{
		{ID: "running", Name: "当前实例", ContainerID: strings.Repeat("a", 64), Running: true, Connection: connection},
		{ID: "stopped", Name: "旧实例", ContainerID: strings.Repeat("b", 64), Connection: connection},
		{ID: "stopped-proxy", Name: "旧代理", ContainerID: strings.Repeat("c", 64), Connection: protocol.DatabaseConnection{Kind: "proxysql", Address: "127.0.0.1:6032", Username: "admin"}},
	}
	for id, container := range map[string]string{"stopped-saved": strings.Repeat("b", 64), "removed": strings.Repeat("d", 64)} {
		_, err := n.saveDatabaseInstance(t.Context(), DatabaseInstanceInput{DatabaseInstance: DatabaseInstance{ID: id, Name: id, Kind: "galera", Address: connection.Address, Username: connection.Username, ContainerID: container, Running: true}, Password: &connection.Password})
		if err != nil {
			t.Fatal(err)
		}
	}
	scan, err := n.ScanDatabases(t.Context(), DatabaseScanInput{Nodes: []string{n.ID}})
	if err != nil || len(scan.Groups) != 1 || len(scan.Groups[0].Members) != 1 || len(scan.Groups[0].Proxies) != 0 || len(scan.Unresolved) != 0 || e.databaseReads != 1 {
		t.Fatal("停止或已移除容器读取了当前实例", scan, e.databaseReads, err)
	}
	for _, id := range []string{"stopped-saved", "removed", "stopped-proxy", "missing"} {
		if _, err := n.inspectDiscoveredDatabase(t.Context(), DatabaseInstanceInput{DatabaseInstance: DatabaseInstance{ID: id}}, true, "test-galera"); err == nil {
			t.Fatal("停止或已移除容器允许登记", id)
		}
	}
	var count int
	if err := n.DB.QueryRow("SELECT count(*) FROM managed_database_instances").Scan(&count); err != nil || count != 2 || e.databaseReads != 1 {
		t.Fatal("拒绝接入后仍读取或保存了实例", count, e.databaseReads, err)
	}
}

func TestDatabaseDiscoveryRunningCredentialsAndStoppedAfterScan(t *testing.T) {
	for _, kind := range []string{"container", "native", "wrong-password"} {
		t.Run(kind, func(t *testing.T) {
			n, e := deploymentNode(t)
			candidate := protocol.DatabaseCandidate{ID: "instance", Name: "实例", Running: true, Connection: protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "root", Password: "private-secret"}}
			if kind != "native" {
				candidate.ContainerID = strings.Repeat("a", 64)
			}
			if kind == "wrong-password" {
				candidate.Connection.Password = "wrong"
			}
			e.candidates = []protocol.DatabaseCandidate{candidate}
			scan, err := n.ScanDatabases(t.Context(), DatabaseScanInput{Nodes: []string{n.ID}})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "wrong-password" {
				if len(scan.Unresolved) != 1 || scan.Unresolved[0].Status.Error == nil {
					t.Fatal("运行实例凭据错误未保留补填入口", scan)
				}
				return
			}
			if len(scan.Groups) != 1 || len(scan.Groups[0].Members) != 1 || len(scan.Unresolved) != 0 {
				t.Fatal("可连接实例未发现", scan)
			}
			if kind == "container" {
				e.candidates[0].Running = false
				if _, err := n.inspectDiscoveredDatabase(t.Context(), DatabaseInstanceInput{DatabaseInstance: scan.Groups[0].Members[0].Instance}, true, "test-galera"); err == nil || e.databaseReads != 1 {
					t.Fatal("扫描后停止的容器仍可接入", e.databaseReads, err)
				}
			}
		})
	}
}

func TestDatabaseDeploymentPreviewRejectsChangedEnvironmentAndExpiry(t *testing.T) {
	n, e := deploymentNode(t)
	input := DatabaseDeploymentInput{Mode: "create", Name: "检查环境", Targets: []DatabaseDeploymentTargetInput{{NodeID: n.ID, Address: "192.0.2.10"}}}
	for _, problem := range []string{"端口冲突", "数据库目录非空", "磁盘不足"} {
		e.problems = []string{problem}
		p, err := n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
		if err != nil || p.Token != "" || len(p.Problems) != 1 {
			t.Fatal("阻断原因未返回预览", p.Problems, err)
		}
	}
	e.problems = nil
	p, err := n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	n.Now = func() time.Time { return p.ExpiresAt.Add(time.Second) }
	if _, err = n.SubmitDatabaseDeployment(t.Context(), DatabaseDeploymentCommit{Coordinator: n.ID, Token: p.Token, Revision: p.Revision, Actor: n.ID, Key: "expired", Digest: "expired", ExpectedMode: "create_or_import"}); err == nil {
		t.Fatal("接受了过期预览")
	}
	n.Now = time.Now
	op := commitDeployment(t, n, p, "changed", "create_or_import")
	e.problems = []string{"端口已占用"}
	op = advanceDeploymentTest(t, n, op.ID, n)
	if op.State != protocol.AttentionRequired || e.submits != 0 {
		t.Fatal("环境改变后仍然部署", op.State, e.submits)
	}
}

func TestDatabaseExpansionRechecksExistingCluster(t *testing.T) {
	for _, change := range []string{"version", "health"} {
		t.Run(change, func(t *testing.T) {
			a, source := deploymentNode(t)
			b, target := deploymentNode(t)
			joinTest(t, b, a)
			preview, err := a.PreviewDatabaseDeployment(t.Context(), DatabaseDeploymentInput{Mode: "create", Name: "原集群", Targets: []DatabaseDeploymentTargetInput{{NodeID: a.ID, Address: "192.0.2.10"}}}, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			op := commitDeployment(t, a, preview, "create", "create_or_import")
			if op = advanceDeploymentTest(t, a, op.ID, a, b); op.State != protocol.Succeeded {
				t.Fatal(op.State, op.Error)
			}
			c, err := a.DatabaseCluster(t.Context(), preview.ClusterID)
			if err != nil {
				t.Fatal(err)
			}
			preview, err = a.PreviewDatabaseDeployment(t.Context(), DatabaseDeploymentInput{Mode: "members", ClusterID: c.ID, ExpectedRevision: c.Revision, Targets: []DatabaseDeploymentTargetInput{{NodeID: b.ID, Address: "192.0.2.11"}}}, a.ID)
			if err != nil || len(preview.Problems) > 0 {
				t.Fatal(preview.Problems, err)
			}
			if change == "version" {
				source.d.Image = "mariadb:11.8.8"
			} else {
				source.unhealthy = true
			}
			op = commitDeployment(t, a, preview, "expand", "members")
			op = advanceDeploymentTest(t, a, op.ID, a, b)
			if op.State != protocol.AttentionRequired || target.submits != 0 {
				t.Fatal("原集群改变后仍然部署", op.State, target.submits)
			}
		})
	}
}

func TestDatabaseDeploymentCoordinatorReopenAndQueuedCancel(t *testing.T) {
	n, executor := deploymentNode(t)
	input := DatabaseDeploymentInput{Mode: "create", Name: "恢复创建", Targets: []DatabaseDeploymentTargetInput{{NodeID: n.ID, Address: "192.0.2.10"}}}
	preview, err := n.PreviewDatabaseDeployment(t.Context(), input, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := commitDeployment(t, n, preview, "cancel", "create_or_import")
	if _, err = n.cancelQueued(t.Context(), cancelled.ID); err != nil {
		t.Fatal(err)
	}
	n.advance()
	if executor.submits != 0 {
		t.Fatal("已取消操作仍然部署")
	}
	executor.lost = true
	op := commitDeployment(t, n, preview, "restart", "create_or_import")
	op = advanceDeploymentTest(t, n, op.ID, n)
	if op.State != protocol.AttentionRequired {
		t.Fatal(op.State)
	}
	var path string
	if err = n.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	n.Close()
	reopened, err := Open(t.Context(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	reopened.Executor = executor
	if duplicate := commitDeployment(t, reopened, preview, "restart", "create_or_import"); duplicate.ID != op.ID {
		t.Fatal("重启后生成了新操作")
	}
	if _, err = reopened.decideDatabase(t.Context(), "operation.recover", OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}, op); err != nil {
		t.Fatal(err)
	}
	op = advanceDeploymentTest(t, reopened, op.ID, reopened)
	if op.State != protocol.Succeeded || executor.submits != 1 {
		t.Fatal("协调节点重启后重复部署", op.State, op.Error, executor.submits)
	}
}

func TestDatabaseDeploymentMultipleTargetsKeepsCompletedMember(t *testing.T) {
	a, x := deploymentNode(t)
	b, y := deploymentNode(t)
	joinTest(t, b, a)
	y.failed = true
	preview, err := a.PreviewDatabaseDeployment(t.Context(), DatabaseDeploymentInput{Mode: "create", Name: "多节点创建", Targets: []DatabaseDeploymentTargetInput{{NodeID: a.ID, Address: "192.0.2.10"}, {NodeID: b.ID, Address: "192.0.2.11"}}}, a.ID)
	if err != nil || len(preview.Problems) > 0 {
		t.Fatal(preview.Problems, err)
	}
	op := commitDeployment(t, a, preview, "multi", "create_or_import")
	op = advanceDeploymentTest(t, a, op.ID, a, b)
	c, err := a.DatabaseCluster(t.Context(), preview.ClusterID)
	if err != nil || op.State != protocol.AttentionRequired || len(c.Members) != 1 || x.submits != 1 {
		t.Fatal("后续失败未保留首成员", op.State, c.Members, err)
	}
	if _, err = a.decideDatabase(t.Context(), "operation.recover", OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}, op); err != nil {
		t.Fatal(err)
	}
	op = advanceDeploymentTest(t, a, op.ID, a, b)
	c, err = a.DatabaseCluster(t.Context(), preview.ClusterID)
	if err != nil || op.State != protocol.Succeeded || len(c.Members) != 2 || x.submits != 1 || y.submits != 1 || y.d.Bootstrap || y.d.ClusterID != c.ClusterID {
		t.Fatal("多节点创建恢复失败", op.State, op.Error, err)
	}
}
