package cluster

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

type DatabaseInstance struct {
	Revision             uint64   `json:"revision"`
	ID                   string   `json:"id"`
	NodeID               string   `json:"node_id"`
	Name                 string   `json:"name"`
	Kind                 string   `json:"kind"`
	Address              string   `json:"address"`
	Socket               string   `json:"socket,omitempty"`
	Username             string   `json:"username"`
	CredentialsAvailable bool     `json:"credentials_available"`
	ContainerID          string   `json:"container_id,omitempty"`
	Running              bool     `json:"running"`
	ProtectedUsers       []string `json:"protected_users"`
	BackupRoot           string   `json:"backup_root,omitempty"`
}
type DatabaseInstanceInput struct {
	ExpectedRevision *uint64 `json:"expected_revision,omitempty"`
	CreateOnly       bool    `json:"create_only,omitempty"`
	DatabaseInstance
	Password       *string  `json:"password,omitempty"`
	ProtectedUsers []string `json:"protected_users"`
	BackupRoot     string   `json:"backup_root,omitempty"`
}
type DatabaseRef struct {
	NodeID           string `json:"node_id"`
	InstanceID       string `json:"instance_id"`
	InstanceRevision uint64 `json:"instance_revision"`
	Name             string `json:"name"`
	Address          string `json:"address"`
}
type ManagedDatabaseCluster struct {
	ProxyRouting *protocol.ProxyRoutingPolicy `json:"proxy_routing,omitempty"`
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Coordinator  string                       `json:"coordinator"`
	ClusterID    string                       `json:"cluster_id"`
	Revision     uint64                       `json:"revision"`
	Members      []DatabaseRef                `json:"members"`
	Proxies      []DatabaseRef                `json:"proxies"`
	WorkbenchURL string                       `json:"workbench_url"`
	Deleted      bool                         `json:"deleted"`
	UpdatedAt    time.Time                    `json:"updated_at"`
	Signature    []byte                       `json:"signature,omitempty"`
}
type ManagedDatabaseMember struct {
	DatabaseRef
	State protocol.DatabaseState `json:"status"`
}
type ManagedDatabaseStatus struct {
	Cluster   ManagedDatabaseCluster  `json:"cluster"`
	Members   []ManagedDatabaseMember `json:"members"`
	Proxies   []ManagedDatabaseMember `json:"proxies"`
	CheckedAt time.Time               `json:"checked_at"`
}

func databaseClusterBytes(c ManagedDatabaseCluster) []byte {
	c.Signature = nil
	b, _ := json.Marshal(c)
	return b
}
func (n *Node) DatabaseClusters(ctx context.Context) ([]ManagedDatabaseCluster, error) {
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM managed_database_clusters ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ManagedDatabaseCluster{}
	for rows.Next() {
		var b string
		var c ManagedDatabaseCluster
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (n *Node) DatabaseCluster(ctx context.Context, id string) (ManagedDatabaseCluster, error) {
	var b string
	var c ManagedDatabaseCluster
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM managed_database_clusters WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &c)
	}
	if e == nil && c.Deleted {
		e = sql.ErrNoRows
	}
	return c, e
}
func (n *Node) saveDatabaseCluster(ctx context.Context, c ManagedDatabaseCluster) (ManagedDatabaseCluster, error) {
	write := requeststate.Current(ctx)
	if write != nil {
		prior, e := requeststate.Lookup(ctx, n.DB, *write)
		if e == nil {
			e = json.Unmarshal(prior.Result, &c)
			return c, e
		}
		if errors.Is(e, requeststate.ErrConflict) {
			return c, fault("conflict", e.Error())
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return c, e
		}
	}
	if c.ID == "" {
		c.ID = NewOperationID(n.ID)
	}
	if previous, err := n.DatabaseCluster(ctx, c.ID); err == nil {
		// 路由策略通过预览和持久操作更新，普通定义编辑保留当前策略。
		c.ProxyRouting = previous.ProxyRouting
	}
	if OperationOwner(c.ID) != n.ID || c.Coordinator != n.ID || c.Name == "" || len(c.Name) > 128 || c.ClusterID == "" || len(c.Members) == 0 || len(c.Members) > 100 || len(c.Proxies) > 100 {
		return c, fault("invalid_request", "数据库集群配置无效")
	}
	if c.WorkbenchURL != "" {
		u, e := url.Parse(c.WorkbenchURL)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil {
			return c, fault("invalid_request", "工作台地址无效")
		}
	}
	local := false
	seen := map[string]bool{}
	for _, group := range [][]DatabaseRef{c.Members, c.Proxies} {
		for _, r := range group {
			key := r.NodeID + "/" + r.InstanceID
			if !n.Active(ctx, r.NodeID) || r.InstanceID == "" || seen[key] {
				return c, fault("invalid_request", "数据库成员或代理无效")
			}
			seen[key] = true
		}
	}
	for _, group := range [][]DatabaseRef{c.Members, c.Proxies} {
		for index := range group {
			if group[index].InstanceRevision == 0 {
				var instance DatabaseInstance
				if e := n.Call(ctx, group[index].NodeID, "database.instance.get", DatabaseRPC{InstanceID: group[index].InstanceID}, &instance); e != nil {
					return c, e
				}
				group[index].InstanceRevision = instance.Revision
			}
		}
	}
	for _, r := range c.Members {
		local = local || r.NodeID == n.ID
	}
	if !local {
		return c, fault("invalid_request", "协调节点须包含本机 Galera 实例")
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	if write != nil {
		prior, e := requeststate.Lookup(ctx, tx, *write)
		if e == nil {
			e = json.Unmarshal(prior.Result, &c)
			return c, e
		}
		if errors.Is(e, requeststate.ErrConflict) {
			return c, fault("conflict", e.Error())
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return c, e
		}
	}
	var revision uint64
	e = tx.QueryRowContext(ctx, "SELECT revision FROM managed_database_clusters WHERE id=?", c.ID).Scan(&revision)
	if e != nil && e != sql.ErrNoRows {
		return c, e
	}
	if revision != c.Revision {
		return c, fault("precondition_failed", "数据库集群配置已经变化")
	}
	var routingPending int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE kind='database' AND terminal=0 AND json_extract(body,'$.resource_id')=? AND json_extract(body,'$.action')='configure_proxy_routes'", c.ID).Scan(&routingPending); e != nil {
		return c, e
	}
	if routingPending > 0 {
		return c, fault("node_busy", "代理路由仍有未结束操作")
	}
	c.Revision++
	c.UpdatedAt = n.Now().UTC()
	c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
	b, _ := json.Marshal(c)
	_, e = tx.ExecContext(ctx, "INSERT INTO managed_database_clusters(id,body,revision) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,revision=excluded.revision", c.ID, string(b), c.Revision)
	if e != nil {
		return c, e
	}
	if e = n.appendTx(ctx, tx, "database_cluster", c.ID, c, n.Now()); e != nil {
		return c, e
	}
	if write != nil {
		if e = requeststate.Save(ctx, tx, requeststate.Receipt{Write: *write, Kind: "database", ResourceID: c.ID, Result: b, CreatedAt: n.Now().UTC()}); e != nil {
			return c, e
		}
	}
	if e = tx.Commit(); e == nil {
		n.notifyChange(Change{Resource: "databases"})
	}
	return c, e
}
func (n *Node) mergeDatabaseClusters(ctx context.Context, tx *sql.Tx, items []ManagedDatabaseCluster, known map[string]Member, revoked map[string]bool) error {
	if len(items) > 1000 {
		return fault("invalid_request", "数据库目录过大")
	}
	for _, c := range items {
		owner, ok := known[c.Coordinator]
		if revoked[c.Coordinator] {
			continue
		}
		key, e := identity.Decode(owner.Signing)
		if !ok || e != nil || OperationOwner(c.ID) != c.Coordinator || c.Revision == 0 || !ed25519.Verify(key, databaseClusterBytes(c), c.Signature) {
			return fault("forbidden", "数据库集群签名无效")
		}
		b, _ := json.Marshal(c)
		if _, e = tx.ExecContext(ctx, "INSERT INTO managed_database_clusters(id,body,revision) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,revision=excluded.revision WHERE managed_database_clusters.revision<excluded.revision", c.ID, string(b), c.Revision); e != nil {
			return e
		}
	}
	return nil
}
func (n *Node) databaseConnection(ctx context.Context, id string, version ...uint64) (protocol.DatabaseConnection, error) {
	var b string
	var revision uint64
	var secret Secret
	var c protocol.DatabaseConnection
	query := "SELECT connection,COALESCE(json_extract(body,'$.revision'),0) FROM managed_database_instances WHERE id=?"
	args := []any{id}
	if len(version) > 0 && version[0] > 0 {
		query = "SELECT connection,revision FROM managed_database_versions WHERE id=? AND revision=?"
		args = append(args, version[0])
	}
	e := n.DB.QueryRowContext(ctx, query, args...).Scan(&b, &revision)
	if e == nil {
		e = json.Unmarshal([]byte(b), &secret)
	}
	if e != nil {
		c.PoolKey = fmt.Sprintf("%s/%s/%d", n.ID, id, revision)
		return c, e
	}
	plain, e := unseal(secret, n.age)
	if e == nil {
		e = json.Unmarshal(plain, &c)
	}
	c.PoolKey = fmt.Sprintf("%s/%s/%d", n.ID, id, revision)
	return c, e
}
func (n *Node) databaseInstance(ctx context.Context, id string) (DatabaseInstance, error) {
	var b string
	var out DatabaseInstance
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM managed_database_instances WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &out)
	}
	return out, e
}
func (n *Node) saveDatabaseInstance(ctx context.Context, input DatabaseInstanceInput) (DatabaseInstance, error) {
	n.databaseMu.Lock()
	defer n.databaseMu.Unlock()
	i := input.DatabaseInstance
	if input.ExpectedRevision != nil {
		old, e := n.databaseInstance(ctx, i.ID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return i, e
		}
		if input.CreateOnly && e == nil || !input.CreateOnly && errors.Is(e, sql.ErrNoRows) || e == nil && old.Revision != *input.ExpectedRevision {
			return i, fault("precondition_failed", "数据库实例配置已经变化")
		}
	}
	if i.ID == "" {
		i.ID = identity.Random()
	}
	if len(i.ID) > 128 || i.Name == "" || len(i.Name) > 255 {
		return i, fault("invalid_request", "数据库实例无效")
	}
	c := protocol.DatabaseConnection{Kind: i.Kind, Address: i.Address, Socket: i.Socket, Username: i.Username, ProtectedUsers: input.ProtectedUsers, BackupRoot: input.BackupRoot}
	if input.Password != nil {
		c.Password = *input.Password
	} else if old, e := n.databaseConnection(ctx, i.ID); e == nil {
		c.Password = old.Password
	}
	if e := c.Validate(); e != nil {
		return i, fault("invalid_request", e.Error())
	}
	i.NodeID = n.ID
	i.CredentialsAvailable = c.Password != ""
	i.ProtectedUsers = c.ProtectedUsers
	if i.ProtectedUsers == nil {
		i.ProtectedUsers = []string{}
	}
	i.BackupRoot = c.BackupRoot
	i.Revision = 1
	if old, e := n.databaseInstance(ctx, i.ID); e == nil {
		i.Revision = old.Revision
		if i.Revision == 0 {
			i.Revision = 1
		}
		if connection, e := n.databaseConnection(ctx, i.ID); e != nil || !sameDatabaseConnection(c, connection) || old.Name != i.Name {
			i.Revision++
		}
	}
	b, _ := json.Marshal(c)
	secret, e := seal(b, []string{n.age.Recipient().String()})
	if e != nil {
		return i, e
	}
	encrypted, _ := json.Marshal(secret)
	body, _ := json.Marshal(i)
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return i, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO managed_database_versions(id,revision,connection) VALUES(?,?,?) ON CONFLICT(id,revision) DO NOTHING", i.ID, i.Revision, string(encrypted)); e != nil {
		return i, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO managed_database_instances(id,body,connection) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,connection=excluded.connection", i.ID, string(body), string(encrypted)); e != nil {
		return i, e
	}
	e = tx.Commit()
	if e == nil {
		n.databaseReads = nil
	}
	return i, e
}
func sameDatabaseConnection(a, b protocol.DatabaseConnection) bool {
	a.PoolKey, b.PoolKey = "", ""
	first, _ := json.Marshal(a)
	second, _ := json.Marshal(b)
	return string(first) == string(second)
}
func (n *Node) discoverDatabaseInstances(ctx context.Context) ([]DatabaseInstance, error) {
	reply, e := n.executeRead(ctx, &protocol.DatabaseDiscovery{})
	if e != nil {
		return nil, e
	}
	found, ok := reply.(*protocol.DatabaseDiscovered)
	if !ok {
		return nil, fmt.Errorf("数据库发现应答无效")
	}
	items := []DatabaseInstance{}
	for _, candidate := range found.Instances {
		i := DatabaseInstance{ID: candidate.ID, NodeID: n.ID, Name: candidate.Name, Kind: candidate.Connection.Kind, Address: candidate.Connection.Address, Socket: candidate.Connection.Socket, Username: candidate.Connection.Username, ContainerID: candidate.ContainerID, Running: candidate.Running}
		c := candidate.Connection
		if old, e := n.databaseConnection(ctx, i.ID); e == nil {
			c.Password = old.Password
			if old.Username != "" {
				i.Username = old.Username
			}
			if c.BackupRoot == "" {
				c.BackupRoot = old.BackupRoot
			}
			c.ProtectedUsers = old.ProtectedUsers
		}
		input := DatabaseInstanceInput{DatabaseInstance: i, ProtectedUsers: c.ProtectedUsers, BackupRoot: c.BackupRoot}
		if c.Password != "" {
			input.Password = &c.Password
		}
		i, e = n.saveDatabaseInstance(ctx, input)
		if e != nil {
			return nil, e
		}
		items = append(items, i)
	}
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM managed_database_instances ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var i DatabaseInstance
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &i); e != nil {
			return nil, e
		}
		if !slices.ContainsFunc(items, func(existing DatabaseInstance) bool { return existing.ID == i.ID }) {
			i.Running = false
			items = append(items, i)
		}
	}
	return items, rows.Err()
}
func (n *Node) managedDatabaseRead(ctx context.Context, id string, backups bool, user, host string, options ...DatabaseRPC) (protocol.DatabaseReadResult, error) {
	var request DatabaseRPC
	if len(options) > 0 {
		request = options[0]
	}
	c, e := n.databaseConnection(ctx, id, request.InstanceRevision)
	if e != nil {
		return protocol.DatabaseReadResult{}, e
	}
	reply, e := n.executeRead(ctx, &protocol.DatabaseRead{Scope: request.Scope, Connection: c, ClusterID: request.ExpectedClusterID, Backups: backups, User: user, Host: host})
	if e != nil {
		return protocol.DatabaseReadResult{}, e
	}
	result, ok := reply.(*protocol.DatabaseReadResult)
	if !ok {
		return protocol.DatabaseReadResult{}, fmt.Errorf("数据库读取应答无效")
	}
	return *result, nil
}
func (n *Node) databaseDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	var request DatabaseRPC
	if len(body) > 0 && string(body) != "null" {
		if e := json.Unmarshal(body, &request); e != nil {
			return nil, fault("invalid_request", "数据库请求无效")
		}
	}
	switch method {
	case "database.discover":
		return n.discoverDatabaseInstances(ctx)
	case "database.instance.save":
		return n.saveDatabaseInstance(ctx, request.Instance)
	case "database.instance.get":
		return n.databaseInstance(ctx, request.InstanceID)
	case "database.cluster.save":
		return n.saveDatabaseCluster(requeststate.With(ctx, request.Write), request.Cluster)
	case "database.cluster.get":
		return n.DatabaseCluster(ctx, request.ResourceID)
	case "database.read":
		if !request.Backups && request.User == "" && request.InstanceRevision == 0 && request.ExpectedClusterID == "" {
			return n.cachedManagedDatabaseRead(ctx, request.InstanceID, request.Scope)
		}
		return n.managedDatabaseRead(ctx, request.InstanceID, request.Backups, request.User, request.Host, request)
	case "database.proxy_routing.read":
		return n.readProxyRouting(ctx, request.InstanceID, request.Hostgroups)
	case "database.proxy_routing.state":
		return n.proxyRoutingState(ctx, request.ResourceID, request.InstanceID)
	case "database.proxy_routing.applied":
		return nil, n.noteProxyRouteApplied(ctx, request.ResourceID, request.InstanceID, request.OperationID, request.Action.Routing)
	case "database.execute":
		if OperationOwner(request.OperationID) != n.ID {
			return nil, fault("invalid_request", "数据库步骤目标无效")
		}
		if request.InstanceRevision == 0 {
			return nil, fault("invalid_request", "数据库步骤缺少连接版本")
		}
		c, e := n.databaseConnection(ctx, request.InstanceID, request.InstanceRevision)
		if e != nil {
			return nil, e
		}
		a := request.Action
		a.Connection = c
		if e = a.Validate(); e != nil {
			return nil, fault("invalid_request", e.Error())
		}
		if origin != n.ID {
			data, _ := json.Marshal(a)
			op, e := n.enqueue(ctx, origin, Request{ID: request.OperationID, Actor: request.Actor, ResourceID: request.ResourceID, Kind: "database_step", Payload: data})
			return &protocol.OperationUpdated{Record: op.OperationRecord}, e
		}
		return n.executeRead(ctx, &protocol.OperationSubmit{OperationID: request.OperationID, ContractVersion: protocol.ContractVersion, Action: protocol.OperationAction{Kind: "database", Database: &a}})
	case "database.query":
		if op, e := n.operation(ctx, request.OperationID); e == nil && op.Subject == "database_step" {
			if op.State == protocol.Succeeded {
				n.databaseMu.Lock()
				n.databaseReads = nil
				n.databaseMu.Unlock()
			}
			return &protocol.OperationQueryResult{OperationID: op.ID, Found: true, Record: &op.OperationRecord}, nil
		}
		reply, e := n.executeRead(ctx, &protocol.OperationQuery{OperationID: request.OperationID})
		if result, ok := reply.(*protocol.OperationQueryResult); ok && result.Record != nil && result.Record.State == protocol.Succeeded {
			n.databaseMu.Lock()
			n.databaseReads = nil
			n.databaseMu.Unlock()
		}
		return reply, e
	case "database.reconcile":
		return n.executeRead(ctx, &protocol.OperationReconcile{OperationID: request.OperationID})
	case "database.recover", "database.resolve":
		if op, e := n.operation(ctx, request.OperationID); e == nil && op.Subject == "database_step" {
			if method == "database.resolve" && op.State == protocol.Accepted {
				op, e = n.cancelQueued(ctx, op.ID)
				return &protocol.OperationUpdated{Record: op.OperationRecord}, e
			}
			decision := "operation.recover"
			if method == "database.resolve" {
				decision = "operation.resolve"
			}
			op, e = n.decide(ctx, decision, OperationDecision{ID: op.ID, ExpectedRevision: request.Revision, AcceptCurrent: true})
			return &protocol.OperationUpdated{Record: op.OperationRecord}, e
		}
		if method == "database.recover" {
			return n.executeRead(ctx, &protocol.OperationRecover{OperationID: request.OperationID, ExpectedRevision: request.Revision})
		}
		return n.executeRead(ctx, &protocol.OperationResolve{OperationID: request.OperationID, ExpectedRevision: request.Revision, AcceptCurrent: true})
	}
	return nil, fault("invalid_request", "未知数据库操作")
}

type DatabaseRPC struct {
	Hostgroups        *protocol.ProxyHostgroups `json:"hostgroups,omitempty"`
	Write             *requeststate.Write       `json:"write,omitempty"`
	Scope             string                    `json:"scope,omitempty"`
	Instance          DatabaseInstanceInput     `json:"instance"`
	Cluster           ManagedDatabaseCluster    `json:"cluster"`
	InstanceID        string                    `json:"instance_id"`
	InstanceRevision  uint64                    `json:"instance_revision"`
	ExpectedClusterID string                    `json:"expected_cluster_id"`
	Actor             string                    `json:"actor"`
	ResourceID        string                    `json:"resource_id"`
	OperationID       string                    `json:"operation_id"`
	Action            protocol.DatabaseAction   `json:"action"`
	Revision          uint64                    `json:"revision"`
	Backups           bool                      `json:"backups"`
	User              string                    `json:"user"`
	Host              string                    `json:"host"`
}

func (n *Node) ManagedDatabaseStatus(ctx context.Context, c ManagedDatabaseCluster, scope ...string) (ManagedDatabaseStatus, error) {
	out := ManagedDatabaseStatus{Cluster: c, Members: []ManagedDatabaseMember{}, Proxies: []ManagedDatabaseMember{}, CheckedAt: n.Now().UTC()}
	out.Members, out.Proxies = n.readManagedDatabaseGroup(ctx, c, scope...)
	return out, nil
}

func validateManagedDatabaseKind(kind string) bool { return strings.HasPrefix(kind, "database.") }
