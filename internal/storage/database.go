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
)

//go:embed migrations/*/*.sql
var migrations embed.FS

// Open 打开指定子系统的数据库，迁移成功后才交给调用者使用。
func Open(ctx context.Context, path string, kind Kind) (*sql.DB, error) {
	if kind != Executor && kind != Node {
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
		if _, err := conn.ExecContext(ctx, string(body)); err != nil {
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
