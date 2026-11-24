// Package sharedsql 保存 OPSD 共享管理状态，不执行宿主机或远程操作。
package sharedsql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const SchemaVersion = 2
const MaxConnections = 2
const MonitorConnections = 4

var ErrConflict = errors.New("共享状态已经变化")

// ErrOutcomeUnknown 表示命中了迁移前的旧回执：归属无法确认，
// 只能阻止重复执行，原结果需凭操作标识经授权查询。
var ErrOutcomeUnknown = errors.New("旧回执归属无法确认，请按操作标识查询原结果")

// ErrResourceBusy 表示同一资源已有未结束的流程；调用方应映射成冲突，而不是依赖故障。
var ErrResourceBusy = errors.New("该资源已有未结束流程")
var ErrUnavailable = errors.New("共享数据库当前不可写")
var ErrCommitUnknown = errors.New("共享事务提交结果未知，请查询原请求")

type Store struct {
	DB      *sql.DB
	Cluster string
}
type Object struct {
	Kind, ID, Identity string
	Revision           uint64
	Body               json.RawMessage
}
type Flow struct {
	ID          string          `json:"id"`
	ResourceID  string          `json:"resource_id"`
	// ResourceKey 是比 resource_id 更细的资源标识（如 Galera 身份 + 库名）；
	// 为空表示该流程不参与细粒度互斥。
	ResourceKey string          `json:"resource_key,omitempty"`
	Coordinator string          `json:"coordinator"`
	Epoch       uint64          `json:"epoch"`
	Revision    uint64          `json:"revision"`
	Terminal    bool            `json:"terminal"`
	Body        json.RawMessage `json:"body"`
}
type Receipt struct {
	// Origin 是写入时的**已认证来源**；声明操作者只区分来源内的请求。
	Origin             string
	Actor, Key, Digest string
	Kind, ResourceID   string
	Result             json.RawMessage
	CreatedAt          time.Time
}
type Tx struct{ *sql.Tx }

// Open 只连接本机代理；结构迁移由维护入口以独立账号完成。
func Open(ctx context.Context, address, user, password, database, cluster string) (*Store, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("共享数据库须连接本机 ProxySQL")
	}
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = address
	c.User = user
	c.Passwd = password
	c.DBName = database
	c.ParseTime = true
	c.Loc = time.UTC
	c.Timeout = 3 * time.Second
	c.ReadTimeout = 5 * time.Second
	c.WriteTimeout = 5 * time.Second
	c.RejectReadOnly = true
	c.Params = map[string]string{"wsrep_sync_wait": "1", "time_zone": "'+00:00'"}
	connector, err := mysql.NewConnector(c)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(MaxConnections)
	db.SetMaxIdleConns(MaxConnections)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	s := &Store{DB: db, Cluster: cluster}
	if _, err = s.Phase(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS shared_metadata(id TINYINT PRIMARY KEY,cluster_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,schema_version INT NOT NULL,phase VARCHAR(16) NOT NULL,write_version BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_objects(kind VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,id VARCHAR(192) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,identity_key VARCHAR(192) CHARACTER SET ascii COLLATE ascii_bin NULL,revision BIGINT UNSIGNED NOT NULL,body MEDIUMBLOB NOT NULL,PRIMARY KEY(kind,id),UNIQUE KEY identity_owner(kind,identity_key)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_requests(origin VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,actor VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,request_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,digest CHAR(64) CHARACTER SET ascii NOT NULL,kind VARCHAR(32) CHARACTER SET ascii NOT NULL,resource_id VARCHAR(192) CHARACTER SET ascii NOT NULL,result MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(origin,actor,request_key)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_flows(id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,resource_key VARCHAR(192) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',coordinator CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,epoch BIGINT UNSIGNED NOT NULL,revision BIGINT UNSIGNED NOT NULL,lease_until DATETIME(6) NOT NULL,terminal BOOL NOT NULL,body MEDIUMBLOB NOT NULL,KEY active_flows(terminal,lease_until),KEY resource_flows(resource_id,terminal),KEY resource_key_flows(resource_key,terminal)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_records(id VARCHAR(384) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,owner CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,kind VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,object_id VARCHAR(192) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,sequence BIGINT UNSIGNED NOT NULL,created_at DATETIME(6) NOT NULL,body MEDIUMBLOB NOT NULL,digest BINARY(32) NOT NULL,UNIQUE KEY source_sequence(owner,sequence),KEY latest_object(owner,kind,object_id,sequence),KEY history_time(kind,created_at,owner)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_watermarks(owner CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,sequence BIGINT UNSIGNED NOT NULL) ENGINE=InnoDB`,
}

func Initialize(ctx context.Context, db *sql.DB, cluster string) error {
	for _, q := range schema {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO shared_metadata(id,cluster_id,schema_version,phase) VALUES(1,?,?,'preparing')", cluster, SchemaVersion); err != nil {
		return err
	}
	s := &Store{DB: db, Cluster: cluster}
	_, err := s.Phase(ctx)
	return err
}
func (s *Store) Phase(ctx context.Context) (string, error) {
	var cluster, phase string
	var version int
	err := s.DB.QueryRowContext(ctx, "SELECT cluster_id,schema_version,phase FROM shared_metadata WHERE id=1").Scan(&cluster, &version, &phase)
	if err != nil {
		return "", err
	}
	if cluster != s.Cluster || version != SchemaVersion {
		return "", fmt.Errorf("共享数据库身份或版本不匹配")
	}
	return phase, nil
}
func (s *Store) Transact(ctx context.Context, node string, preparing bool, fn func(*Tx) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var phase, cluster string
		var version int
		err = tx.QueryRowContext(ctx, "SELECT cluster_id,schema_version,phase FROM shared_metadata WHERE id=1 FOR UPDATE").Scan(&cluster, &version, &phase)
		if err == nil && (cluster != s.Cluster || version != SchemaVersion || phase != "active" && !preparing) {
			err = ErrUnavailable
		}
		if err == nil && node != "" {
			err = authorize(ctx, tx, node)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE shared_metadata SET write_version=write_version+1 WHERE id=1")
		}
		if err == nil {
			err = fn(&Tx{tx})
		}
		if err != nil {
			tx.Rollback()
			var server *mysql.MySQLError
			if errors.As(err, &server) && (server.Number == 1213 || server.Number == 1205) {
				continue
			}
			if errors.As(err, &server) && server.Number == 1062 {
				return ErrConflict
			}
			return err
		}
		if err = tx.Commit(); err != nil {
			var server *mysql.MySQLError
			if errors.As(err, &server) && server.Number == 1213 {
				continue
			}
			return fmt.Errorf("%w：%v", ErrCommitUnknown, err)
		}
		return nil
	}
	return ErrConflict
}
func authorize(ctx context.Context, tx *sql.Tx, node string) error {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM shared_objects m WHERE m.kind='member' AND m.id=? AND NOT EXISTS(SELECT 1 FROM shared_objects r WHERE r.kind='revocation' AND r.id=m.id)", node).Scan(&n)
	if err == nil && n != 1 {
		return fmt.Errorf("共享管理权限已撤销")
	}
	return err
}
func (t *Tx) Object(ctx context.Context, kind, id string) (Object, error) {
	return object(ctx, t.Tx, kind, id)
}

type rowQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func object(ctx context.Context, q rowQuery, kind, id string) (Object, error) {
	o := Object{Kind: kind, ID: id}
	var identity sql.NullString
	err := q.QueryRowContext(ctx, "SELECT revision,identity_key,body FROM shared_objects WHERE kind=? AND id=?", kind, id).Scan(&o.Revision, &identity, &o.Body)
	o.Identity = identity.String
	return o, err
}
func (s *Store) Object(ctx context.Context, kind, id string) (Object, error) {
	return object(ctx, s.DB, kind, id)
}
func (s *Store) Objects(ctx context.Context, kind string) ([]Object, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,revision,COALESCE(identity_key,''),body FROM shared_objects WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Object{}
	for rows.Next() {
		o := Object{Kind: kind}
		if err = rows.Scan(&o.ID, &o.Revision, &o.Identity, &o.Body); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (t *Tx) Put(ctx context.Context, o Object, expected uint64) error {
	var identity any
	if o.Identity != "" {
		identity = o.Identity
	}
	if expected == 0 {
		_, err := t.ExecContext(ctx, "INSERT INTO shared_objects(kind,id,identity_key,revision,body) VALUES(?,?,?,?,?)", o.Kind, o.ID, identity, o.Revision, []byte(o.Body))
		return err
	}
	r, err := t.ExecContext(ctx, "UPDATE shared_objects SET identity_key=?,revision=?,body=? WHERE kind=? AND id=? AND revision=?", identity, o.Revision, []byte(o.Body), o.Kind, o.ID, expected)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (t *Tx) Import(ctx context.Context, o Object) error {
	old, err := t.Object(ctx, o.Kind, o.ID)
	if err == sql.ErrNoRows {
		return t.Put(ctx, o, 0)
	}
	if err != nil {
		return err
	}
	if old.Revision > o.Revision {
		return nil
	}
	if old.Revision == o.Revision {
		if !bytes.Equal(old.Body, o.Body) {
			return fmt.Errorf("同一对象版本内容冲突：%s/%s", o.Kind, o.ID)
		}
		return nil
	}
	return t.Put(ctx, o, old.Revision)
}
func (t *Tx) Receipt(ctx context.Context, r Receipt) (Receipt, error) {
	var digest string
	err := t.QueryRowContext(ctx, "SELECT digest,kind,resource_id,result,created_at FROM shared_requests WHERE origin=? AND actor=? AND request_key=?", r.Origin, r.Actor, r.Key).Scan(&digest, &r.Kind, &r.ResourceID, &r.Result, &r.CreatedAt)
	if err == nil && r.Digest != "" && digest != r.Digest {
		return r, ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, legacyReceipt(ctx, t, r)
	}
	return r, err
}
func (t *Tx) SaveReceipt(ctx context.Context, r Receipt) error {
	var at any
	if !r.CreatedAt.IsZero() {
		at = r.CreatedAt.UTC()
	}
	_, err := t.ExecContext(ctx, "INSERT INTO shared_requests(origin,actor,request_key,digest,kind,resource_id,result,created_at) VALUES(?,?,?,?,?,?,?,COALESCE(?,CURRENT_TIMESTAMP(6)))", r.Origin, r.Actor, r.Key, r.Digest, r.Kind, r.ResourceID, []byte(r.Result), at)
	return err
}
func (s *Store) Receipt(ctx context.Context, r Receipt) (Receipt, error) {
	var digest string
	err := s.DB.QueryRowContext(ctx, "SELECT digest,kind,resource_id,result,created_at FROM shared_requests WHERE origin=? AND actor=? AND request_key=?", r.Origin, r.Actor, r.Key).Scan(&digest, &r.Kind, &r.ResourceID, &r.Result, &r.CreatedAt)
	if err == nil && r.Digest != "" && digest != r.Digest {
		return r, ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, legacyReceipt(ctx, s.DB, r)
	}
	return r, err
}

// legacyReceipt 只判断“这条旧键是否已经用过”，不返回旧结果。
func legacyReceipt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, r Receipt) error {
	var exists int
	err := q.QueryRowContext(ctx, "SELECT 1 FROM shared_requests WHERE origin='' AND actor=? AND request_key=?", r.Actor, r.Key).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return ErrOutcomeUnknown
}
func (t *Tx) CreateFlow(ctx context.Context, f Flow) error {
	var count int
	if err := t.QueryRowContext(ctx, "SELECT count(*) FROM shared_flows WHERE resource_id=? AND terminal=0", f.ResourceID).Scan(&count); err != nil {
		return err
	}
	if count != 0 && !f.Terminal {
		return ErrResourceBusy
	}
	// 资源键比集群粒度更细：同一集群里不同库名互不影响，同名库才互斥。
	if f.ResourceKey != "" && !f.Terminal {
		var occupied int
		if err := t.QueryRowContext(ctx, "SELECT count(*) FROM shared_flows WHERE resource_key=? AND terminal=0", f.ResourceKey).Scan(&occupied); err != nil {
			return err
		}
		if occupied != 0 {
			return ErrResourceBusy
		}
	}
	_, err := t.ExecContext(ctx, "INSERT INTO shared_flows(id,resource_id,resource_key,coordinator,epoch,revision,lease_until,terminal,body) VALUES(?,?,?,?,?,?,TIMESTAMPADD(SECOND,15,CURRENT_TIMESTAMP(6)),?,?)", f.ID, f.ResourceID, f.ResourceKey, f.Coordinator, f.Epoch, f.Revision, f.Terminal, []byte(f.Body))
	return err
}
func (s *Store) Flow(ctx context.Context, id string) (Flow, error) {
	return flow(ctx, s.DB, id)
}
func (t *Tx) Flow(ctx context.Context, id string) (Flow, error) {
	return flow(ctx, t.Tx, id)
}
func flow(ctx context.Context, q rowQuery, id string) (Flow, error) {
	var f Flow
	err := q.QueryRowContext(ctx, "SELECT id,resource_id,coordinator,epoch,revision,terminal,body FROM shared_flows WHERE id=?", id).Scan(&f.ID, &f.ResourceID, &f.Coordinator, &f.Epoch, &f.Revision, &f.Terminal, &f.Body)
	return f, err
}
func (s *Store) Flows(ctx context.Context) ([]Flow, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,resource_id,coordinator,epoch,revision,terminal,body FROM shared_flows WHERE terminal=0 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Flow{}
	for rows.Next() {
		var f Flow
		if err = rows.Scan(&f.ID, &f.ResourceID, &f.Coordinator, &f.Epoch, &f.Revision, &f.Terminal, &f.Body); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (t *Tx) SaveFlow(ctx context.Context, f Flow, expected uint64) error {
	r, err := t.ExecContext(ctx, "UPDATE shared_flows SET revision=?,terminal=?,body=?,lease_until=TIMESTAMPADD(SECOND,15,CURRENT_TIMESTAMP(6)) WHERE id=? AND coordinator=? AND epoch=? AND revision=? AND lease_until>CURRENT_TIMESTAMP(6)", f.Revision, f.Terminal, []byte(f.Body), f.ID, f.Coordinator, f.Epoch, expected)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (s *Store) Claim(ctx context.Context, id, node string) error {
	return s.Transact(ctx, node, false, func(t *Tx) error {
		candidate, err := t.Object(ctx, "candidate", node)
		if err != nil || string(candidate.Body) != "true" {
			return fmt.Errorf("节点尚未登记为协调候选")
		}
		r, err := t.ExecContext(ctx, "UPDATE shared_flows SET epoch=epoch+IF(coordinator=?,0,1),coordinator=?,lease_until=TIMESTAMPADD(SECOND,15,CURRENT_TIMESTAMP(6)) WHERE id=? AND terminal=0 AND (coordinator=? OR lease_until<=CURRENT_TIMESTAMP(6))", node, node, id, node)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err == nil && n != 1 {
			return ErrConflict
		}
		return err
	})
}
func (s *Store) CheckFlow(ctx context.Context, id, node string, epoch uint64) error {
	return checkFlow(ctx, s.DB, id, node, epoch)
}
func (t *Tx) CheckFlow(ctx context.Context, id, node string, epoch uint64) error {
	return checkFlow(ctx, t.Tx, id, node, epoch)
}
func checkFlow(ctx context.Context, q rowQuery, id, node string, epoch uint64) error {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM shared_flows f WHERE f.id=? AND f.coordinator=? AND f.epoch=? AND f.terminal=0 AND f.lease_until>CURRENT_TIMESTAMP(6) AND NOT EXISTS(SELECT 1 FROM shared_objects r WHERE r.kind='revocation' AND r.id=?)", id, node, epoch, node).Scan(&n)
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (s *Store) PutRecord(ctx context.Context, id, owner, kind, object string, seq uint64, at time.Time, body []byte) error {
	hash := sha256.Sum256(body)
	return s.Transact(ctx, "", true, func(t *Tx) error {
		var prior []byte
		err := t.QueryRowContext(ctx, "SELECT digest FROM shared_records WHERE id=?", id).Scan(&prior)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return fmt.Errorf("同一记录内容冲突：%s", id)
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err = t.ExecContext(ctx, "INSERT INTO shared_records(id,owner,kind,object_id,sequence,created_at,body,digest) VALUES(?,?,?,?,?,?,?,?)", id, owner, kind, object, seq, at.UTC(), body, hash[:]); err != nil {
			return err
		}
		_, err = t.ExecContext(ctx, "INSERT INTO shared_watermarks(owner,sequence) VALUES(?,?) ON DUPLICATE KEY UPDATE sequence=GREATEST(sequence,VALUES(sequence))", owner, seq)
		return err
	})
}
func (s *Store) Records(ctx context.Context, owner, kind, object, after string, limit int) ([]json.RawMessage, error) {
	query := "SELECT body FROM shared_records WHERE owner=? AND kind=? AND id>?"
	args := []any{owner, kind, after}
	if object != "" {
		query += " AND object_id=?"
		args = append(args, object)
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var body json.RawMessage
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, rows.Err()
}
func (s *Store) Latest(ctx context.Context, owner, kind, object string) (json.RawMessage, error) {
	var body json.RawMessage
	err := s.DB.QueryRowContext(ctx, "SELECT body FROM shared_records WHERE owner=? AND kind=? AND object_id=? ORDER BY sequence DESC LIMIT 1", owner, kind, object).Scan(&body)
	return body, err
}

func (s *Store) PruneMetrics(ctx context.Context) error {
	var expired int
	err := s.DB.QueryRowContext(ctx, "SELECT 1 FROM shared_records WHERE kind='metrics' AND created_at<UTC_DATE()-INTERVAL 7 DAY LIMIT 1").Scan(&expired)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Transact(ctx, "", false, func(t *Tx) error {
		_, err := t.ExecContext(ctx, "DELETE FROM shared_records WHERE kind='metrics' AND created_at<UTC_DATE()-INTERVAL 7 DAY LIMIT 200")
		return err
	})
}

// Migrate 把 v1 的共享库升到 v2。
//
// 只能在**停写窗口**内由独立迁移账号执行：先阻断旧写入方的新连接、排空存量会话
// 与未结束事务，再执行这里的变更，最后核对结构并发布新版本号。
// ALTER TABLE 会隐式提交，因此这些语句不能包在一个长事务里假装原子。
//
// 顺序上有一点必须守住：**先核对、先处理旧在途流程，最后才发布 v2**。
// 一旦版本号写上去，旧写入方会被拒绝，而新版还没准备好就再也回不去了。
func Migrate(ctx context.Context, db *sql.DB, cluster string) error {
	var version int
	var current string
	if err := db.QueryRowContext(ctx, "SELECT cluster_id,schema_version FROM shared_metadata WHERE id=1").Scan(&current, &version); err != nil {
		return err
	}
	if current != cluster {
		return fmt.Errorf("共享数据库身份不匹配")
	}
	if version == SchemaVersion {
		return nil
	}
	if version != 1 {
		return fmt.Errorf("共享数据库版本 %d 无法直接迁移到 %d", version, SchemaVersion)
	}
	// 每一步都先看实际结构再决定做不做：中断后重跑时，已经完成的步骤会跳过，
	// 而不是在重复列或重复主键上直接失败。
	if err := ensureColumn(ctx, db, "shared_requests", "origin",
		"VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 主键变更必须一条 ALTER 完成：拆成 DROP 再 ADD 若中途中断，
	// 表会处于**没有主键**的状态，重跑也无从判断原状。
	if err := ensurePrimaryKey(ctx, db, "shared_requests", []string{"origin", "actor", "request_key"}); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "shared_flows", "resource_key",
		"VARCHAR(192) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureIndex(ctx, db, "shared_flows", "resource_key_flows", []string{"resource_key", "terminal"}, false); err != nil {
		return err
	}
	// DDL 之后、发布之前的核对：结构不对就不要把版本写上去。
	if err := verifyV2(ctx, db); err != nil {
		return err
	}
	// 旧的在途流程没有资源键，新版无法判断库名是否被占用。
	// 停写窗口内它们本应已经结束；仍然存在就必须先排空，不能带着空键上线。
	var pending int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM shared_flows WHERE terminal=0 AND resource_key=''").Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fmt.Errorf("仍有 %d 个未结束的旧流程缺少资源键，请先排空后再发布 v2", pending)
	}
	_, err := db.ExecContext(ctx, "UPDATE shared_metadata SET schema_version=? WHERE id=1", SchemaVersion)
	return err
}

// columnDefinition 返回该列在 information_schema 里的类型定义。
func columnDefinition(ctx context.Context, db *sql.DB, table, column string) (string, error) {
	var kind, nullable string
	var length sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT DATA_TYPE, IS_NULLABLE, CHARACTER_MAXIMUM_LENGTH
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&kind, &nullable, &length)
	if err != nil {
		return "", err
	}
	definition := strings.ToUpper(kind)
	if length.Valid {
		definition += fmt.Sprintf("(%d)", length.Int64)
	}
	if nullable == "NO" {
		definition += " NOT NULL"
	}
	return definition, nil
}

// ensureColumn 缺列才加；已有同名列则什么都不做（中断重跑的常见情况）。
func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// primaryKey 返回主键列（按顺序）。
func primaryKey(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME='PRIMARY' ORDER BY SEQ_IN_INDEX`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

// ensurePrimaryKey 把主键调整成目标列序；已经是目标形状就不动。
func ensurePrimaryKey(ctx context.Context, db *sql.DB, table string, want []string) error {
	have, err := primaryKey(ctx, db, table)
	if err != nil {
		return err
	}
	if slices.Equal(have, want) {
		return nil
	}
	// 一条语句完成替换，任何时刻都不会留下“没有主键”的中间状态。
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY, ADD PRIMARY KEY(%s)", table, strings.Join(want, ",")))
	return err
}

// ensureIndex 缺索引才建；已有同名列索引则跳过。
func ensureIndex(ctx context.Context, db *sql.DB, table, name string, columns []string, unique bool) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?`, table, name).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD %s %s(%s)", table, kind, name, strings.Join(columns, ",")))
	return err
}

// verifyV2 核对 v2 的结构：列、主键列序与索引都在，才允许发布版本号。
func verifyV2(ctx context.Context, db *sql.DB) error {
	hasColumn := func(table, column string) error {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("结构核对失败：缺少 %s.%s", table, column)
		}
		return nil
	}
	for _, c := range []struct{ table, column string }{
		{"shared_requests", "origin"},
		{"shared_flows", "resource_key"},
	} {
		if err := hasColumn(c.table, c.column); err != nil {
			return err
		}
	}
	// 主键要按**完整列序**核对：只有首列对不算迁移完成。
	have, err := primaryKey(ctx, db, "shared_requests")
	if err != nil {
		return err
	}
	if want := []string{"origin", "actor", "request_key"}; !slices.Equal(have, want) {
		return fmt.Errorf("结构核对失败：shared_requests 主键为 %v，期望 %v", have, want)
	}
	var indexes int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='shared_flows' AND INDEX_NAME='resource_key_flows'`).Scan(&indexes); err != nil {
		return err
	}
	if indexes == 0 {
		return fmt.Errorf("结构核对失败：缺少 shared_flows.resource_key_flows")
	}
	return nil
}
