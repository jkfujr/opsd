//go:build linux

package sharedsql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 停写演练：按维护窗口的硬判据走一遍，两端都要有证据。
//
// 需要真实 MariaDB（PROCESSLIST、INNODB_TRX、information_schema 都无法在 SQLite 上模拟）。
// 设置 OPSD_SHARED_TEST_DSN 指向一个**可写且会被清空**的库。
func TestStopWriteRehearsal(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("OPSD_SHARED_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 OPSD_SHARED_TEST_DSN 指向一个可写的 MariaDB 库")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	// 1) 干净的 v1 结构 + 一张业务表（用来制造真实未提交事务）。
	reset := []string{
		"DROP TABLE IF EXISTS shared_requests", "DROP TABLE IF EXISTS shared_flows",
		"DROP TABLE IF EXISTS shared_objects", "DROP TABLE IF EXISTS shared_records",
		"DROP TABLE IF EXISTS shared_watermarks", "DROP TABLE IF EXISTS shared_metadata",
		"DROP TABLE IF EXISTS rehearsal_business",
		"CREATE TABLE shared_metadata(id TINYINT PRIMARY KEY,cluster_id VARCHAR(64) NOT NULL,schema_version INT NOT NULL,phase VARCHAR(16) NOT NULL,write_version BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB",
		"CREATE TABLE shared_requests(actor VARCHAR(128) NOT NULL,request_key VARCHAR(128) NOT NULL,digest CHAR(64) NOT NULL,kind VARCHAR(32) NOT NULL,resource_id VARCHAR(192) NOT NULL,result MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(actor,request_key)) ENGINE=InnoDB",
		"CREATE TABLE shared_flows(id VARCHAR(128) PRIMARY KEY,resource_id VARCHAR(128) NOT NULL,coordinator CHAR(32) NOT NULL,epoch BIGINT UNSIGNED NOT NULL,revision BIGINT UNSIGNED NOT NULL,lease_until DATETIME(6) NOT NULL,terminal BOOL NOT NULL,body MEDIUMBLOB NOT NULL,KEY active_flows(terminal,lease_until),KEY resource_flows(resource_id,terminal)) ENGINE=InnoDB",
		"CREATE TABLE rehearsal_business(id INT PRIMARY KEY, value INT NOT NULL) ENGINE=InnoDB",
		"INSERT INTO rehearsal_business VALUES(1, 0)",
		"INSERT INTO shared_metadata(id,cluster_id,schema_version,phase) VALUES(1,'cluster-rehearsal',1,'active')",
		// 迁移前的旧回执：没有可信来源，只能用于防重。
		"INSERT INTO shared_requests(actor,request_key,digest,kind,resource_id,result) VALUES('account:legacy','old-key',REPEAT('a',64),'operation','op-legacy','{}')",
	}
	for _, q := range reset {
		if _, err = admin.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("步骤 1：已建立 v1 结构（schema_version=1），含一条迁移前旧回执")

	// 2) 写账号持有一笔**已执行更新、尚未提交**的事务。
	writer := &sql.DB{}
	writerDSN := writeDSN(dsn)
	writer, err = sql.Open("mysql", writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	// 上一次演练可能留下同名账号：先删再建，保证口令就是这次用的。
	if _, err = admin.ExecContext(ctx, "DROP USER IF EXISTS 'opsd_write'@'%'"); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, "CREATE USER 'opsd_write'@'%' IDENTIFIED BY 'rehearsal-write'"); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, "GRANT ALL ON *.* TO 'opsd_write'@'%'"); err != nil {
		t.Fatal(err)
	}
	writeTx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writeTx.ExecContext(ctx, "UPDATE rehearsal_business SET value = value + 1 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	var login string
	if err = writeTx.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	var writeConn int
	if err = writeTx.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&writeConn); err != nil {
		t.Fatal(err)
	}
	t.Logf("步骤 2：写账号 %s 持有未提交事务（连接 %d）", login, writeConn)

	// 3) 停写：锁账号。锁住后新连接必须被拒，但已有事务仍在——两个观察都要有。
	if _, err = admin.ExecContext(ctx, "ALTER USER 'opsd_write'@'%' ACCOUNT LOCK"); err != nil {
		t.Fatal(err)
	}
	fresh, err := sql.Open("mysql", writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := fresh.PingContext(pingCtx)
	cancel()
	fresh.Close()
	if pingErr == nil {
		t.Fatal("账号锁定后新连接仍被接受")
	}
	t.Logf("步骤 3：账号已锁定，新连接被拒（%v）", pingErr)

	// 4) 未结束事务仍在，且此时版本必须还是 v1。
	var trx int
	if err = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE trx_mysql_thread_id=?", writeConn).Scan(&trx); err != nil {
		t.Fatalf("读取 INNODB_TRX 失败（不降级，按判据要求停在这里）：%v", err)
	}
	if trx != 1 {
		t.Fatalf("应观察到 1 个未提交事务，实际 %d", trx)
	}
	var version int
	if err = admin.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("停写期间版本不应推进：%d", version)
	}
	t.Logf("步骤 4：未提交事务仍在（%d），版本仍为 %d", trx, version)

	// 5) 回滚并关闭写连接，确认会话与事务都归零后才允许迁移。
	if err = writeTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var sessions, transactions int
		if err = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER='opsd_write'").Scan(&sessions); err != nil {
			t.Fatalf("读取 PROCESSLIST 失败：%v", err)
		}
		if err = admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.INNODB_TRX").Scan(&transactions); err != nil {
			t.Fatalf("读取 INNODB_TRX 失败：%v", err)
		}
		if sessions == 0 && transactions == 0 {
			t.Logf("步骤 5：会话与事务均已归零（sessions=%d transactions=%d）", sessions, transactions)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("会话/事务未归零：sessions=%d transactions=%d", sessions, transactions)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 6) 迁移（含核对与发布 v2）。
	if err = Migrate(ctx, admin, "cluster-rehearsal"); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	if err = admin.QueryRowContext(ctx, "SELECT schema_version FROM shared_metadata WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("版本未发布：%d", version)
	}
	t.Logf("步骤 6：迁移完成，schema_version=%d", version)

	// 7) 旧回执只防重：命中返回 outcome_unknown，不返回旧结果。
	store := &Store{DB: admin, Cluster: "cluster-rehearsal"}
	if _, err = store.Receipt(ctx, Receipt{Origin: strings.Repeat("a", 32), Actor: "account:legacy", Key: "old-key"}); err != ErrOutcomeUnknown {
		t.Fatalf("旧回执应返回 outcome_unknown，实际 %v", err)
	}
	t.Logf("步骤 7：旧回执命中返回 outcome_unknown")

	// 8) 恢复写入：新版写入方用**写账号**重新连接，写入并读回回执。
	if _, err = admin.ExecContext(ctx, "ALTER USER 'opsd_write'@'%' ACCOUNT UNLOCK"); err != nil {
		t.Fatal(err)
	}
	resumed, err := sql.Open("mysql", writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	resumeTx, err := resumed.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("解锁后写账号应能重连：%v", err)
	}
	shared := &Tx{Tx: resumeTx}
	origin := strings.Repeat("1", 32)
	if err = shared.SaveReceipt(ctx, Receipt{Origin: origin, Actor: "account:new", Key: "k1", Digest: strings.Repeat("b", 64), Kind: "operation", ResourceID: "op-1", Result: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err = resumeTx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 用写账号读回自己的回执。
	var digest string
	if err = resumed.QueryRowContext(ctx, "SELECT digest FROM shared_requests WHERE origin=? AND actor='account:new' AND request_key='k1'", origin).Scan(&digest); err != nil {
		t.Fatalf("写账号应能读回自己写入的回执：%v", err)
	}
	// 换来源用同一个键是另一个桶，不会命中。
	if _, err = store.Receipt(ctx, Receipt{Origin: strings.Repeat("2", 32), Actor: "account:new", Key: "k1"}); err != sql.ErrNoRows {
		t.Fatalf("换来源不应命中旧回执，实际 %v", err)
	}
	t.Logf("步骤 8：写账号已解锁并能写入/读回回执；换来源不命中")
}

// writeDSN 把维护账号的连接串换成写账号，只用于演练。
func writeDSN(dsn string) string {
	at := strings.Index(dsn, "@")
	if at < 0 {
		return dsn
	}
	return fmt.Sprintf("opsd_write:rehearsal-write%s", dsn[at:])
}
