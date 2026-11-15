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
	"time"

	"github.com/go-sql-driver/mysql"
)

const SchemaVersion = 1
const MaxConnections = 2
const MonitorConnections = 4

var ErrConflict = errors.New("共享状态已经变化")
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
	Coordinator string          `json:"coordinator"`
	Epoch       uint64          `json:"epoch"`
	Revision    uint64          `json:"revision"`
	Terminal    bool            `json:"terminal"`
	Body        json.RawMessage `json:"body"`
}
type Receipt struct {
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
	c.Params = map[string]string{"wsrep_sync_wait": "1"}
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
	`CREATE TABLE IF NOT EXISTS shared_requests(actor VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,request_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,digest CHAR(64) CHARACTER SET ascii NOT NULL,kind VARCHAR(32) CHARACTER SET ascii NOT NULL,resource_id VARCHAR(192) CHARACTER SET ascii NOT NULL,result MEDIUMBLOB NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(actor,request_key)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS shared_flows(id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,coordinator CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,epoch BIGINT UNSIGNED NOT NULL,revision BIGINT UNSIGNED NOT NULL,lease_until DATETIME(6) NOT NULL,terminal BOOL NOT NULL,body MEDIUMBLOB NOT NULL,KEY active_flows(terminal,lease_until),KEY resource_flows(resource_id,terminal)) ENGINE=InnoDB`,
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
	err := t.QueryRowContext(ctx, "SELECT digest,kind,resource_id,result,created_at FROM shared_requests WHERE actor=? AND request_key=?", r.Actor, r.Key).Scan(&digest, &r.Kind, &r.ResourceID, &r.Result, &r.CreatedAt)
	if err == nil && r.Digest != "" && digest != r.Digest {
		return r, ErrConflict
	}
	return r, err
}
func (t *Tx) SaveReceipt(ctx context.Context, r Receipt) error {
	var at any
	if !r.CreatedAt.IsZero() {
		at = r.CreatedAt.UTC()
	}
	_, err := t.ExecContext(ctx, "INSERT INTO shared_requests(actor,request_key,digest,kind,resource_id,result,created_at) VALUES(?,?,?,?,?,?,COALESCE(?,CURRENT_TIMESTAMP(6)))", r.Actor, r.Key, r.Digest, r.Kind, r.ResourceID, []byte(r.Result), at)
	return err
}
func (s *Store) Receipt(ctx context.Context, r Receipt) (Receipt, error) {
	var digest string
	err := s.DB.QueryRowContext(ctx, "SELECT digest,kind,resource_id,result,created_at FROM shared_requests WHERE actor=? AND request_key=?", r.Actor, r.Key).Scan(&digest, &r.Kind, &r.ResourceID, &r.Result, &r.CreatedAt)
	if err == nil && r.Digest != "" && digest != r.Digest {
		return r, ErrConflict
	}
	return r, err
}
func (t *Tx) CreateFlow(ctx context.Context, f Flow) error {
	var count int
	if err := t.QueryRowContext(ctx, "SELECT count(*) FROM shared_flows WHERE resource_id=? AND terminal=0", f.ResourceID).Scan(&count); err != nil {
		return err
	}
	if count != 0 && !f.Terminal {
		return fmt.Errorf("该数据库已有未结束流程")
	}
	_, err := t.ExecContext(ctx, "INSERT INTO shared_flows(id,resource_id,coordinator,epoch,revision,lease_until,terminal,body) VALUES(?,?,?,?,?,TIMESTAMPADD(SECOND,15,CURRENT_TIMESTAMP(6)),?,?)", f.ID, f.ResourceID, f.Coordinator, f.Epoch, f.Revision, f.Terminal, []byte(f.Body))
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
