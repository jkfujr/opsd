package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type Kind string

const (
	Executor Kind = "executor"
	Node     Kind = "node"
	Metrics  Kind = "metrics"
)

//go:embed migrations/*/*.sql
var migrations embed.FS

// Open 打开指定子系统的数据库，迁移成功后才交给调用者使用。
func Open(ctx context.Context, path string, kind Kind) (*sql.DB, error) {
	if kind != Executor && kind != Node && kind != Metrics {
		return nil, fmt.Errorf("未知数据库类型：%s", kind)
	}
	source, err := fs.Sub(migrations, "migrations/"+string(kind))
	if err != nil {
		return nil, err
	}
	return open(ctx, path, source)
}

func open(ctx context.Context, path string, source fs.FS) (*sql.DB, error) {
	uri, err := databaseURL(path)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "synchronous(FULL)", "busy_timeout(5000)"} {
		params.Add("_pragma", pragma)
	}
	uri.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("打开数据库：%w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(ctx, db, source); err != nil {
		db.Close()
		return nil, err
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("启用 WAL：%w", err)
	}
	if mode != "wal" {
		db.Close()
		return nil, fmt.Errorf("数据库未进入 WAL 模式：%s", mode)
	}
	return db, nil
}

func databaseURL(path string) (url.URL, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return url.URL{}, fmt.Errorf("解析数据库路径：%w", err)
	}
	uriPath := filepath.ToSlash(abs)
	if filepath.VolumeName(abs) != "" {
		uriPath = "/" + uriPath
	}
	return url.URL{Scheme: "file", Path: uriPath}, nil
}

// ReadOnly 用于本机查看入口，不迁移、不初始化，也不切换日志模式。
func ReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	uri, err := databaseURL(path)
	if err != nil {
		return nil, err
	}
	uri.RawQuery = "mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB, source fs.FS) error {
	names, err := fs.Glob(source, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for i, name := range names {
		prefix, _, ok := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version != i+1 {
			return fmt.Errorf("迁移编号必须从 001 连续递增：%s", name)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("连接数据库：%w", err)
	}
	defer conn.Close()
	// 在同一写事务内读取版本，避免两个启动者重复应用同一迁移。
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("取得迁移写入资格：%w", err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(names) {
		return fmt.Errorf("数据库版本 %d 高于程序支持版本 %d，拒绝写入", current, len(names))
	}
	for i := current; i < len(names); i++ {
		body, err := fs.ReadFile(source, names[i])
		if err != nil {
			return err
		}
		// 有些迁移要兼容“已应用过前一版”的库（例如列已经存在），
		// SQL 本身无法表达 ADD COLUMN IF NOT EXISTS，只能交给 Go 判断。
		if hook, ok := migrationHooks[names[i]]; ok {
			if err := hook(ctx, conn); err != nil {
				return fmt.Errorf("执行迁移 %s：%w", names[i], err)
			}
		} else if _, err := conn.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("执行迁移 %s：%w", names[i], err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations(version, name) VALUES (?, ?)", i+1, names[i]); err != nil {
			return fmt.Errorf("保存迁移版本：%w", err)
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("提交数据库迁移：%w", err)
	}
	return nil
}

// migrationHooks 处理需要在 Go 里分支的迁移。
var migrationHooks = map[string]func(context.Context, *sql.Conn) error{
	"012_queue_resource_key.sql": migrateQueueResourceKey,
}

// migrateQueueResourceKey 把资源键从 create_key 迁到 resource_key。
//
// 两个已推送的结构都要能升级：只跑过 011 的库没有 resource_key，
// 跑过早期 7515d25 的库已经有这一列——重复 ADD COLUMN 会直接失败。
func migrateQueueResourceKey(ctx context.Context, conn *sql.Conn) error {
	var has int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('queue') WHERE name='resource_key'").Scan(&has); err != nil {
		return err
	}
	if has == 0 {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE queue ADD COLUMN resource_key TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, "UPDATE queue SET resource_key = create_key WHERE resource_key = '' AND create_key <> ''"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "DROP INDEX IF EXISTS queue_create_key"); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS queue_resource_key
		ON queue(resource_key) WHERE resource_key <> '' AND terminal = 0`)
	return err
}
