package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestOpenAndReopen(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []Kind{Executor, Node} {
		t.Run(string(kind), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "含 空格.db")
			for range 2 {
				db, err := Open(ctx, path, kind)
				if err != nil {
					t.Fatal(err)
				}
				var count, version, sync, foreign int
				var mode string
				for query, dest := range map[string]any{
					"SELECT count(*) FROM schema_migrations": &count,
					"PRAGMA user_version":                    &version,
					"PRAGMA synchronous":                     &sync,
					"PRAGMA foreign_keys":                    &foreign,
					"PRAGMA journal_mode":                    &mode,
				} {
					if err := db.QueryRowContext(ctx, query).Scan(dest); err != nil {
						t.Fatal(err)
					}
				}
				expectedVersion := 6
				if kind == Node {
					expectedVersion = 2
				}
				if count != expectedVersion || version != expectedVersion || sync != 2 || foreign != 1 || mode != "wal" {
					t.Fatalf("数据库配置或迁移错误：记录=%d 版本=%d 同步=%d 外键=%d 日志=%s", count, version, sync, foreign, mode)
				}
				db.Close()
			}
		})
	}
}

func TestExistingNodeDatabaseKeepsAccountAndQueue(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "node.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(migrations, "migrations/node/001_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.SplitN(string(body), "\n", 2)[1]
	if _, err = db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO account VALUES(1,'existing','hash','/signin',7,'encrypted'); INSERT INTO queue(seq,id,origin,kind,payload,request_key,request_digest,phase,body,terminal) VALUES(9,'original','origin','firewall','encrypted','request',X'01','submitted','{}',0)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		db, err = Open(ctx, path, Node)
		if err != nil {
			t.Fatal(err)
		}
		var username, id string
		var revision, sequence, terminal int
		if err = db.QueryRow("SELECT username,revision FROM account").Scan(&username, &revision); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow("SELECT id,seq,terminal FROM queue").Scan(&id, &sequence, &terminal); err != nil {
			t.Fatal(err)
		}
		if username != "existing" || revision != 7 || id != "original" || sequence != 9 || terminal != 0 {
			t.Fatal("迁移改变了已有身份或受理记录")
		}
		db.Close()
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "failed.db")
	source, _ := fs.Sub(migrations, "migrations/executor")
	first, _ := fs.ReadFile(source, "001_schema.sql")
	broken := fstest.MapFS{
		"001_schema.sql": {Data: first},
		"002_broken.sql": {Data: []byte("CREATE TABLE partial(id INTEGER); INSERT INTO missing VALUES (1);")},
	}
	if db, err := open(ctx, path, broken); err == nil {
		db.Close()
		t.Fatal("错误迁移不应成功")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('partial', 'schema_migrations')").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if version != 0 || tables != 0 {
		t.Fatalf("迁移残留：版本=%d 表=%d", version, tables)
	}
	db, err = Open(ctx, path, Executor)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func TestNewerDatabaseIsNotChanged(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "newer.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=99; CREATE TABLE future(value TEXT); INSERT INTO future VALUES ('保留');"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if opened, err := Open(ctx, path, Executor); err == nil || !strings.Contains(err.Error(), "拒绝写入") {
		if opened != nil {
			opened.Close()
		}
		t.Fatalf("应拒绝较新数据库：%v", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var value, mode string
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT value FROM future").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if version != 99 || value != "保留" || mode != "delete" {
		t.Fatalf("较新数据库被修改：%d %s %s", version, value, mode)
	}
}
