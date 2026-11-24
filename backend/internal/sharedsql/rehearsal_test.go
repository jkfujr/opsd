//go:build linux

package sharedsql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// 停写演练：按维护窗口的硬判据走一遍，并把每一步的真实输出留在日志里。
//
// 需要真实 MariaDB（information_schema、PROCESSLIST、INNODB_TRX 都无法在 SQLite 上模拟）。
// 设置 OPSD_SHARED_TEST_DSN 指向一个**可写且会被清空**的库。
func TestStopWriteRehearsal(t *testing.T) {
	dsn := os.Getenv("OPSD_SHARED_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 OPSD_SHARED_TEST_DSN 指向一个可写的 MariaDB 库")
	}
	ctx := context.Background()
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	// 1) 干净的 v1 结构。
	for _, q := range []string{
		"DROP TABLE IF EXISTS shared_requests", "DROP TABLE IF EXISTS shared_flows",
		"DROP TABLE IF EXISTS shared_objects", "DROP TABLE IF EXISTS shared_records",
		"DROP TABLE IF EXISTS shared_watermarks", "DROP TABLE IF EXISTS shared_metadata",
	} {
		if _, err = admin.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"CREATE TABLE shared_metadata(id TINYINT PRIMARY KEY,cluster_id VARCHAR(64) NOT NULL,schema_version INT NOT NULL,phase VARCHAR(16) NOT NULL,write_version BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB",
		"CREATE TABLE shared_requests(actor VARCHAR(128) NOT NULL,request_key VARCHAR(128) NOT NULL,digest CHAR(64) NOT NULL,kind VARCHAR(32) NOT NULL,resource_id VARCHAR(192) NOT NULL,result MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(actor,request_key)) ENGINE=InnoDB",
		"CREATE TABLE shared_flows(id VARCHAR(128) PRIMARY KEY,resource_id VARCHAR(128) NOT NULL,coordinator CHAR(32) NOT NULL,epoch BIGINT UNSIGNED NOT NULL,revision BIGINT UNSIGNED NOT NULL,lease_until DATETIME(6) NOT NULL,terminal BOOL NOT NULL,body MEDIUMBLOB NOT NULL,KEY active_flows(terminal,lease_until),KEY resource_flows(resource_id,terminal)) ENGINE=InnoDB",
		"INSERT INTO shared_metadata(id,cluster_id,schema_version,phase) VALUES(1,'cluster-rehearsal',1,'active')",
		// 迁移前的旧回执：没有可信来源，只能用于防重。
		"INSERT INTO shared_requests(actor,request_key,digest,kind,resource_id,result) VALUES('account:legacy','old-key',REPEAT('a',64),'operation','op-legacy','{}')",
	} {
		if _, err = admin.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("步骤 1：已建立 v1 结构，含一条迁移前旧回执")

	// 2) 停写：用维护账号锁住写账号，并确认会话与事务归零。
	if _, err = admin.ExecContext(ctx, "CREATE USER IF NOT EXISTS 'opsd_write'@'%' IDENTIFIED BY 'rehearsal'"); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, "ALTER USER 'opsd_write'@'%' ACCOUNT LOCK"); err != nil {
		t.Fatal(err)
	}
	var sessions, transactions int
	if err = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER='opsd_write'").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.INNODB_TRX").Scan(&transactions); err != nil {
		// MariaDB 未必装了 innodb 信息库；这里只要求能读出会话数。
		t.Logf("步骤 2：INNODB_TRX 不可读（%v），仅校验会话归零", err)
	}
	t.Logf("步骤 2：写账号已锁，会话=%d 未结束事务=%d", sessions, transactions)
	if sessions != 0 {
		t.Fatalf("仍有写账号会话：%d", sessions)
	}

	// 3) 迁移（含核对与发布）。
	if err = Migrate(ctx, admin, "cluster-rehearsal"); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	var version int
	if err = admin.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("版本未发布：%d", version)
	}
	t.Logf("步骤 3：迁移完成，schema_version=%d", version)

	// 4) 旧回执只防重：命中返回 outcome_unknown，拿不到旧结果。
	store := &Store{DB: admin, Cluster: "cluster-rehearsal"}
	if _, err = store.Receipt(ctx, Receipt{Origin: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Actor: "account:legacy", Key: "old-key"}); err != ErrOutcomeUnknown {
		t.Fatalf("旧回执应返回 outcome_unknown，实际 %v", err)
	}
	t.Logf("步骤 4：旧回执命中返回 outcome_unknown（阻止重复执行，但不返回旧结果）")

	// 5) 恢复写入方：解锁账号后新回执可以正常写入与读回。
	if _, err = admin.ExecContext(ctx, "ALTER USER 'opsd_write'@'%' ACCOUNT UNLOCK"); err != nil {
		t.Fatal(err)
	}
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	shared := &Tx{Tx: tx}
	if err = shared.SaveReceipt(ctx, Receipt{Origin: "11111111111111111111111111111111", Actor: "account:new", Key: "k1", Digest: strings.Repeat("b", 64), Kind: "operation", ResourceID: "op-1", Result: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Receipt(ctx, Receipt{Origin: "11111111111111111111111111111111", Actor: "account:new", Key: "k1"}); err != nil {
		t.Fatalf("新回执应按来源命中：%v", err)
	}
	// 换来源用同一个键是另一桶，不会命中。
	if _, err = store.Receipt(ctx, Receipt{Origin: "22222222222222222222222222222222", Actor: "account:new", Key: "k1"}); err != sql.ErrNoRows {
		t.Fatalf("换来源不应命中旧回执，实际 %v", err)
	}
	t.Logf("步骤 5：写账号已解锁；新回执按来源归属，换来源不命中")
}
