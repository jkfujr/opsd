package sharedsql

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// 迁移必须能在中断后重跑：v1 结构 → 只做了一部分 → 再跑一次仍然成功。
//
// 需要真实 MySQL/MariaDB（information_schema 的行为无法在 SQLite 上模拟）。
func TestMigrateResumesAfterInterruption(t *testing.T) {
	dsn := os.Getenv("OPSD_SHARED_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 OPSD_SHARED_TEST_DSN 指向一个可写的 MariaDB 库")
	}
	ctx := context.Background()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 每个用例从干净的 v1 开始。
	for _, q := range []string{
		"DROP TABLE IF EXISTS shared_requests",
		"DROP TABLE IF EXISTS shared_flows",
		"DROP TABLE IF EXISTS shared_objects",
		"DROP TABLE IF EXISTS shared_records",
		"DROP TABLE IF EXISTS shared_watermarks",
		"DROP TABLE IF EXISTS shared_metadata",
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// 用 v1 的建表语句（迁移前结构）。
	for _, q := range []string{
		"CREATE TABLE shared_metadata(id TINYINT PRIMARY KEY,cluster_id VARCHAR(64) NOT NULL,schema_version INT NOT NULL,phase VARCHAR(16) NOT NULL,write_version BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB",
		"CREATE TABLE shared_requests(actor VARCHAR(128) NOT NULL,request_key VARCHAR(128) NOT NULL,digest CHAR(64) NOT NULL,kind VARCHAR(32) NOT NULL,resource_id VARCHAR(192) NOT NULL,result MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(actor,request_key)) ENGINE=InnoDB",
		"CREATE TABLE shared_flows(id VARCHAR(128) PRIMARY KEY,resource_id VARCHAR(128) NOT NULL,coordinator CHAR(32) NOT NULL,epoch BIGINT UNSIGNED NOT NULL,revision BIGINT UNSIGNED NOT NULL,lease_until DATETIME(6) NOT NULL,terminal BOOL NOT NULL,body MEDIUMBLOB NOT NULL,KEY active_flows(terminal,lease_until),KEY resource_flows(resource_id,terminal)) ENGINE=InnoDB",
		"INSERT INTO shared_metadata(id,cluster_id,schema_version,phase) VALUES(1,'cluster-test',1,'active')",
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// 第一次迁移：做完一部分就停（模拟中断）。
	if err := ensureColumn(ctx, db, "shared_requests", "origin",
		"VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"); err != nil {
		t.Fatal(err)
	}
	// 此时版本仍是 v1，结构只改了一半。
	var version int
	if err = db.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("中断后版本不应推进：%d", version)
	}
	// 重跑应当成功（不会在重复列上失败）。
	if err = Migrate(ctx, db, "cluster-test"); err != nil {
		t.Fatalf("重跑迁移失败：%v", err)
	}
	if err = db.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("版本未发布：%d", version)
	}
	// 主键列序必须完整。
	have, err := primaryKey(ctx, db, "shared_requests")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"origin", "actor", "request_key"}
	if len(have) != len(want) {
		t.Fatalf("主键列序不完整：%v", have)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Fatalf("主键列序不符：%v", have)
		}
	}
	// 再跑一次是幂等的。
	if err = Migrate(ctx, db, "cluster-test"); err != nil {
		t.Fatalf("重复迁移应无副作用：%v", err)
	}

	// 旧在途流程没有资源键时不许发布：新版据此判断库名是否被占用。
	if _, err = db.ExecContext(ctx, "UPDATE shared_metadata SET schema_version=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO shared_flows(id,resource_id,resource_key,coordinator,epoch,revision,lease_until,terminal,body)
		VALUES('legacy-flow','res','',  REPEAT('a',32),1,1,TIMESTAMPADD(SECOND,60,CURRENT_TIMESTAMP(6)),0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db, "cluster-test"); err == nil {
		t.Fatal("旧在途流程未排空时不应发布 v2")
	}
	if err = db.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("拒绝发布后版本被改了：%d", version)
	}
}
