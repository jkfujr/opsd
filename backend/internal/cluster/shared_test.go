package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestSharedMigrationWaitDoesNotAppendUnchangedState(t *testing.T) {
	n, worker := deploymentNode(t)
	id := stepID(n.ID, "shared-maintenance/"+n.cluster+"/"+protocol.SharedDatabaseName+"/initialize/")
	worker.records[id] = protocol.OperationRecord{ID: id, Revision: 1, State: protocol.Applying}
	state := SharedLocal{Binding: SharedBinding{ClusterID: "database", GaleraID: "galera", Database: protocol.SharedDatabaseName}, Phase: "preparing", Migration: &sharedMigration{Stage: "initialize", Source: DatabaseRef{NodeID: n.ID, InstanceID: "instance"}}}
	if err := n.saveSharedLocal(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	op, err := n.StartSharedInitialization(t.Context(), SharedInitialize{ClusterID: state.Binding.ClusterID}, "waiting-migration")
	if err != nil {
		t.Fatal(err)
	}
	n.advance()
	first, err := n.operation(t.Context(), op.ID)
	if err != nil || first.WaitingReason != "initialize_database" {
		t.Fatal(first, err)
	}
	n.advance()
	second, err := n.operation(t.Context(), op.ID)
	if err != nil || second.Revision != first.Revision {
		t.Fatal("等待状态重复写入历史并唤醒队列", second.Revision, first.Revision, err)
	}
}

func TestSharedBackendUsesConfirmedPortAndStandardAddress(t *testing.T) {
	ref := DatabaseRef{NodeID: "node", InstanceID: "database", Address: "172.30.91.11"}
	c := ManagedDatabaseCluster{}
	endpoint, err := sharedBackendEndpoint(c, ref)
	if err != nil || endpoint.Host != ref.Address || endpoint.Port != 3306 {
		t.Fatal("裸地址没有沿用 SQL 默认端口", endpoint, err)
	}
	c.ProxyRouting = &protocol.ProxyRoutingPolicy{Backends: []protocol.ProxyBackend{{NodeID: ref.NodeID, InstanceID: ref.InstanceID, ProxyEndpoint: protocol.ProxyEndpoint{Host: "10.0.0.1", Port: 3307}}}}
	endpoint, err = sharedBackendEndpoint(c, ref)
	if err != nil || endpoint.Host != "10.0.0.1" || endpoint.Port != 3307 {
		t.Fatal("已确认代理地址被默认端口替换", endpoint, err)
	}
}

func TestSharedMigrationFailureRetainsOriginalRecovery(t *testing.T) {
	n, _ := openTestNode(t)
	binding := SharedBinding{ClusterID: "database", GaleraID: "galera", Database: protocol.SharedDatabaseName}
	state := SharedLocal{Binding: binding, Phase: "preparing", Migration: &sharedMigration{Stage: "initialize", Source: DatabaseRef{NodeID: n.ID, InstanceID: "instance"}}}
	if err := n.saveSharedLocal(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	op, err := n.StartSharedInitialization(t.Context(), SharedInitialize{ClusterID: binding.ClusterID}, "original-migration")
	if err != nil {
		t.Fatal(err)
	}
	n.advance()
	failed, err := n.operation(t.Context(), op.ID)
	if err != nil || failed.State != protocol.AttentionRequired {
		t.Fatal("迁移错误丢失恢复入口", failed.State, err)
	}
	n.advance()
	unchanged, err := n.operation(t.Context(), op.ID)
	if err != nil || unchanged.Revision != failed.Revision {
		t.Fatal("同输入迁移错误自动重试", unchanged.Revision, err)
	}
	if err = n.RecoverSharedState(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := n.operation(t.Context(), op.ID)
	if err != nil || recovered.ID != op.ID || recovered.State != protocol.Applying || recovered.Revision <= failed.Revision {
		t.Fatal("迁移恢复改变原操作", recovered, err)
	}
}

func TestDatabaseConnectionVersionKeepsOriginalContainer(t *testing.T) {
	n, _ := openTestNode(t)
	input := DatabaseInstanceInput{DatabaseInstance: DatabaseInstance{ID: "database", Name: "数据库", Kind: "galera", Address: "127.0.0.1:3306", Username: "root", ContainerID: strings.Repeat("a", 64)}}
	first, err := n.saveDatabaseInstance(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.ContainerID = strings.Repeat("b", 64)
	second, err := n.saveDatabaseInstance(t.Context(), input)
	if err != nil || second.Revision <= first.Revision {
		t.Fatal("容器替换没有产生连接版本", second.Revision, err)
	}
	connection, err := n.databaseConnection(t.Context(), first.ID, first.Revision)
	if err != nil || connection.ContainerID != strings.Repeat("a", 64) {
		t.Fatal("历史连接改用新容器", connection.ContainerID, err)
	}
	derived := connection
	derived.PoolKey = "derived"
	derived.ProtectedDatabases = []string{protocol.SharedDatabaseName}
	if !sameDatabaseConnection(connection, derived) {
		t.Fatal("派生保护信息改变持久连接版本")
	}
}

func TestSharedInitializationRejectsInvalidBindingBeforeAdmission(t *testing.T) {
	n, _ := openTestNode(t)
	for _, input := range []SharedInitialize{{ClusterID: "existing", Database: "business"}, {Database: protocol.SharedDatabaseName}} {
		if _, err := n.StartSharedInitialization(t.Context(), input, "invalid"); err == nil {
			t.Fatal("无效绑定进入迁移队列")
		}
	}
	var count int
	if err := n.DB.QueryRow("SELECT COUNT(*) FROM queue").Scan(&count); err != nil || count != 0 || n.sharedEnabled() {
		t.Fatal("无效初始化改变节点状态", count, err)
	}
}

func TestSharedAccessOnlyRecipientCanRecoverCredentials(t *testing.T) {
	seed, _ := openTestNode(t)
	joining, _ := openTestNode(t)
	joinTest(t, joining, seed)
	local := SharedLocal{Access: Credentials{Username: protocol.SharedUsername(joining.ID), Password: "private-node-password"}, Monitor: Credentials{Username: protocol.SharedMonitorUser(joining.ID), Password: "private-monitor-password"}}
	access, err := seed.sealSharedAccess(t.Context(), joining.ID, local)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unseal(access.Secret, joining.age)
	if err != nil {
		t.Fatal("所属节点无法恢复凭据", err)
	}
	var recovered SharedLocal
	if err = json.Unmarshal(plain, &recovered); err != nil || recovered.Access != local.Access {
		t.Fatal("所属节点凭据变化", err)
	}
	if _, err = unseal(access.Secret, seed.age); err == nil {
		t.Fatal("其他节点能够读取数据库服务密码")
	}
}

func TestSharedUploadWaitsForStartupRecovery(t *testing.T) {
	n, _ := openTestNode(t)
	if err := n.saveSharedLocal(t.Context(), SharedLocal{Phase: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := n.uploadShared(t.Context(), nil); err == nil {
		t.Fatal("旧节点资料在核对共享版本前开始上传")
	}
}

func TestSharedConnectionBudgetKeepsExistingLimitAndDeduplicatesHostgroups(t *testing.T) {
	endpoint := protocol.ProxyEndpoint{Host: "10.0.0.1", Port: 3306}
	observed := protocol.ProxyRoutingObservation{Servers: []protocol.ProxyRouteServer{
		{ProxyEndpoint: endpoint, Hostgroup: 10, MaxConnections: 50},
		{ProxyEndpoint: endpoint, Hostgroup: 20, MaxConnections: 50},
	}}
	if got := sharedProxyConnections(observed)[endpoint.Key()]; got != 54 {
		t.Fatal("重复主机组改变原代理连接预算", got)
	}
}

func TestSharedJoinKeepsBindingAfterCoordinatorHandoff(t *testing.T) {
	seed, _ := openTestNode(t)
	joining, _ := openTestNode(t)
	token, _, _, err := seed.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	member, err := joining.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = seed.acceptMember(t.Context(), member, token); err != nil {
		t.Fatal(err)
	}
	directory, err := seed.Directory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	binding := SharedBinding{ClusterID: NewOperationID(seed.ID), GaleraID: "galera", Database: protocol.SharedDatabaseName, Revision: 1, Issuer: seed.ID}
	binding.Signature = ed25519.Sign(seed.signing, bindingBytes(binding))
	directory.Shared = &binding
	directory.Databases = []ManagedDatabaseCluster{{ID: binding.ClusterID, Coordinator: joining.ID, CoordinatorEpoch: 2, Revision: 2}}
	if err = joining.joinDirectory(t.Context(), directory); err != nil {
		t.Fatal("协调权转交后加入失败", err)
	}
	joining.sharedLocal = nil
	if err = joining.loadSharedLocal(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := joining.sharedState()
	if state == nil || state.Binding.ClusterID != binding.ClusterID || state.Phase != "joining" || joining.cluster != seed.cluster {
		t.Fatal("加入资料未一并保存", state)
	}
	var count int
	if err = joining.DB.QueryRow("SELECT COUNT(*) FROM managed_database_clusters").Scan(&count); err != nil || count != 0 {
		t.Fatal("共享定义提前使用目录副本", count, err)
	}
}

func TestSharedMigrationExportKeepsCiphertextAndOriginalReceipt(t *testing.T) {
	n, _ := openTestNode(t)
	c := ManagedDatabaseCluster{ID: NewOperationID(n.ID), Coordinator: n.ID, ClusterID: "galera", Revision: 1, Name: "业务集群"}
	input := DatabaseSubmission{Cluster: c, Change: DatabaseChange{Cluster: c.ID, ExpectedRevision: 1, Kind: "create_database", Database: "business", Charset: "utf8mb4"}}
	digest := sha256.Sum256([]byte("原始请求"))
	op, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "original", Actor: "account:" + n.ID + ":1", InputDigest: hex.EncodeToString(digest[:]), Payload: payload(input)})
	if err != nil {
		t.Fatal(err)
	}
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err = n.saveSharedLocal(t.Context(), SharedLocal{Binding: SharedBinding{ClusterID: c.ID, GaleraID: c.ClusterID, Database: protocol.SharedDatabaseName}, DataKey: key.String(), Phase: "preparing"}); err != nil {
		t.Fatal(err)
	}
	var original string
	for range 2 {
		page, err := n.exportShared(t.Context(), sharedExportRequest{Source: "flows"})
		if err != nil || len(page.Flows) != 1 || len(page.Receipts) != 1 {
			t.Fatal("迁移导出未保留原流程", err)
		}
		flow, receipt := page.Flows[0], page.Receipts[0]
		if flow.ID != op.ID || receipt.ResourceID != op.ID || receipt.Key != "original" || receipt.Digest != hex.EncodeToString(digest[:]) {
			t.Fatal("迁移改变原请求身份")
		}
		if original != "" && original != string(flow.Body) {
			t.Fatal("同版本流程在重试时改变密文")
		}
		original = string(flow.Body)
		var shared sharedFlowPayload
		if err = json.Unmarshal(flow.Body, &shared); err != nil {
			t.Fatal(err)
		}
		if _, err = unseal(shared.Input, key); err != nil {
			t.Fatal("共享接入密钥未能恢复原流程", err)
		}
	}
}

func TestPeerEndpointFallbackVerifiesSameIdentity(t *testing.T) {
	n, _ := openTestNode(t)
	seed, server := openTestNode(t)
	_, wrong := openTestNode(t)
	joinTest(t, n, seed)
	member, err := seed.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	member.Endpoints = []string{wrong.URL, server.URL}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	channel, err := n.openPeerChannel(ctx, member, seed.Key.Public)
	if err != nil {
		t.Fatal("备选端点未能连接正确节点", err)
	}
	channel.Close()
	member.Endpoints = []string{wrong.URL}
	if channel, err = n.openPeerChannel(ctx, member, seed.Key.Public); err == nil {
		channel.Close()
		t.Fatal("接受了其他节点的端点身份")
	}
}

func TestCurrentReadExpiryUsesReceiverCompletion(t *testing.T) {
	n, _ := openTestNode(t)
	n.closed = true
	for _, item := range []struct {
		age    time.Duration
		source time.Duration
		state  string
	}{
		{time.Second, -time.Hour, "ready"},
		{10 * time.Second, time.Hour, "stale"},
	} {
		at := n.Now().Add(item.source)
		n.current = map[string]*currentRead{n.ID + "/ping/null": {completed: n.Now().Add(-item.age), status: ReadStatus{State: "ready", DataAt: &at}}}
		if got := n.CurrentRead(t.Context(), n.ID, "ping", nil, nil, 2*time.Second); got.State != item.state {
			t.Fatal("来源时钟改变缓存有效期", got.State, item.state)
		}
	}
}
