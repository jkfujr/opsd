// Package databaseengine 通过本机 MySQL 协议读取和管理 MariaDB Galera 与 ProxySQL。
package databaseengine

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Manager struct{}
type Observation struct {
	Fingerprint string
	Source      string
	Satisfied   bool
	CheckedAt   time.Time
}
type session struct {
	*sql.Conn
	db *sql.DB
}

func (s *session) Close() { s.Conn.Close(); s.db.Close() }
func connect(ctx context.Context, c protocol.DatabaseConnection) (*session, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	config := mysql.NewConfig()
	config.User = c.Username
	config.Passwd = c.Password
	config.Net = "tcp"
	config.Addr = c.Address
	config.Timeout = 3 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	config.InterpolateParams = true
	config.CheckConnLiveness = true
	if c.Socket != "" {
		config.Net = "unix"
		config.Addr = c.Socket
	}
	connector, e := mysql.NewConnector(config)
	if e != nil {
		return nil, fmt.Errorf("数据库连接配置无效")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, e := db.Conn(ctx)
	if e != nil {
		db.Close()
		return nil, clean(e)
	}
	if e = conn.PingContext(ctx); e != nil {
		conn.Close()
		db.Close()
		return nil, clean(e)
	}
	return &session{conn, db}, nil
}
func clean(e error) error {
	if e == nil {
		return nil
	}
	var m *mysql.MySQLError
	if errors.As(e, &m) {
		return fmt.Errorf("数据库执行失败（%d）", m.Number)
	}
	return fmt.Errorf("数据库连接或应答失败")
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func rows(ctx context.Context, q queryer, query string, args ...any) ([]map[string]string, error) {
	r, e := q.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, clean(e)
	}
	defer r.Close()
	names, e := r.Columns()
	if e != nil {
		return nil, clean(e)
	}
	out := []map[string]string{}
	for r.Next() {
		values := make([]sql.RawBytes, len(names))
		dest := make([]any, len(names))
		for i := range values {
			dest[i] = &values[i]
		}
		if e = r.Scan(dest...); e != nil {
			return nil, clean(e)
		}
		item := map[string]string{}
		for i, name := range names {
			item[strings.ToLower(name)] = string(values[i])
		}
		out = append(out, item)
		if len(out) > 10000 {
			return nil, fmt.Errorf("数据库读取结果过大")
		}
	}
	return out, clean(r.Err())
}
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func NativePassword(password string) string {
	first := sha1.Sum([]byte(password))
	second := sha1.Sum(first[:])
	return "*" + strings.ToUpper(hex.EncodeToString(second[:]))
}
func number(v string) float64                  { n, _ := strconv.ParseFloat(v, 64); return n }
func ident(v string) string                    { return "`" + strings.ReplaceAll(v, "`", "``") + "`" }
func literal(v string) string                  { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
func account(a protocol.DatabaseAction) string { return literal(a.User) + "@" + literal(a.Host) }
func guardDatabaseUser(ctx context.Context, s *session, a protocol.DatabaseAction) error {
	if a.User == "" {
		return nil
	}
	r, e := rows(ctx, s, "SELECT PRIVILEGE_TYPE FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=? AND PRIVILEGE_TYPE<>'USAGE'", account(a))
	if e != nil {
		return e
	}
	if len(r) > 0 {
		return fmt.Errorf("系统及管理账号保持只读")
	}
	v, e := variables(ctx, s)
	if e != nil {
		return e
	}
	if strings.SplitN(v["wsrep_sst_auth"], ":", 2)[0] == a.User {
		return fmt.Errorf("SST 账号保持只读")
	}
	return nil
}
func status(ctx context.Context, s *session) (map[string]string, error) {
	values, e := rows(ctx, s, "SHOW GLOBAL STATUS WHERE Variable_name IN ('wsrep_cluster_state_uuid','wsrep_cluster_status','wsrep_local_state_comment','wsrep_ready','wsrep_connected','wsrep_cluster_size','wsrep_local_recv_queue','wsrep_local_send_queue','wsrep_flow_control_paused','wsrep_incoming_addresses','Threads_connected')")
	if e != nil {
		return nil, e
	}
	out := map[string]string{}
	for _, v := range values {
		out[strings.ToLower(v["variable_name"])] = v["value"]
	}
	return out, nil
}
func variables(ctx context.Context, s *session) (map[string]string, error) {
	values, e := rows(ctx, s, "SHOW GLOBAL VARIABLES WHERE Variable_name IN ('wsrep_cluster_name','wsrep_cluster_address','wsrep_node_address','read_only','wsrep_sst_auth','version')")
	if e != nil {
		return nil, e
	}
	out := map[string]string{}
	for _, v := range values {
		out[strings.ToLower(v["variable_name"])] = v["value"]
	}
	return out, nil
}
func checkGalera(ctx context.Context, s *session, expected string) error {
	v, e := status(ctx, s)
	if e != nil {
		return e
	}
	vars, e := variables(ctx, s)
	if e != nil {
		return e
	}
	if expected == "" || v["wsrep_cluster_state_uuid"] != expected {
		return fmt.Errorf("Galera 集群身份已经变化")
	}
	if v["wsrep_cluster_status"] != "Primary" || v["wsrep_local_state_comment"] != "Synced" || v["wsrep_ready"] != "ON" || v["wsrep_connected"] != "ON" || vars["read_only"] == "ON" {
		return fmt.Errorf("Galera 成员当前不可写")
	}
	return nil
}
func (Manager) Read(ctx context.Context, c protocol.DatabaseConnection) (protocol.DatabaseState, error) {
	out := protocol.DatabaseState{State: "unavailable", CheckedAt: time.Now().UTC(), Databases: []protocol.DatabaseSchema{}, Users: []protocol.DatabaseUser{}, Servers: []protocol.DatabaseProxyServer{}, Pools: []protocol.DatabaseProxyPool{}, ProxyUsers: []protocol.DatabaseProxyUser{}}
	s, e := connect(ctx, c)
	if e != nil {
		return out, e
	}
	defer s.Close()
	if c.Kind == "proxysql" {
		return readProxy(ctx, s, c, out)
	}
	v, e := status(ctx, s)
	if e != nil {
		return out, e
	}
	vars, e := variables(ctx, s)
	if e != nil {
		return out, e
	}
	out.Version = vars["version"]
	out.ClusterID = v["wsrep_cluster_state_uuid"]
	out.ClusterName = vars["wsrep_cluster_name"]
	out.ConfiguredMembers = vars["wsrep_cluster_address"]
	out.ActualMembers = v["wsrep_incoming_addresses"]
	out.MemberAddress = vars["wsrep_node_address"]
	out.ClusterSize = int(number(v["wsrep_cluster_size"]))
	out.Primary = v["wsrep_cluster_status"] == "Primary"
	out.Ready = v["wsrep_ready"] == "ON"
	out.Synced = v["wsrep_local_state_comment"] == "Synced"
	out.ReadOnly = vars["read_only"] == "ON"
	out.Connections = int64(number(v["threads_connected"]))
	out.ReceiveQueue = number(v["wsrep_local_recv_queue"])
	out.SendQueue = number(v["wsrep_local_send_queue"])
	out.FlowControl = number(v["wsrep_flow_control_paused"])
	out.State = "ready"
	if !out.Ready || !out.Primary || !out.Synced {
		out.State = "unavailable"
	}
	dbs, e := rows(ctx, s, "SELECT s.SCHEMA_NAME,s.DEFAULT_CHARACTER_SET_NAME,s.DEFAULT_COLLATION_NAME,COALESCE(SUM(t.DATA_LENGTH+t.INDEX_LENGTH),0) AS bytes FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA=s.SCHEMA_NAME GROUP BY s.SCHEMA_NAME,s.DEFAULT_CHARACTER_SET_NAME,s.DEFAULT_COLLATION_NAME ORDER BY s.SCHEMA_NAME")
	if e != nil {
		return out, e
	}
	for _, d := range dbs {
		out.Databases = append(out.Databases, protocol.DatabaseSchema{Name: d["schema_name"], Charset: d["default_character_set_name"], Collation: d["default_collation_name"], Bytes: uint64(number(d["bytes"])), Protected: protocol.SystemDatabase(d["schema_name"])})
	}
	users, e := rows(ctx, s, "SELECT User,Host FROM mysql.user ORDER BY User,Host")
	if e != nil {
		return out, e
	}
	grants, e := rows(ctx, s, "SELECT GRANTEE,TABLE_SCHEMA,PRIVILEGE_TYPE FROM information_schema.SCHEMA_PRIVILEGES ORDER BY GRANTEE,TABLE_SCHEMA,PRIVILEGE_TYPE")
	if e != nil {
		return out, e
	}
	global, e := rows(ctx, s, "SELECT GRANTEE FROM information_schema.USER_PRIVILEGES WHERE PRIVILEGE_TYPE<>'USAGE'")
	if e != nil {
		return out, e
	}
	protected := map[string]bool{}
	for _, g := range global {
		protected[g["grantee"]] = true
	}
	byUser := map[string][]protocol.DatabaseGrant{}
	for _, g := range grants {
		key := g["grantee"]
		list := byUser[key]
		if len(list) == 0 || list[len(list)-1].Database != g["table_schema"] {
			list = append(list, protocol.DatabaseGrant{Database: g["table_schema"], Privileges: []string{}})
		}
		i := len(list) - 1
		list[i].Privileges = append(list[i].Privileges, g["privilege_type"])
		byUser[key] = list
	}
	sst := strings.SplitN(vars["wsrep_sst_auth"], ":", 2)[0]
	for _, u := range users {
		key := literal(u["user"]) + "@" + literal(u["host"])
		user := protocol.DatabaseUser{Name: u["user"], Host: u["host"], Protected: protocol.SystemDatabaseUser(u["user"], c) || u["user"] == sst || protected[key], Grants: byUser[key]}
		if user.Grants == nil {
			user.Grants = []protocol.DatabaseGrant{}
		}
		out.Users = append(out.Users, user)
	}
	return out, nil
}

const proxyColumns = "username,password,active,use_ssl,default_hostgroup,default_schema,schema_locked,transaction_persistent,fast_forward,backend,frontend,max_connections,attributes,comment"

func proxyRows(ctx context.Context, s *session, table string) ([]map[string]string, error) {
	return rows(ctx, s, "SELECT "+proxyColumns+" FROM "+table+" ORDER BY username,backend,frontend")
}
func proxyLayers(ctx context.Context, s *session) ([][]map[string]string, error) {
	out := [][]map[string]string{}
	for _, table := range []string{"mysql_users", "runtime_mysql_users", "disk.mysql_users"} {
		r, e := proxyRows(ctx, s, table)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}

// 运行表按前后端分别保存启用用户；配置中的同一行可同时承担两种角色。
func proxyRuntimeFingerprint(users []map[string]string) string {
	projected := []map[string]string{}
	for _, user := range users {
		if user["active"] != "1" {
			continue
		}
		for _, role := range []string{"backend", "frontend"} {
			if user[role] == "1" {
				row := maps.Clone(user)
				row["backend"], row["frontend"] = "0", "0"
				row[role] = "1"
				projected = append(projected, row)
			}
		}
	}
	sort.Slice(projected, func(i, j int) bool {
		a, b := projected[i], projected[j]
		return a["username"]+"/"+a["backend"]+a["frontend"] < b["username"]+"/"+b["backend"]+b["frontend"]
	})
	return fingerprint(projected)
}
func readProxy(ctx context.Context, s *session, c protocol.DatabaseConnection, out protocol.DatabaseState) (protocol.DatabaseState, error) {
	v, e := rows(ctx, s, "SELECT @@version")
	if e != nil {
		return out, e
	}
	if len(v) > 0 {
		out.Version = v[0]["@@version"]
	}
	layers, e := proxyLayers(ctx, s)
	if e != nil {
		return out, e
	}
	out.PendingChanges = proxyRuntimeFingerprint(layers[0]) != proxyRuntimeFingerprint(layers[1]) || fingerprint(layers[0]) != fingerprint(layers[2])
	for _, u := range layers[0] {
		state := "in_sync"
		if out.PendingChanges {
			state = "pending"
		}
		out.ProxyUsers = append(out.ProxyUsers, protocol.DatabaseProxyUser{Name: u["username"], Active: u["active"] == "1", Hostgroup: int(number(u["default_hostgroup"])), State: state, Protected: protocol.SystemDatabaseUser(u["username"], c)})
	}
	servers, e := rows(ctx, s, "SELECT hostgroup_id,hostname,port,status FROM runtime_mysql_servers ORDER BY hostgroup_id,hostname,port")
	if e != nil {
		return out, e
	}
	for _, v := range servers {
		out.Servers = append(out.Servers, protocol.DatabaseProxyServer{Hostgroup: int(number(v["hostgroup_id"])), Host: v["hostname"], Port: int(number(v["port"])), State: v["status"]})
	}
	pools, e := rows(ctx, s, "SELECT hostgroup,srv_host,srv_port,ConnUsed,ConnFree,ConnERR FROM stats_mysql_connection_pool ORDER BY hostgroup,srv_host,srv_port")
	if e != nil {
		return out, e
	}
	for _, v := range pools {
		out.Pools = append(out.Pools, protocol.DatabaseProxyPool{Hostgroup: int(number(v["hostgroup"])), Host: v["srv_host"], Port: int(number(v["srv_port"])), Used: int64(number(v["connused"])), Free: int64(number(v["connfree"])), Errors: int64(number(v["connerr"]))})
		out.Connections += int64(number(v["connused"]))
	}
	out.State = "ready"
	out.Ready = true
	return out, nil
}
func (Manager) PasswordHash(ctx context.Context, c protocol.DatabaseConnection, user, host string, expected ...string) (string, error) {
	a := protocol.DatabaseAction{Connection: c, Kind: "delete_user", User: user, Host: host, ClusterID: "read"}
	if e := a.Validate(); e != nil {
		return "", e
	}
	s, e := connect(ctx, c)
	if e != nil {
		return "", e
	}
	defer s.Close()
	if len(expected) > 0 && expected[0] != "" {
		if e = checkGalera(ctx, s, expected[0]); e != nil {
			return "", e
		}
	}
	privileges, e := rows(ctx, s, "SELECT PRIVILEGE_TYPE FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=? AND PRIVILEGE_TYPE<>'USAGE'", literal(user)+"@"+literal(host))
	if e != nil {
		return "", e
	}
	if len(privileges) > 0 {
		return "", fmt.Errorf("系统及管理账号保持只读")
	}
	r, e := rows(ctx, s, "SELECT plugin,authentication_string FROM mysql.user WHERE User=? AND Host=?", user, host)
	if e != nil {
		return "", e
	}
	if len(r) != 1 || r[0]["plugin"] != "mysql_native_password" {
		return "", fmt.Errorf("账号需要 mysql_native_password 验证信息")
	}
	return r[0]["authentication_string"], nil
}
func (Manager) Prepare(ctx context.Context, a protocol.DatabaseAction) ([]string, error) {
	if e := a.Validate(); e != nil {
		return nil, e
	}
	s, e := connect(ctx, a.Connection)
	if e != nil {
		return nil, e
	}
	defer s.Close()
	if a.Connection.Kind == "proxysql" {
		layers, e := proxyLayers(ctx, s)
		if e != nil {
			return nil, e
		}
		if proxyRuntimeFingerprint(layers[0]) != proxyRuntimeFingerprint(layers[1]) || fingerprint(layers[0]) != fingerprint(layers[2]) {
			return nil, fmt.Errorf("ProxySQL 存在其他待应用的账号配置")
		}
		return []string{"memory", "runtime", "disk"}, nil
	}
	if e = checkGalera(ctx, s, a.ClusterID); e != nil {
		return nil, e
	}
	if a.User != "" {
		if e = guardDatabaseUser(ctx, s, a); e != nil {
			return nil, e
		}
		u, e := rows(ctx, s, "SELECT User,Super_priv,Grant_priv,Create_user_priv,Repl_slave_priv,Repl_client_priv FROM mysql.user WHERE User=? AND Host=?", a.User, a.Host)
		if e != nil {
			return nil, e
		}
		for _, r := range u {
			for _, p := range []string{"super_priv", "grant_priv", "create_user_priv", "repl_slave_priv", "repl_client_priv"} {
				if r[p] == "Y" {
					return nil, fmt.Errorf("系统及管理账号保持只读")
				}
			}
		}
		vars, e := variables(ctx, s)
		if e != nil {
			return nil, e
		}
		if strings.SplitN(vars["wsrep_sst_auth"], ":", 2)[0] == a.User {
			return nil, fmt.Errorf("SST 账号保持只读")
		}
		if a.Kind == "create_user" && len(u) > 0 {
			return nil, fmt.Errorf("数据库账号已经存在")
		}
		if a.Kind != "create_user" && a.Kind != "delete_user" && len(u) == 0 {
			return nil, fmt.Errorf("数据库账号不存在")
		}
	}
	if a.Kind == "create_database" {
		r, e := rows(ctx, s, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", a.Database)
		if e != nil {
			return nil, e
		}
		if len(r) > 0 {
			return nil, fmt.Errorf("数据库已经存在")
		}
	}
	return []string{"database"}, nil
}
func (Manager) Observe(ctx context.Context, a protocol.DatabaseAction, step string) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC()}
	s, e := connect(ctx, a.Connection)
	if e != nil {
		return out, e
	}
	defer s.Close()
	if a.Connection.Kind == "proxysql" {
		layers, e := proxyLayers(ctx, s)
		if e != nil {
			return out, e
		}
		out.Source = fingerprint(layers[0])
		out.Fingerprint = fingerprint(layers)
		if step == "runtime" {
			out.Satisfied = proxyRuntimeFingerprint(layers[0]) == proxyRuntimeFingerprint(layers[1])
			return out, nil
		}
		if step == "disk" {
			out.Satisfied = fingerprint(layers[0]) == fingerprint(layers[2])
			return out, nil
		}
		selected := []map[string]string{}
		for _, u := range layers[0] {
			if u["username"] == a.User {
				selected = append(selected, u)
			}
		}
		switch a.Kind {
		case "proxy_delete":
			out.Satisfied = len(selected) == 0
		case "proxy_disable":
			out.Satisfied = true
			for _, u := range selected {
				out.Satisfied = out.Satisfied && u["active"] == "0"
			}
		case "proxy_upsert":
			out.Satisfied = len(selected) > 0
			for _, u := range selected {
				out.Satisfied = out.Satisfied && u["password"] == a.PasswordHash && u["active"] == "1" && int(number(u["default_hostgroup"])) == a.Hostgroup
			}
		}
		return out, nil
	}
	if e = checkGalera(ctx, s, a.ClusterID); e != nil {
		return out, e
	}
	if a.Kind == "create_database" || a.Kind == "delete_database" {
		v, e := rows(ctx, s, "SELECT SCHEMA_NAME,DEFAULT_CHARACTER_SET_NAME,DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", a.Database)
		if e != nil {
			return out, e
		}
		tables, e := rows(ctx, s, "SELECT TABLE_NAME,ENGINE,TABLE_ROWS,DATA_LENGTH,INDEX_LENGTH,CREATE_TIME,UPDATE_TIME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? ORDER BY TABLE_NAME", a.Database)
		if e != nil {
			return out, e
		}
		out.Fingerprint = fingerprint([]any{v, tables})
		out.Satisfied = a.Kind == "delete_database" && len(v) == 0 || a.Kind == "create_database" && len(v) == 1 && v[0]["default_character_set_name"] == a.Charset
		return out, nil
	}
	u, e := rows(ctx, s, "SELECT User,Host,plugin,authentication_string,Super_priv,Grant_priv,Create_user_priv FROM mysql.user WHERE User=? AND Host=?", a.User, a.Host)
	if e != nil {
		return out, e
	}
	g, e := rows(ctx, s, "SELECT PRIVILEGE_TYPE FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=? AND TABLE_SCHEMA=? ORDER BY PRIVILEGE_TYPE", account(a), a.Database)
	if e != nil {
		return out, e
	}
	out.Fingerprint = fingerprint([]any{u, g})
	switch a.Kind {
	case "create_user", "password":
		out.Satisfied = len(u) == 1 && u[0]["plugin"] == "mysql_native_password" && u[0]["authentication_string"] == a.PasswordHash
	case "delete_user":
		out.Satisfied = len(u) == 0
	case "grant", "revoke":
		out.Satisfied = true
		for _, p := range a.Privileges {
			found := false
			for _, v := range g {
				found = found || v["privilege_type"] == p
			}
			if a.Kind == "grant" && !found || a.Kind == "revoke" && found {
				out.Satisfied = false
			}
		}
	}
	return out, nil
}
func (Manager) Apply(ctx context.Context, a protocol.DatabaseAction, step string) error {
	if e := a.Validate(); e != nil {
		return e
	}
	s, e := connect(ctx, a.Connection)
	if e != nil {
		return e
	}
	defer s.Close()
	query := ""
	if a.Connection.Kind == "proxysql" {
		switch step {
		case "runtime":
			query = "LOAD MYSQL USERS TO RUNTIME"
		case "disk":
			query = "SAVE MYSQL USERS TO DISK"
		case "memory":
			switch a.Kind {
			case "proxy_upsert":
				existing, err := rows(ctx, s, "SELECT username FROM mysql_users WHERE username=?", a.User)
				if err != nil {
					return err
				}
				if len(existing) > 0 {
					_, e = s.ExecContext(ctx, "UPDATE mysql_users SET password=?,active=1,default_hostgroup=? WHERE username=?", a.PasswordHash, a.Hostgroup, a.User)
				} else {
					_, e = s.ExecContext(ctx, "INSERT INTO mysql_users(username,password,active,default_hostgroup,transaction_persistent) VALUES(?,?,1,?,1)", a.User, a.PasswordHash, a.Hostgroup)
				}
				return clean(e)
			case "proxy_disable":
				_, e = s.ExecContext(ctx, "UPDATE mysql_users SET active=0 WHERE username=?", a.User)
				return clean(e)
			case "proxy_delete":
				_, e = s.ExecContext(ctx, "DELETE FROM mysql_users WHERE username=?", a.User)
				return clean(e)
			}
		}
	} else {
		if e = checkGalera(ctx, s, a.ClusterID); e != nil {
			return e
		}
		if e = guardDatabaseUser(ctx, s, a); e != nil {
			return e
		}
		switch a.Kind {
		case "create_database":
			query = "CREATE DATABASE " + ident(a.Database) + " CHARACTER SET " + a.Charset
		case "delete_database":
			query = "DROP DATABASE " + ident(a.Database)
		case "create_user":
			query = "CREATE USER " + account(a) + " IDENTIFIED VIA mysql_native_password USING " + literal(a.PasswordHash)
		case "password":
			query = "ALTER USER " + account(a) + " IDENTIFIED VIA mysql_native_password USING " + literal(a.PasswordHash)
		case "delete_user":
			query = "DROP USER " + account(a)
		case "grant":
			query = "GRANT " + strings.Join(a.Privileges, ",") + " ON " + ident(a.Database) + ".* TO " + account(a)
		case "revoke":
			query = "REVOKE " + strings.Join(a.Privileges, ",") + " ON " + ident(a.Database) + ".* FROM " + account(a)
		}
	}
	if query == "" {
		return fmt.Errorf("数据库步骤无效")
	}
	_, e = s.ExecContext(ctx, query)
	return clean(e)
}

func SortedPrivileges(p []string) []string {
	out := slices.Clone(p)
	sort.Strings(out)
	return slices.Compact(out)
}
