package databaseengine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type proxyServerLayers struct {
	Memory  map[string][]map[string]string
	Runtime map[string][]map[string]string
	Disk    map[string][]map[string]string
}

// serverLayers 读取 LOAD/SAVE MYSQL SERVERS 覆盖的配置，连同未接管主机组核对。
func serverLayers(ctx context.Context, s *session) (proxyServerLayers, error) {
	out := proxyServerLayers{Memory: map[string][]map[string]string{}, Runtime: map[string][]map[string]string{}, Disk: map[string][]map[string]string{}}
	tables, err := rows(ctx, s, "SELECT name FROM main.sqlite_master WHERE type='table'")
	if err != nil {
		return out, err
	}
	available := map[string]bool{}
	for _, table := range tables {
		available[table["name"]] = true
	}
	for _, table := range []string{"mysql_servers", "mysql_servers_ssl_params", "mysql_galera_hostgroups", "mysql_replication_hostgroups", "mysql_group_replication_hostgroups", "mysql_aws_aurora_hostgroups", "mysql_hostgroup_attributes"} {
		if !available[table] || !available["runtime_"+table] {
			continue
		}
		for i, prefix := range []string{"", "runtime_", "disk."} {
			value, err := rows(ctx, s, "SELECT * FROM "+prefix+table)
			if err != nil {
				return out, err
			}
			sortRows(value)
			switch i {
			case 0:
				out.Memory[table] = value
			case 1:
				out.Runtime[table] = value
			case 2:
				out.Disk[table] = value
			}
		}
	}
	if _, ok := out.Memory["mysql_servers"]; !ok {
		return out, fmt.Errorf("ProxySQL 后端配置表不可用")
	}
	if _, ok := out.Memory["mysql_galera_hostgroups"]; !ok {
		return out, fmt.Errorf("ProxySQL 版本不支持 Galera 主机组")
	}
	return out, nil
}

func sortRows(value []map[string]string) {
	sort.Slice(value, func(i, j int) bool { return fingerprint(value[i]) < fingerprint(value[j]) })
}

func runtimeServersProjection(value []map[string]string, groups protocol.ProxyHostgroups) []map[string]string {
	unique := map[string]map[string]string{}
	for _, source := range value {
		row := maps.Clone(source)
		group := int(number(row["hostgroup_id"]))
		if groups.Contains(group) {
			row["hostgroup_id"] = strconv.Itoa(groups.Writer)
		}
		if row["status"] == "SHUNNED" {
			row["status"] = "ONLINE"
		}
		key := row["hostgroup_id"] + "/" + row["hostname"] + "/" + row["port"]
		if previous, exists := unique[key]; exists && fingerprint(previous) != fingerprint(row) {
			// 同一后端的不同主机组配置有差异，保留它以阻止误报一致。
			key += "/" + fingerprint(row)
		}
		unique[key] = row
	}
	out := []map[string]string{}
	for _, row := range unique {
		out = append(out, row)
	}
	sortRows(out)
	return out
}

func serverLayersInSync(layers proxyServerLayers, groups protocol.ProxyHostgroups) bool {
	if fingerprint(layers.Memory) != fingerprint(layers.Disk) {
		return false
	}
	for table, memory := range layers.Memory {
		runtime := layers.Runtime[table]
		if table == "mysql_servers" {
			memory, runtime = runtimeServersProjection(memory, groups), runtimeServersProjection(runtime, groups)
		}
		if fingerprint(memory) != fingerprint(runtime) {
			return false
		}
	}
	return true
}

func unmanagedServersFingerprint(memory map[string][]map[string]string, groups protocol.ProxyHostgroups) string {
	out := map[string][]map[string]string{}
	for table, rows := range memory {
		out[table] = []map[string]string{}
		for _, row := range rows {
			if table == "mysql_servers" && groups.Contains(int(number(row["hostgroup_id"]))) || table == "mysql_galera_hostgroups" && int(number(row["writer_hostgroup"])) == groups.Writer {
				continue
			}
			out[table] = append(out[table], row)
		}
	}
	return fingerprint(out)
}

func proxyRouteServers(value []map[string]string) []protocol.ProxyRouteServer {
	out := []protocol.ProxyRouteServer{}
	for _, row := range value {
		out = append(out, protocol.ProxyRouteServer{ProxyEndpoint: protocol.ProxyEndpoint{Host: row["hostname"], Port: int(number(row["port"]))}, Hostgroup: int(number(row["hostgroup_id"])), State: row["status"], Weight: int(number(row["weight"])), MaxConnections: int(number(row["max_connections"])), UseSSL: row["use_ssl"] == "1"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hostgroup != out[j].Hostgroup {
			return out[i].Hostgroup < out[j].Hostgroup
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

func (Manager) ReadProxyRouting(ctx context.Context, connection protocol.DatabaseConnection, selected *protocol.ProxyHostgroups) (protocol.ProxyRoutingObservation, error) {
	out := protocol.ProxyRoutingObservation{Servers: []protocol.ProxyRouteServer{}, RuntimeServers: []protocol.ProxyRouteServer{}, Mappings: []protocol.ProxyRouteMapping{}, Samples: []protocol.ProxyPingSample{}, Accounts: []string{}, CheckedAt: time.Now().UTC()}
	if connection.Kind != "proxysql" {
		return out, fmt.Errorf("需要 ProxySQL 实例")
	}
	s, err := connect(ctx, connection)
	if err != nil {
		return out, err
	}
	defer s.Close()
	layers, err := serverLayers(ctx, s)
	if err != nil {
		return out, err
	}
	for _, row := range layers.Memory["mysql_galera_hostgroups"] {
		groups := protocol.ProxyHostgroups{Writer: int(number(row["writer_hostgroup"])), BackupWriter: int(number(row["backup_writer_hostgroup"])), Reader: int(number(row["reader_hostgroup"])), Offline: int(number(row["offline_hostgroup"]))}
		out.Mappings = append(out.Mappings, protocol.ProxyRouteMapping{Hostgroups: groups, MaxWriters: int(number(row["max_writers"])), WriterIsAlsoReader: int(number(row["writer_is_also_reader"])), MaxTransactionsBehind: int(number(row["max_transactions_behind"]))})
	}
	out.Hostgroups = protocol.ProxyHostgroups{Writer: 10, BackupWriter: 20, Reader: 30, Offline: 40}
	if selected != nil {
		out.Hostgroups = *selected
	} else if len(out.Mappings) == 1 {
		out.Hostgroups = out.Mappings[0].Hostgroups
	}
	if err = out.Hostgroups.Validate(); err != nil {
		return out, err
	}
	out.Servers, out.RuntimeServers = proxyRouteServers(layers.Memory["mysql_servers"]), proxyRouteServers(layers.Runtime["mysql_servers"])
	out.PendingChanges = !serverLayersInSync(layers, out.Hostgroups)
	out.Fingerprint, out.UnmanagedFingerprint = fingerprint(layers.Memory), unmanagedServersFingerprint(layers.Memory, out.Hostgroups)
	peers, err := rows(ctx, s, "SELECT count(*) AS count FROM proxysql_servers")
	if err != nil {
		return out, err
	}
	sync, err := rows(ctx, s, "SELECT variable_value FROM global_variables WHERE variable_name='admin-cluster_mysql_servers_diffs_before_sync'")
	if err != nil {
		return out, err
	}
	out.ClusterSync = len(peers) > 0 && number(peers[0]["count"]) > 0 && (len(sync) == 0 || sync[0]["variable_value"] != "0")
	users, err := rows(ctx, s, "SELECT username,default_hostgroup FROM mysql_users ORDER BY username")
	if err != nil {
		return out, err
	}
	for _, user := range users {
		group := int(number(user["default_hostgroup"]))
		if out.Hostgroups.Contains(group) && !protocol.SystemDatabaseUser(user["username"], connection) {
			out.Accounts = append(out.Accounts, user["username"])
			out.SplitRouting = out.SplitRouting || group != out.Hostgroups.Writer
		}
	}
	queryRules, err := rows(ctx, s, "SELECT username,destination_hostgroup FROM mysql_query_rules WHERE active=1 AND destination_hostgroup IS NOT NULL")
	if err != nil {
		return out, err
	}
	for _, rule := range queryRules {
		group := int(number(rule["destination_hostgroup"]))
		out.SplitRouting = out.SplitRouting || group != out.Hostgroups.Writer && (rule["username"] == "" || slices.Contains(out.Accounts, rule["username"]))
	}
	samples, err := rows(ctx, s, "SELECT hostname,port,time_start_us,ping_success_time_us FROM monitor.mysql_server_ping_log WHERE ping_error IS NULL AND ping_success_time_us>0 AND time_start_us>=? ORDER BY time_start_us DESC LIMIT 10000", time.Now().Add(-120*time.Second).UnixMicro())
	if err != nil {
		return out, err
	}
	for _, sample := range samples {
		at, _ := strconv.ParseInt(sample["time_start_us"], 10, 64)
		latency, _ := strconv.ParseInt(sample["ping_success_time_us"], 10, 64)
		out.Samples = append(out.Samples, protocol.ProxyPingSample{ProxyEndpoint: protocol.ProxyEndpoint{Host: sample["hostname"], Port: int(number(sample["port"]))}, At: time.UnixMicro(at).UTC(), LatencyUS: latency})
	}
	return out, nil
}

func validateRouteLayers(layers proxyServerLayers, action protocol.ProxyRouteAction) error {
	if unmanagedServersFingerprint(layers.Memory, action.Hostgroups) != action.UnmanagedFingerprint {
		return fmt.Errorf("ProxySQL 其他配置已变化，请重新预览")
	}
	for _, row := range layers.Memory["mysql_galera_hostgroups"] {
		if int(number(row["writer_hostgroup"])) == action.Hostgroups.Writer {
			continue
		}
		for _, field := range []string{"writer_hostgroup", "backup_writer_hostgroup", "reader_hostgroup", "offline_hostgroup"} {
			if action.Hostgroups.Contains(int(number(row[field]))) {
				return fmt.Errorf("主机组已用于其他 Galera 配置")
			}
		}
	}
	for _, table := range []string{"mysql_replication_hostgroups", "mysql_group_replication_hostgroups", "mysql_aws_aurora_hostgroups"} {
		for _, row := range layers.Memory[table] {
			for _, field := range []string{"writer_hostgroup", "backup_writer_hostgroup", "reader_hostgroup", "offline_hostgroup"} {
				if value, exists := row[field]; exists && action.Hostgroups.Contains(int(number(value))) {
					return fmt.Errorf("主机组已用于其他复制配置")
				}
			}
		}
	}
	return nil
}

func routeMemorySatisfied(memory map[string][]map[string]string, action protocol.ProxyRouteAction) bool {
	mapping := false
	for _, row := range memory["mysql_galera_hostgroups"] {
		if int(number(row["writer_hostgroup"])) == action.Hostgroups.Writer {
			mapping = row["active"] == "1" && int(number(row["backup_writer_hostgroup"])) == action.Hostgroups.BackupWriter && int(number(row["reader_hostgroup"])) == action.Hostgroups.Reader && int(number(row["offline_hostgroup"])) == action.Hostgroups.Offline && row["max_writers"] == "1" && row["writer_is_also_reader"] == "0" && int(number(row["max_transactions_behind"])) == action.MaxTransactionsBehind
		}
	}
	if !mapping {
		return false
	}
	for _, target := range action.Servers {
		found := false
		for _, row := range memory["mysql_servers"] {
			if !action.Hostgroups.Contains(int(number(row["hostgroup_id"]))) || row["hostname"] != target.Host || int(number(row["port"])) != target.Port {
				continue
			}
			if int(number(row["weight"])) != target.Weight || int(number(row["max_connections"])) != target.MaxConnections || (row["use_ssl"] == "1") != target.UseSSL || row["status"] != "ONLINE" {
				return false
			}
			found = found || int(number(row["hostgroup_id"])) == action.Hostgroups.Writer
		}
		if !found {
			return false
		}
	}
	for _, endpoint := range action.Remove {
		for _, row := range memory["mysql_servers"] {
			if action.Hostgroups.Contains(int(number(row["hostgroup_id"]))) && row["hostname"] == endpoint.Host && int(number(row["port"])) == endpoint.Port {
				return false
			}
		}
	}
	return true
}

func prepareProxyRoute(ctx context.Context, s *session, action protocol.ProxyRouteAction) ([]string, error) {
	layers, err := serverLayers(ctx, s)
	if err != nil {
		return nil, err
	}
	if err = validateRouteLayers(layers, action); err != nil {
		return nil, err
	}
	if !serverLayersInSync(layers, action.Hostgroups) {
		return nil, fmt.Errorf("ProxySQL 存在其他待应用的后端配置")
	}
	if fingerprint(layers.Memory) != action.ExpectedFingerprint && !routeMemorySatisfied(layers.Memory, action) {
		return nil, fmt.Errorf("代理配置已变化，请重新预览")
	}
	return []string{"memory", "runtime", "disk"}, nil
}

func observeProxyRoute(ctx context.Context, s *session, action protocol.ProxyRouteAction, step string) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC()}
	layers, err := serverLayers(ctx, s)
	if err != nil {
		return out, err
	}
	if err = validateRouteLayers(layers, action); err != nil {
		return out, err
	}
	out.Source, out.Fingerprint = fingerprint(layers.Memory), fingerprint(layers)
	switch step {
	case "memory":
		out.Satisfied = routeMemorySatisfied(layers.Memory, action)
	case "runtime":
		copy := layers
		copy.Disk = copy.Memory
		out.Satisfied = routeMemorySatisfied(layers.Memory, action) && serverLayersInSync(copy, action.Hostgroups)
	case "disk":
		out.Satisfied = routeMemorySatisfied(layers.Memory, action) && fingerprint(layers.Memory) == fingerprint(layers.Disk)
	}
	return out, nil
}

func applyProxyRoute(ctx context.Context, s *session, action protocol.ProxyRouteAction, step string) error {
	layers, err := serverLayers(ctx, s)
	if err != nil {
		return err
	}
	if err = validateRouteLayers(layers, action); err != nil {
		return err
	}
	if step == "runtime" || step == "disk" {
		if !routeMemorySatisfied(layers.Memory, action) {
			return fmt.Errorf("代理目标配置出现外部修改")
		}
		query := "LOAD MYSQL SERVERS TO RUNTIME"
		if step == "disk" {
			query = "SAVE MYSQL SERVERS TO DISK"
		}
		_, err = s.ExecContext(ctx, query)
		return clean(err)
	}
	if step != "memory" {
		return fmt.Errorf("代理路由步骤无效")
	}
	if routeMemorySatisfied(layers.Memory, action) {
		return nil
	}
	if fingerprint(layers.Memory) != action.ExpectedFingerprint {
		return fmt.Errorf("代理配置已变化，请重新预览")
	}
	groups := fmt.Sprintf("(%d,%d,%d,%d)", action.Hostgroups.Writer, action.Hostgroups.BackupWriter, action.Hostgroups.Reader, action.Hostgroups.Offline)
	for _, endpoint := range action.Remove {
		used, err := rows(ctx, s, "SELECT COALESCE(SUM(ConnUsed),0) AS used FROM stats_mysql_connection_pool WHERE srv_host=? AND srv_port=? AND hostgroup IN "+groups, endpoint.Host, endpoint.Port)
		if err != nil {
			return err
		}
		if len(used) > 0 && number(used[0]["used"]) > 0 {
			return fmt.Errorf("退出后端仍有活动连接，请等待完成后重新预览")
		}
	}
	for _, endpoint := range action.Remove {
		if _, err = s.ExecContext(ctx, "DELETE FROM mysql_servers WHERE hostname=? AND port=? AND hostgroup_id IN "+groups, endpoint.Host, endpoint.Port); err != nil {
			return clean(err)
		}
	}
	for _, target := range action.Servers {
		if _, err = s.ExecContext(ctx, "UPDATE mysql_servers SET weight=?,max_connections=?,use_ssl=?,status='ONLINE' WHERE hostname=? AND port=? AND hostgroup_id IN "+groups, target.Weight, target.MaxConnections, target.UseSSL, target.Host, target.Port); err != nil {
			return clean(err)
		}
		writer := slices.ContainsFunc(layers.Memory["mysql_servers"], func(row map[string]string) bool {
			return row["hostname"] == target.Host && int(number(row["port"])) == target.Port && int(number(row["hostgroup_id"])) == action.Hostgroups.Writer
		})
		if !writer {
			if _, err = s.ExecContext(ctx, "INSERT INTO mysql_servers(hostgroup_id,hostname,port,status,weight,max_connections,use_ssl) VALUES(?,?,?,'ONLINE',?,?,?)", action.Hostgroups.Writer, target.Host, target.Port, target.Weight, target.MaxConnections, target.UseSSL); err != nil {
				return clean(err)
			}
		}
	}
	exists := slices.ContainsFunc(layers.Memory["mysql_galera_hostgroups"], func(row map[string]string) bool {
		return int(number(row["writer_hostgroup"])) == action.Hostgroups.Writer
	})
	if exists {
		_, err = s.ExecContext(ctx, "UPDATE mysql_galera_hostgroups SET backup_writer_hostgroup=?,reader_hostgroup=?,offline_hostgroup=?,active=1,max_writers=1,writer_is_also_reader=0,max_transactions_behind=? WHERE writer_hostgroup=?", action.Hostgroups.BackupWriter, action.Hostgroups.Reader, action.Hostgroups.Offline, action.MaxTransactionsBehind, action.Hostgroups.Writer)
	} else {
		_, err = s.ExecContext(ctx, "INSERT INTO mysql_galera_hostgroups(writer_hostgroup,backup_writer_hostgroup,reader_hostgroup,offline_hostgroup,active,max_writers,writer_is_also_reader,max_transactions_behind) VALUES(?,?,?,?,1,1,0,?)", action.Hostgroups.Writer, action.Hostgroups.BackupWriter, action.Hostgroups.Reader, action.Hostgroups.Offline, action.MaxTransactionsBehind)
	}
	return clean(err)
}

func ProxyRouteActionSatisfied(observation protocol.ProxyRoutingObservation, action protocol.ProxyRouteAction) bool {
	if observation.PendingChanges || observation.ClusterSync || observation.UnmanagedFingerprint != action.UnmanagedFingerprint {
		return false
	}
	// 公开观测用于调度判断；实际写入和恢复继续在适配器中核对完整配置。
	for _, desired := range action.Servers {
		found := false
		for _, actual := range observation.Servers {
			if actual.Hostgroup == action.Hostgroups.Writer && actual.ProxyEndpoint == desired.ProxyEndpoint {
				found = actual.Weight == desired.Weight && actual.MaxConnections == desired.MaxConnections && actual.UseSSL == desired.UseSSL && actual.State == "ONLINE"
			}
		}
		if !found {
			return false
		}
	}
	for _, removed := range action.Remove {
		if slices.ContainsFunc(observation.Servers, func(server protocol.ProxyRouteServer) bool {
			return action.Hostgroups.Contains(server.Hostgroup) && server.ProxyEndpoint == removed
		}) {
			return false
		}
	}
	for _, mapping := range observation.Mappings {
		if mapping.Hostgroups == action.Hostgroups && mapping.MaxWriters == 1 && mapping.WriterIsAlsoReader == 0 && mapping.MaxTransactionsBehind == action.MaxTransactionsBehind {
			return true
		}
	}
	return false
}
