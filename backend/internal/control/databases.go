package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

func (s *Server) discoveredInstance(ctx context.Context, node string, item cluster.DatabaseInstance) httpapi.DiscoveredDatabaseInstance {
	out := convertHTTP[httpapi.DiscoveredDatabaseInstance](item)
	var result protocol.DatabaseReadResult
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e := s.node.Call(readCtx, node, "database.read", cluster.DatabaseRPC{InstanceID: item.ID}, &result)
	state := convertHTTP[httpapi.DatabaseStatus](result.State)
	if e != nil {
		state = httpapi.DatabaseStatus{State: "unavailable", CheckedAt: s.now().UTC(), Error: &httpapi.NetworkError{Code: "dependency_unavailable", Message: "数据库连接失败，请检查连接信息"}}
	}
	out.Status = &state
	return out
}
func (s *Server) DiscoverDatabases(w http.ResponseWriter, r *http.Request, id string, _ httpapi.DiscoverDatabasesParams) {
	items := []cluster.DatabaseInstance{}
	e := s.node.Call(r.Context(), id, "database.discover", cluster.DatabaseRPC{}, &items)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	out := httpapi.DatabaseDiscoveryList{Items: make([]httpapi.DiscoveredDatabaseInstance, len(items))}
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func() { defer wg.Done(); out.Items[i] = s.discoveredInstance(r.Context(), id, item) }()
	}
	wg.Wait()
	write(w, 200, out)
}
func (s *Server) SaveDatabaseInstance(w http.ResponseWriter, r *http.Request, id, instanceID string, _ httpapi.SaveDatabaseInstanceParams) {
	var input httpapi.DatabaseInstanceInput
	if !s.body(w, r, &input) {
		return
	}
	domain := convertHTTP[cluster.DatabaseInstanceInput](input)
	domain.ID = instanceID
	domain.ExpectedRevision = apiRequestOf(r.Context()).expected
	domain.CreateOnly = apiRequestOf(r.Context()).create
	var item cluster.DatabaseInstance
	e := s.node.Call(r.Context(), id, "database.instance.save", cluster.DatabaseRPC{Instance: domain}, &item)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	s.setETag(w, r, item.Revision)
	status := 200
	if domain.CreateOnly {
		status = 201
		w.Header().Set("Location", httpapi.APIBasePath+"/nodes/"+id+"/database-instances/"+item.ID)
	}
	write(w, status, s.discoveredInstance(r.Context(), id, item))
}
func (s *Server) databaseDefinitions(ctx context.Context) ([]cluster.ManagedDatabaseCluster, error) {
	all, e := s.node.DatabaseClusters(ctx)
	if e != nil {
		return nil, e
	}
	out := []cluster.ManagedDatabaseCluster{}
	for _, c := range all {
		if c.Deleted || !s.node.Active(ctx, c.Coordinator) {
			continue
		}
		if a := access(ctx); a != nil && !a.Token.Allows("databases", "read", c.ID, "") {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
func (s *Server) databaseStatuses(ctx context.Context) ([]cluster.ManagedDatabaseStatus, error) {
	definitions, e := s.databaseDefinitions(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]cluster.ManagedDatabaseStatus, len(definitions))
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, c := range definitions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
			}
			out[i], _ = s.node.ManagedDatabaseStatus(ctx, c)
		}()
	}
	wg.Wait()
	return out, nil
}
func (s *Server) ListDatabaseClusters(w http.ResponseWriter, r *http.Request, _ httpapi.ListDatabaseClustersParams) {
	items, e := s.databaseStatuses(r.Context())
	s.resultError(w, r, 200, httpapi.ManagedDatabaseList{Items: convertHTTP[[]httpapi.ManagedDatabaseStatus](items)}, e)
}
func (s *Server) GetDatabaseCluster(w http.ResponseWriter, r *http.Request, id string, params httpapi.GetDatabaseClusterParams) {
	c, e := s.node.DatabaseCluster(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	scope := "health"
	if params.View != nil {
		scope = string(*params.View)
	}
	if scope != "health" && scope != "schemas" && scope != "users" && scope != "proxies" {
		s.fail(w, r, 422, "invalid_request", "读取分类无效")
		return
	}
	out, e := s.node.ManagedDatabaseStatus(r.Context(), c, scope)
	s.resultError(w, r, 200, convertHTTP[httpapi.ManagedDatabaseStatus](out), e)
}
func (s *Server) saveDatabaseDefinition(w http.ResponseWriter, r *http.Request, id string, create bool) {
	var input httpapi.DatabaseClusterInput
	if create {
		if !s.body(w, r, &input) {
			return
		}
	} else {
		old, e := s.node.DatabaseCluster(r.Context(), id)
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		if !s.mergeBody(w, r, convertHTTP[httpapi.DatabaseClusterInput](old), &input) {
			return
		}
	}

	c := convertHTTP[cluster.ManagedDatabaseCluster](input)
	c.Revision = expectedRevision(r)
	if create {
		key := r.Header.Get("Idempotency-Key")
		if len(key) < 1 || len(key) > 128 {
			s.fail(w, r, 422, "invalid_request", "缺少有效请求标识")
			return
		}
		sum := sha256.Sum256([]byte("database_cluster/" + actor(r) + "/" + key))
		c.ID = c.Coordinator + "_" + hex.EncodeToString(sum[:])
		if c.Revision != 0 {
			s.fail(w, r, 422, "invalid_request", "新集群版本须为零")
			return
		}
	} else {
		c.ID = id
		old, e := s.node.DatabaseCluster(r.Context(), id)
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		if old.Coordinator != c.Coordinator {
			s.fail(w, r, 409, "conflict", "协调节点已经固定")
			return
		}
	}
	var out cluster.ManagedDatabaseCluster
	if e := s.node.RequireAPIAuthorization(r.Context(), c.Coordinator); e != nil {
		s.operationError(w, r, e)
		return
	}
	var receipt *requeststate.Write
	if create {
		receipt = writeIdentity(r)
	}
	e := s.node.Call(r.Context(), c.Coordinator, "database.cluster.save", cluster.DatabaseRPC{Cluster: c, Write: receipt}, &out)
	status := 200
	if create {
		status = 201
		w.Header().Set("Location", httpapi.APIBasePath+"/database-clusters/"+out.ID)
	}
	if e == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, status, convertHTTP[httpapi.ManagedDatabaseCluster](out), e)
}
func (s *Server) CreateDatabaseCluster(w http.ResponseWriter, r *http.Request, _ httpapi.CreateDatabaseClusterParams) {
	s.saveDatabaseDefinition(w, r, "", true)
}
func (s *Server) UpdateDatabaseCluster(w http.ResponseWriter, r *http.Request, id string, _ httpapi.UpdateDatabaseClusterParams) {
	s.saveDatabaseDefinition(w, r, id, false)
}
func (s *Server) ChangeDatabase(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeDatabaseParams) {
	var input httpapi.DatabaseChange
	if !s.body(w, r, &input) {
		return
	}
	change := convertHTTP[cluster.DatabaseChange](input)
	if input.ExpectedRevision < 1 {
		s.fail(w, r, 422, "invalid_request", "数据库集群版本无效")
		return
	}
	change.Cluster = id
	op, e := s.node.SubmitDatabaseChange(r.Context(), change, actor(r), r.Header.Get("Idempotency-Key"), requestFingerprint(r))
	if e == nil {
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+op.ID)
	}
	s.resultError(w, r, 202, op, e)
}
func (s *Server) GetDatabaseBackups(w http.ResponseWriter, r *http.Request, id string) {
	c, e := s.node.DatabaseCluster(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	out := httpapi.DatabaseBackupList{CheckedAt: s.now().UTC(), Items: make([]httpapi.DatabaseNodeBackups, len(c.Members))}
	var wg sync.WaitGroup
	for i, ref := range c.Members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result protocol.DatabaseReadResult
			readCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			e := s.node.Call(readCtx, ref.NodeID, "database.read", cluster.DatabaseRPC{InstanceID: ref.InstanceID, Backups: true}, &result)
			item := httpapi.DatabaseNodeBackups{NodeId: ref.NodeID, InstanceId: ref.InstanceID, Items: []httpapi.DatabaseBackup{}}
			if e != nil {
				message := "备份状态不可用"
				item.Error = &message
			} else {
				item.Items = convertHTTP[[]httpapi.DatabaseBackup](result.Backups)
			}
			out.Items[i] = item
		}()
	}
	wg.Wait()
	write(w, 200, out)
}
func (s *Server) databaseMetrics(ctx context.Context, emit func(string, string, float64), label func(string, string) string) {
	a := access(ctx)
	if a == nil || !a.Token.Has("databases", "read") {
		return
	}
	items, e := s.databaseStatuses(ctx)
	if e != nil {
		return
	}
	for _, item := range items {
		for _, m := range item.Members {
			labels := label("cluster_id", item.Cluster.ID) + "," + label("node_id", m.NodeID) + "," + label("instance_id", m.InstanceID)
			up := 0.0
			if m.State.State == "ready" {
				up = 1
			}
			emit("opsd_database_up", labels, up)
			emit("opsd_database_checked_timestamp_seconds", labels, float64(m.State.CheckedAt.Unix()))
			if m.State.Version == "" {
				continue
			}
			emit("opsd_database_connections", labels, float64(m.State.Connections))
			emit("opsd_galera_members", labels, float64(m.State.ClusterSize))
			emit("opsd_galera_receive_queue", labels, m.State.ReceiveQueue)
			emit("opsd_galera_send_queue", labels, m.State.SendQueue)
			emit("opsd_galera_flow_control_ratio", labels, m.State.FlowControl)
		}
		for _, p := range item.Proxies {
			labels := label("cluster_id", item.Cluster.ID) + "," + label("node_id", p.NodeID) + "," + label("instance_id", p.InstanceID)
			up := 0.0
			if p.State.State == "ready" {
				up = 1
			}
			emit("opsd_proxysql_up", labels, up)
			if p.State.Version == "" {
				continue
			}
			emit("opsd_proxysql_connections", labels, float64(p.State.Connections))
			for _, pool := range p.State.Pools {
				target := labels + "," + label("server", pool.Host) + "," + label("port", strconv.Itoa(pool.Port)) + "," + label("hostgroup", strconv.Itoa(pool.Hostgroup))
				emit("opsd_proxysql_pool_used", target, float64(pool.Used))
				emit("opsd_proxysql_pool_free", target, float64(pool.Free))
			}
		}
	}
}
