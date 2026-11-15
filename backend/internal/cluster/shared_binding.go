package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type SharedBinding struct {
	ClusterID string `json:"cluster_id"`
	GaleraID  string `json:"galera_id"`
	Database  string `json:"database"`
	Revision  uint64 `json:"revision"`
	Issuer    string `json:"issuer"`
	Signature []byte `json:"signature,omitempty"`
}
type SharedLocal struct {
	Consoles      SharedConsoles               `json:"consoles"`
	DataKey       string                       `json:"data_key"`
	AccountSource DatabaseRef                  `json:"account_source"`
	Binding       SharedBinding                `json:"binding"`
	Cluster       ManagedDatabaseCluster       `json:"cluster"`
	Phase         string                       `json:"phase"`
	Access        Credentials                  `json:"access"`
	Monitor       Credentials                  `json:"monitor"`
	Admin         protocol.DatabaseConnection  `json:"admin"`
	SQLAddress    string                       `json:"sql_address"`
	ProxyID       string                       `json:"proxy_id"`
	ProxyReady    bool                         `json:"proxy_ready"`
	Deployment    *protocol.DatabaseDeployment `json:"deployment,omitempty"`
	Migration     *sharedMigration             `json:"migration,omitempty"`
}

func (n *Node) sharedIdentity() (*age.X25519Identity, error) {
	state := n.sharedState()
	if state == nil {
		return nil, sharedsql.ErrUnavailable
	}
	return age.ParseX25519Identity(state.DataKey)
}

type SharedStatus struct {
	Pending       int             `json:"pending"`
	Binding       *SharedBinding  `json:"binding,omitempty"`
	Phase         string          `json:"phase"`
	State         string          `json:"state"`
	ProxyID       string          `json:"proxy_id,omitempty"`
	ProxyReady    bool            `json:"proxy_ready"`
	SQLAddress    string          `json:"sql_address,omitempty"`
	PolicyVersion string          `json:"policy_version,omitempty"`
	Error         *protocol.Error `json:"error,omitempty"`
}

func bindingBytes(b SharedBinding) []byte { b.Signature = nil; data, _ := json.Marshal(b); return data }
func (n *Node) sharedState() *SharedLocal {
	n.sharedMu.Lock()
	defer n.sharedMu.Unlock()
	if n.sharedLocal == nil {
		return nil
	}
	copy := *n.sharedLocal
	return &copy
}
func (n *Node) sharedEnabled() bool { return n.sharedState() != nil }
func (n *Node) sharedActive() bool  { s := n.sharedState(); return s != nil && s.Phase == "active" }
func (n *Node) waitingSharedRecovery(id string) bool {
	n.sharedMu.Lock()
	defer n.sharedMu.Unlock()
	return n.sharedLocal != nil && n.sharedLocal.Phase == "active" && n.sharedStartupPending[id]
}
func (n *Node) loadSharedLocal(ctx context.Context) error {
	var body string
	err := n.DB.QueryRowContext(ctx, "SELECT secret FROM shared_local WHERE id=1").Scan(&body)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var secret Secret
	if err = json.Unmarshal([]byte(body), &secret); err != nil {
		return err
	}
	plain, err := unseal(secret, n.age)
	if err != nil {
		return err
	}
	var state SharedLocal
	if err = json.Unmarshal(plain, &state); err != nil {
		return err
	}
	n.sharedLocal = &state
	return nil
}
func (n *Node) saveSharedLocal(ctx context.Context, state SharedLocal) error {
	old := n.sharedState()
	if old != nil && (old.Binding.ClusterID != state.Binding.ClusterID || old.Binding.Database != state.Binding.Database || old.Binding.GaleraID != state.Binding.GaleraID) {
		return fault("conflict", "节点已经绑定其他 OPSD 数据库")
	}
	plain, _ := json.Marshal(state)
	secret, err := seal(plain, []string{n.age.Recipient().String()})
	if err != nil {
		return err
	}
	body, _ := json.Marshal(secret)
	if _, err = n.DB.ExecContext(ctx, "INSERT INTO shared_local(id,secret) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET secret=excluded.secret", string(body)); err != nil {
		return err
	}
	n.sharedMu.Lock()
	n.sharedLocal = &state
	n.sharedMu.Unlock()
	return nil
}
func (n *Node) sharedStore(ctx context.Context) (*sharedsql.Store, error) {
	if n.Revoked(ctx, n.ID) {
		return nil, fault("forbidden", "节点已撤销")
	}
	n.sharedMu.Lock()
	store := n.shared
	state := n.sharedLocal
	n.sharedMu.Unlock()
	if store != nil {
		return store, nil
	}
	if state == nil || state.Access.Username == "" || state.SQLAddress == "" {
		return nil, sharedsql.ErrUnavailable
	}
	opened, err := sharedsql.Open(ctx, state.SQLAddress, state.Access.Username, state.Access.Password, state.Binding.Database, n.cluster)
	if err != nil {
		return nil, err
	}
	n.sharedMu.Lock()
	defer n.sharedMu.Unlock()
	if n.shared != nil {
		opened.DB.Close()
		return n.shared, nil
	}
	n.shared = opened
	return opened, nil
}
func (n *Node) SharedStatus(ctx context.Context) SharedStatus {
	s := n.sharedState()
	if s == nil {
		return SharedStatus{Phase: "unbound", State: "unbound"}
	}
	out := SharedStatus{Binding: &s.Binding, Phase: s.Phase, State: "pending", ProxyID: s.ProxyID, SQLAddress: s.SQLAddress}
	_ = n.DB.QueryRowContext(ctx, "SELECT count(*) FROM records WHERE uploaded=0").Scan(&out.Pending)
	out.ProxyReady = s.ProxyReady
	if s.Cluster.ProxyRouting != nil {
		out.PolicyVersion = routingVersion(*s.Cluster.ProxyRouting)
	}
	n.sharedMu.Lock()
	issue := n.sharedIssue
	n.sharedMu.Unlock()
	if issue != "" {
		out.State = "error"
		out.Error = &protocol.Error{Code: "dependency_unavailable", Message: issue}
	} else if s.Phase == "active" && s.ProxyReady {
		out.State = "ready"
	}
	return out
}
func (n *Node) sharedLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 25*time.Second)
		err := n.reconcileShared(ctx)
		if n.sharedActive() {
			if store, e := n.sharedStore(ctx); e != nil {
				if err == nil {
					err = e
				}
			} else {
				if e := n.reconcileSharedFlows(ctx, store); err == nil {
					err = e
				}
			}
		}
		cancel()
		n.sharedMu.Lock()
		n.sharedIssue = ""
		if err != nil {
			n.sharedIssue = err.Error()
		}
		n.sharedMu.Unlock()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (n *Node) reconcileShared(ctx context.Context) error {
	n.sharedWorkMu.Lock()
	defer n.sharedWorkMu.Unlock()
	s := n.sharedState()
	if s == nil {
		return nil
	}
	if n.Revoked(ctx, n.ID) {
		n.sharedMu.Lock()
		if n.shared != nil {
			n.shared.DB.Close()
			n.shared = nil
		}
		n.sharedMu.Unlock()
		if s.ProxyID != "" {
			if err := n.reconcileSharedRevocations(ctx, s); err != nil {
				return err
			}
		}
		return fault("forbidden", "节点已撤销")
	}
	if s.Access.Username == "" {
		dir, err := n.Directory(ctx)
		if err != nil {
			return err
		}
		for _, m := range dir.Members {
			if m.ID == n.ID || !n.Active(ctx, m.ID) {
				continue
			}
			var secret Secret
			if err = n.Call(ctx, m.ID, "shared.bootstrap", nil, &secret); err == nil {
				plain, e := unseal(secret, n.age)
				if e != nil {
					return e
				}
				var value SharedLocal
				if e = json.Unmarshal(plain, &value); e != nil {
					return e
				}
				if err = n.saveSharedLocal(ctx, value); err != nil {
					return err
				}
				s = &value
				break
			}
		}
		if s.Access.Username == "" {
			return fault("dependency_unavailable", "OPSD 数据访问资料尚未准备完成")
		}
	}
	if s.ProxyID != "" {
		ready, err := n.recoverSharedProxy(ctx, s)
		if err != nil || !ready {
			return err
		}
	}
	ready, err := n.ensureSharedProxy(ctx, s)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	store, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	phase, err := store.Phase(ctx)
	if err != nil {
		return err
	}
	if !s.ProxyReady {
		s.ProxyReady = true
		if err = n.saveSharedLocal(ctx, *s); err != nil {
			return err
		}
	}
	if phase != "active" {
		return nil
	}
	if err = n.syncSharedDirectory(ctx, store); err != nil {
		return err
	}
	if err = n.reconcileSharedRevocations(ctx, s); err != nil {
		return err
	}
	if err = n.registerSharedProxy(ctx, store, s); err != nil {
		return err
	}
	if current, e := n.DatabaseCluster(ctx, s.Binding.ClusterID); e == nil && current.Revision != s.Cluster.Revision {
		s.Cluster = current
		if e = n.saveSharedLocal(ctx, *s); e != nil {
			return e
		}
	}
	if s.Phase != "active" {
		s.Phase = "active"
		if err = n.saveSharedLocal(ctx, *s); err != nil {
			return err
		}
	}
	if err = n.checkSharedRecovery(ctx, store); err != nil {
		return err
	}
	n.sharedMu.Lock()
	n.sharedStartupPending = nil
	n.sharedRecovered = true
	n.sharedMu.Unlock()
	if err = n.syncSharedCandidate(ctx, store); err != nil {
		return err
	}
	if consoles := n.SharedConsoles(ctx); !consoles.Cached && consoles != s.Consoles {
		s.Consoles = consoles
		if err = n.saveSharedLocal(ctx, *s); err != nil {
			return err
		}
	}
	if err = n.publishSharedMember(ctx, store); err != nil {
		return err
	}
	return n.publishSharedAddress(ctx, store)
}
func stepID(node, key string) string {
	sum := sha256.Sum256([]byte(key))
	return node + "_" + hex.EncodeToString(sum[:])
}
func (n *Node) ensureDatabaseStep(ctx context.Context, ref DatabaseRef, id string, action protocol.DatabaseAction) (bool, error) {
	var result protocol.OperationQueryResult
	request := DatabaseRPC{ResourceID: n.sharedState().Binding.ClusterID, InstanceID: ref.InstanceID, InstanceRevision: ref.InstanceRevision, OperationID: id, Action: action}
	call := func(method string, out any) error {
		if ref.NodeID == n.ID && action.Shared != nil {
			body, _ := json.Marshal(request)
			value, err := n.databaseDispatch(ctx, "system:"+n.ID, method, body)
			if err != nil {
				return err
			}
			data, _ := json.Marshal(value)
			return json.Unmarshal(data, out)
		}
		return n.Call(ctx, ref.NodeID, method, request, out)
	}
	if err := call("database.query", &result); err != nil {
		return false, err
	}
	if !result.Found {
		method := "database.execute"
		if action.Deployment != nil || action.SeedUpdate != nil {
			method = "database.deployment.execute"
		}
		var reply protocol.OperationUpdated
		if err := call(method, &reply); err != nil {
			return false, err
		}
		return false, nil
	}
	if result.Record == nil {
		return false, fmt.Errorf("数据库步骤应答缺少记录")
	}
	if result.Record.State == protocol.Succeeded {
		return true, nil
	}
	if operations.Terminal(result.Record.State) || result.Record.State == protocol.AttentionRequired {
		message := "数据库步骤需要处理"
		if result.Record.Error != nil {
			message = result.Record.Error.Message
		}
		return false, fmt.Errorf("%s：%s", id, message)
	}
	return false, nil
}
func (n *Node) ensureSharedProxy(ctx context.Context, s *SharedLocal) (bool, error) {
	if s.ProxyID == "" {
		for _, ref := range s.Cluster.Proxies {
			if ref.NodeID == n.ID {
				c, err := n.databaseConnection(ctx, ref.InstanceID, ref.InstanceRevision)
				if err != nil {
					return false, err
				}
				observed, err := (databaseengine.Manager{}).ReadProxyRouting(ctx, c, nil)
				if err != nil {
					return false, err
				}
				if len(observed.Servers) == 0 {
					return false, fmt.Errorf("已有代理缺少已确认数据库后端")
				}
				if observed.PendingChanges || observed.ClusterSync || s.Cluster.ProxyRouting == nil || observed.Hostgroups != s.Cluster.ProxyRouting.Hostgroups || !slices.ContainsFunc(observed.Mappings, func(m protocol.ProxyRouteMapping) bool {
					return m.Hostgroups == observed.Hostgroups && m.MaxWriters == 1 && m.WriterIsAlsoReader == 0
				}) {
					return false, fmt.Errorf("已有代理的 Galera 路由需要先核对并应用")
				}
				for _, server := range observed.Servers {
					if observed.Hostgroups.Contains(server.Hostgroup) && !slices.ContainsFunc(s.Cluster.ProxyRouting.Backends, func(b protocol.ProxyBackend) bool { return b.Host == server.Host && b.Port == server.Port }) {
						return false, fmt.Errorf("已有代理包含未登记后端，请先核对")
					}
				}
				s.ProxyID = ref.InstanceID
				s.Admin = c
				s.SQLAddress, err = n.sharedProxyAddress(ctx, c)
				if err != nil {
					return false, err
				}
				if err = n.saveSharedLocal(ctx, *s); err != nil {
					return false, err
				}
				break
			}
		}
	}
	if s.ProxyID == "" {
		if s.Deployment == nil {
			id := "opsd_sql_" + n.ID
			d := protocol.DatabaseDeployment{ID: id, Kind: "proxysql", Image: databaseengine.ProxyImage, Directory: "/var/lib/opsd/databases/" + id, Address: "127.0.0.1", SQLPort: 6033, AdminPort: 6032, InstallDocker: true, Connection: protocol.DatabaseConnection{Kind: "proxysql", Address: "127.0.0.1:6032", Username: "opsdadmin", Password: identity.Random(), ProtectedUsers: []string{"opsdadmin"}}, MonitorUser: s.Monitor.Username, MonitorPassword: s.Monitor.Password, BackendMaxConnections: 2}
			for _, ref := range s.Cluster.Members {
				endpoint, err := sharedBackendEndpoint(s.Cluster, ref)
				if err != nil {
					return false, err
				}
				d.Backends = append(d.Backends, endpoint)
			}
			if err := d.Validate(); err != nil {
				return false, err
			}
			s.Deployment = &d
			if err := n.saveSharedLocal(ctx, *s); err != nil {
				return false, err
			}
		}
		d := s.Deployment
		ref := DatabaseRef{NodeID: n.ID, InstanceID: "deployment_" + d.ID}
		id := stepID(n.ID, "shared-proxy-deploy/"+d.ID)
		ready, err := n.ensureDatabaseStep(ctx, ref, id, protocol.DatabaseAction{Kind: "deploy_database", Deployment: d})
		if err != nil || !ready {
			return false, err
		}
		item, err := n.registerDatabaseDeployment(ctx, DatabaseRPC{InstanceID: ref.InstanceID, OperationID: id, Action: protocol.DatabaseAction{Deployment: d}})
		if err != nil {
			return false, err
		}
		s.ProxyID = item.Instance.ID
		s.Admin, err = n.databaseConnection(ctx, item.Instance.ID)
		if err != nil {
			return false, err
		}
		s.SQLAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(d.SQLPort))
		if err = n.saveSharedLocal(ctx, *s); err != nil {
			return false, err
		}
	}
	hostgroup := 10
	if s.Cluster.ProxyRouting != nil {
		hostgroup = s.Cluster.ProxyRouting.Hostgroups.Writer
	}
	a := protocol.DatabaseAction{Kind: "shared_proxy", Connection: s.Admin, ClusterID: s.Binding.GaleraID, Hostgroup: hostgroup, Shared: &protocol.SharedDatabaseAction{Mode: "provision", Cluster: n.cluster, Database: s.Binding.Database, NodeID: n.ID, PasswordHash: databaseengine.NativePassword(s.Access.Password), MonitorHash: databaseengine.NativePassword(s.Monitor.Password)}}
	instance, err := n.databaseInstance(ctx, s.ProxyID)
	if err != nil {
		return false, err
	}
	ref := DatabaseRef{NodeID: n.ID, InstanceID: s.ProxyID, InstanceRevision: instance.Revision}
	return n.ensureDatabaseStep(ctx, ref, stepID(n.ID, "shared-proxy-account/"+s.Binding.ClusterID+"/"+n.ID+"/"+strconv.Itoa(hostgroup)), a)
}

func (n *Node) acceptSharedBinding(ctx context.Context, b SharedBinding) error {
	issuer, err := n.Member(ctx, b.Issuer)
	if err != nil {
		return err
	}
	key, err := identity.Decode(issuer.Signing)
	if err != nil || b.Revision == 0 || !ed25519.Verify(key, bindingBytes(b), b.Signature) {
		return fault("forbidden", "OPSD 数据库绑定签名无效")
	}
	if n.sharedEnabled() {
		return nil
	}
	return n.saveSharedLocal(ctx, SharedLocal{Binding: b, Phase: "joining"})
}
func sharedProtected(name string) bool {
	return strings.HasPrefix(name, "opsd_node_") || strings.HasPrefix(name, "opsd_mon_")
}
