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
				expectedVersion := 7
				if kind == Node {
					expectedVersion = 5
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
	if _, err = db.Exec(`INSERT INTO account VALUES(1,'existing','hash','/signin',7,'encrypted'); INSERT INTO queue(seq,id,origin,kind,payload,request_key,request_digest,phase,body,terminal) VALUES(9,'original','origin','firewall','encrypted','request',X'01','submitted','{}',0); INSERT INTO account_navigation VALUES(1,4,'["settings","nodes","overview","containers","databases","storage","firewall"]')`); err != nil {
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
		var order string
		if err = db.QueryRow("SELECT order_json,revision FROM account_navigation").Scan(&order, &revision); err != nil {
			t.Fatal(err)
		}
		if order != `["tools","settings","nodes","overview","containers","databases","storage","firewall"]` || revision != 5 {
			t.Fatal("菜单迁移改变了原顺序或重复插入工具", order, revision)
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

func TestAPIV2MigrationPreservesExistingRecords(t *testing.T) {
	ctx := t.Context()
	source, _ := fs.Sub(migrations, "migrations/node")
	before := fstest.MapFS{}
	files, err := fs.ReadDir(source, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		if entry.Name() >= "005" {
			continue
		}
		body, e := fs.ReadFile(source, entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		before[entry.Name()] = &fstest.MapFile{Data: body}
	}
	path := filepath.Join(t.TempDir(), "existing-v1.db")
	db, err := open(ctx, path, before)
	if err != nil {
		t.Fatal(err)
	}
	token := `{"id":"existing-token","owner":"existing-node","revision":7,"name":"原令牌","enabled":true,"grants":[{"module":"containers","level":"manage","scope":"all","resources":[]}],"source_ips":[],"created_at":"2026-11-10T00:00:00Z"}`
	task := `{"id":"existing-task","revision":3,"script":"printf existing","enabled":false}`
	operation := `{"id":"existing-operation","state":"applying","revision":4,"actor":"existing-node","queue_sequence":11}`
	identity := `{"revision":5,"name":"原节点"}`
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO node_identity(id,cluster,private_key,public_key,signing_key,age_key,config) VALUES(1,'cluster',X'01',X'02',X'03','key',?)", []any{identity}},
		{"INSERT INTO account VALUES(1,'existing','hash','/old-entry',7,'encrypted')", nil},
		{"INSERT INTO api_tokens VALUES('existing-token',X'04',?,1,7)", []any{token}},
		{"INSERT INTO scheduled_tasks VALUES('existing-task',3,?)", []any{task}},
		{"INSERT INTO queue(seq,id,origin,kind,payload,request_key,request_digest,body,phase) VALUES(11,'existing-operation','existing-node','container','encrypted','existing-key',X'05',?,'submitted')", []any{operation}},
		{"INSERT INTO console_themes VALUES('existing-theme','theme','1.0.0','{\"api_version\":1}','[]','upload',1)", nil},
		{"UPDATE console_theme_selection SET installation_id='existing-theme'", nil},
	}
	for _, statement := range statements {
		if _, err = db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	for range 2 {
		db, err = Open(ctx, path, Node)
		if err != nil {
			t.Fatal(err)
		}
		for query, want := range map[string]string{"SELECT config FROM node_identity": identity, "SELECT body FROM api_tokens": token, "SELECT body FROM scheduled_tasks": task, "SELECT body FROM queue": operation, "SELECT request_key FROM queue": "existing-key", "SELECT installation_id FROM console_theme_selection": "existing-theme", "SELECT entrance FROM account": "/old-entry"} {
			var got string
			if err = db.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
				t.Fatalf("升级改变了原数据：%s %s %v", query, got, err)
			}
		}
		var count int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM api_requests").Scan(&count); err != nil || count != 0 {
			t.Fatal("迁移生成了历史请求回执", count, err)
		}
		db.Close()
	}
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
