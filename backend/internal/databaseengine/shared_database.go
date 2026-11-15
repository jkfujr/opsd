package databaseengine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func databasePrivilegeName(name string) string {
	return ident(databasePrivilegePattern(name))
}

func databasePrivilegePattern(name string) string {
	return strings.NewReplacer("\\", "\\\\", "_", "\\_", "%", "\\%").Replace(name)
}

func sharedAdmin(ctx context.Context, a protocol.DatabaseAction) (*sql.DB, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	c := mysql.NewConfig()
	c.User = a.Connection.Username
	c.Passwd = a.Connection.Password
	c.Net = "tcp"
	c.Addr = a.Connection.Address
	c.Timeout = 3 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second
	c.ParseTime = true
	if a.Connection.Socket != "" {
		c.Net = "unix"
		c.Addr = a.Connection.Socket
	}
	connector, err := mysql.NewConnector(c)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	return db, nil
}

func observeSharedDatabase(ctx context.Context, a protocol.DatabaseAction) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC()}
	db, err := sharedAdmin(ctx, a)
	if err != nil {
		return out, err
	}
	defer db.Close()
	p := a.Shared
	if p.Mode == "initialize" {
		var count int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='shared_metadata'", p.Database).Scan(&count); err != nil {
			return out, clean(err)
		}
		if count == 0 {
			out.Fingerprint = "empty"
			return out, nil
		}
		var cluster string
		var version int
		if err = db.QueryRowContext(ctx, "SELECT cluster_id,schema_version FROM "+ident(p.Database)+".shared_metadata WHERE id=1").Scan(&cluster, &version); err == sql.ErrNoRows {
			out.Fingerprint = "partial"
			return out, nil
		} else if err != nil {
			return out, clean(err)
		}
		if cluster != p.Cluster || version != sharedsql.SchemaVersion {
			return out, fmt.Errorf("OPSD 数据库身份或版本不匹配")
		}
		out.Satisfied = true
	} else {
		var userHash, monitorHash string
		var userLimit, monitorLimit int
		err = db.QueryRowContext(ctx, "SELECT COALESCE(MAX(IF(User=?,authentication_string,NULL)),''),COALESCE(MAX(IF(User=?,authentication_string,NULL)),''),COALESCE(MAX(IF(User=?,max_user_connections,NULL)),0),COALESCE(MAX(IF(User=?,max_user_connections,NULL)),0) FROM mysql.user WHERE Host='%'", protocol.SharedUsername(p.NodeID), protocol.SharedMonitorUser(p.NodeID), protocol.SharedUsername(p.NodeID), protocol.SharedMonitorUser(p.NodeID)).Scan(&userHash, &monitorHash, &userLimit, &monitorLimit)
		if err != nil {
			return out, clean(err)
		}
		if p.Mode == "revoke" {
			out.Satisfied = userHash == "" && monitorHash == ""
			var sessions int
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER IN (?,?)", protocol.SharedUsername(p.NodeID), protocol.SharedMonitorUser(p.NodeID)).Scan(&sessions); err != nil {
				return out, clean(err)
			}
			out.Satisfied = out.Satisfied && sessions == 0
		} else {
			var grants, outside, exact int
			grantee := literal(protocol.SharedUsername(p.NodeID)) + "@'%'"
			err = db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT PRIVILEGE_TYPE) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=? AND TABLE_SCHEMA IN (?,?) AND PRIVILEGE_TYPE IN ('SELECT','INSERT','UPDATE','DELETE') AND IS_GRANTABLE='NO'", grantee, p.Database, strings.ReplaceAll(p.Database, "_", "\\_")).Scan(&grants)
			if err != nil {
				return out, clean(err)
			}
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.db WHERE User=? AND Host='%' AND Db=?", protocol.SharedUsername(p.NodeID), strings.ReplaceAll(p.Database, "_", "\\_")).Scan(&exact); err != nil {
				return out, clean(err)
			}
			err = db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=?)-(?) + (SELECT COUNT(*) FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=? AND PRIVILEGE_TYPE<>'USAGE') + (SELECT COUNT(*) FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE=?) + (SELECT COUNT(*) FROM information_schema.COLUMN_PRIVILEGES WHERE GRANTEE=?) + (SELECT COUNT(*) FROM mysql.roles_mapping WHERE User=? AND Host='%')", grantee, grants, grantee, grantee, grantee, protocol.SharedUsername(p.NodeID)).Scan(&outside)
			if err != nil {
				return out, clean(err)
			}
			out.Satisfied = userHash == p.PasswordHash && monitorHash == p.MonitorHash && grants == 4 && outside == 0 && exact == 1 && userLimit == sharedsql.MaxConnections && monitorLimit == sharedsql.MonitorConnections
		}
		out.Fingerprint = fingerprint([]any{userHash, monitorHash, out.Satisfied})
	}
	return out, nil
}

func applySharedDatabase(ctx context.Context, a protocol.DatabaseAction) error {
	db, err := sharedAdmin(ctx, a)
	if err != nil {
		return err
	}
	defer db.Close()
	p := a.Shared
	conn, err := db.Conn(ctx)
	if err != nil {
		return clean(err)
	}
	defer conn.Close()
	s := &session{Conn: conn, release: func() {}}
	if err = checkGalera(ctx, s, a.ClusterID); err != nil {
		return err
	}
	if p.Mode == "initialize" {
		var tables int
		if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME NOT LIKE 'shared\\_%'", p.Database).Scan(&tables); err != nil {
			return clean(err)
		}
		if tables > 0 {
			return fmt.Errorf("目标数据库已有其他表")
		}
		if _, err = conn.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+ident(p.Database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
			return clean(err)
		}
		conn.Close()
		// 初始化专用连接的默认库，避免改变现有管理连接池的会话。
		cfg := mysql.NewConfig()
		cfg.User = a.Connection.Username
		cfg.Passwd = a.Connection.Password
		cfg.Net = "tcp"
		cfg.Addr = a.Connection.Address
		cfg.DBName = p.Database
		cfg.ParseTime = true
		cfg.Timeout = 3 * time.Second
		if a.Connection.Socket != "" {
			cfg.Net = "unix"
			cfg.Addr = a.Connection.Socket
		}
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			return e
		}
		migration := sql.OpenDB(connector)
		defer migration.Close()
		migration.SetMaxOpenConns(1)
		return sharedsql.Initialize(ctx, migration, p.Cluster)
	}
	for _, entry := range []struct{ user, hash string }{{protocol.SharedUsername(p.NodeID), p.PasswordHash}, {protocol.SharedMonitorUser(p.NodeID), p.MonitorHash}} {
		account := literal(entry.user) + "@'%'"
		if p.Mode == "revoke" {
			if _, err = conn.ExecContext(ctx, "DROP USER IF EXISTS "+account); err != nil {
				return clean(err)
			}
			ids, e := rows(ctx, s, "SELECT ID FROM information_schema.PROCESSLIST WHERE USER=?", entry.user)
			if e != nil {
				return e
			}
			for _, row := range ids {
				if _, err = conn.ExecContext(ctx, "KILL CONNECTION "+row["id"]); err != nil {
					return clean(err)
				}
			}
			continue
		}
		var old string
		err = conn.QueryRowContext(ctx, "SELECT authentication_string FROM mysql.user WHERE User=? AND Host='%'", entry.user).Scan(&old)
		if err != nil && err != sql.ErrNoRows {
			return clean(err)
		}
		if err == nil && old != entry.hash {
			return fmt.Errorf("OPSD 服务账号已存在且凭据不同")
		}
		if _, err = conn.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+account+" IDENTIFIED VIA mysql_native_password USING "+literal(entry.hash)); err != nil {
			return clean(err)
		}
		limit := sharedsql.MonitorConnections
		if entry.user == protocol.SharedUsername(p.NodeID) {
			limit = sharedsql.MaxConnections
		}
		if _, err = conn.ExecContext(ctx, fmt.Sprintf("ALTER USER %s WITH MAX_USER_CONNECTIONS %d", account, limit)); err != nil {
			return clean(err)
		}
		if entry.user == protocol.SharedUsername(p.NodeID) {
			if _, err = conn.ExecContext(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON "+databasePrivilegeName(p.Database)+".* TO "+account); err != nil {
				return clean(err)
			}
		}
	}
	return nil
}
