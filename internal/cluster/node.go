package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"filippo.io/age"
	"github.com/jkfujr/opsd/internal/account"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/protocol"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Options struct {
	Config                       Config
	Username, Password, Entrance string
	Executor                     Executor
	Now                          func() time.Time
}
type Node struct {
	DB            *sql.DB
	ID            string
	Key           identity.Key
	signing       ed25519.PrivateKey
	age           *age.X25519Identity
	cluster       string
	Executor      Executor
	Now           func() time.Time
	mu            sync.Mutex
	connections   map[string]*peer
	dialing       map[string]chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	wake          chan struct{}
	changed       chan struct{}
	collector     monitoring.Collector
	liveMu        sync.Mutex
	live          *liveRead
	storageMu     sync.Mutex
	storage       *database
	storageError  string
	Configure     func(context.Context, Config) (func(bool), error)
	subscribers   map[chan Change]struct{}
	readings      map[string]monitoring.Reading
	pending       map[string][]Operation
	seen          map[string]time.Time
	closed        bool
	started       bool
	eventSequence atomic.Uint64
	addressMu     sync.Mutex
	networkWake   chan struct{}
	ObservePublic func(context.Context, protocol.AddressFamily) []protocol.AddressObservation
}

func Defaults(c Config) Config {
	if c.Revision == 0 {
		c.Revision = 1
	}
	if c.Name == "" {
		c.Name, _ = os.Hostname()
	}
	if c.Name == "" {
		c.Name = "OPSD"
	}
	if c.Listen == "" {
		c.Listen = ":8444"
	}
	if c.ConsoleListen == "" {
		c.ConsoleListen = ":65533"
	}
	if c.Address == "" {
		c.Address = "http://127.0.0.1:8444"
	}
	if c.IntervalMinutes == 0 {
		c.IntervalMinutes = 5
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = 7
	}
	if c.Storage.Port == 0 {
		c.Storage.Port = 5984
	}
	return c
}
func ValidateConfig(c Config) error {
	if c.Name == "" || len(c.Name) > 255 || !validAddress(c.Address) || c.IntervalMinutes < 1 || c.IntervalMinutes > 1440 || c.RetentionDays != 7 || (c.AddressMode != "" && c.AddressMode != "auto" && c.AddressMode != "manual") {
		return fault("invalid_request", "名称、地址、采集间隔或保留时间无效")
	}
	return validateStorage(c.Storage)
}
func Open(ctx context.Context, path string, o Options) (*Node, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	c := Defaults(o.Config)
	if e := ValidateConfig(c); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, e = db.ExecContext(ctx, schema); e != nil {
		db.Close()
		return nil, e
	}
	n := &Node{DB: db, Executor: o.Executor, Now: o.Now, connections: map[string]*peer{}, dialing: map[string]chan struct{}{}, wake: make(chan struct{}, 1), changed: make(chan struct{}, 1), subscribers: map[chan Change]struct{}{}}
	var private, public, signing []byte
	var ageKey string
	e = db.QueryRowContext(ctx, "SELECT cluster,private_key,public_key,signing_key,age_key FROM node_identity WHERE id=1").Scan(&n.cluster, &private, &public, &signing, &ageKey)
	if e == sql.ErrNoRows {
		n.Key, e = identity.New()
		if e != nil {
			db.Close()
			return nil, e
		}
		_, n.signing, e = ed25519.GenerateKey(rand.Reader)
		if e != nil {
			db.Close()
			return nil, e
		}
		n.age, e = age.GenerateX25519Identity()
		if e != nil {
			db.Close()
			return nil, e
		}
		n.ID = nodeID(n.Key.Public)
		n.cluster = identity.Random()
		if o.Username == "" {
			o.Username = "admin"
		}
		if o.Entrance == "" {
			o.Entrance = "/s_" + identity.Random()
		}
		if e = account.Validate(o.Username, o.Entrance); e != nil {
			db.Close()
			return nil, e
		}
		hash, err := account.Hash(o.Password)
		if err != nil {
			db.Close()
			return nil, err
		}
		secret, err := seal([]byte(o.Password), []string{n.age.Recipient().String()})
		if err != nil {
			db.Close()
			return nil, err
		}
		secretJSON, _ := json.Marshal(secret)
		configJSON, _ := json.Marshal(c)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			db.Close()
			return nil, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO node_identity(id,cluster,private_key,public_key,signing_key,age_key,config) VALUES(1,?,?,?,?,?,?)", n.cluster, n.Key.Private, n.Key.Public, []byte(n.signing), n.age.String(), string(configJSON))
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO account(id,username,password_hash,entrance,secret) VALUES(1,?,?,?,?)", o.Username, hash, o.Entrance, string(secretJSON))
		}
		if err == nil {
			err = n.publishMemberTx(ctx, tx, c)
		}
		if err == nil {
			err = n.publishAccountTx(ctx, tx)
		}
		if err == nil {
			err = n.appendTx(ctx, tx, "config", n.ID, c, n.Now())
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			db.Close()
			return nil, err
		}
	} else if e == nil {
		n.Key, e = identity.Load(private, public)
		if e == nil {
			n.age, e = age.ParseX25519Identity(ageKey)
		}
		if len(signing) != ed25519.PrivateKeySize {
			e = fmt.Errorf("签名身份损坏")
		}
		n.signing = signing
		n.ID = nodeID(public)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	n.networkWake = make(chan struct{}, 1)
	n.ctx, n.cancel = context.WithCancel(ctx)
	return n, nil
}
func (n *Node) Start() {
	n.mu.Lock()
	if n.started || n.closed {
		n.mu.Unlock()
		return
	}
	n.started = true
	n.wg.Add(5)
	n.mu.Unlock()
	go func() { defer n.wg.Done(); n.discovery() }()
	go func() { defer n.wg.Done(); n.queueLoop() }()
	go func() { defer n.wg.Done(); n.storageLoop() }()
	go func() { defer n.wg.Done(); n.firewallLoop() }()
	go func() { defer n.wg.Done(); n.networkLoop() }()
}
func (n *Node) Close() error {
	n.cancel()
	n.mu.Lock()
	n.closed = true
	for _, p := range n.connections {
		p.channel.Close()
	}
	n.mu.Unlock()
	n.wg.Wait()
	return n.DB.Close()
}
func (n *Node) Config(ctx context.Context) (Config, error) {
	var c Config
	var b string
	e := n.DB.QueryRowContext(ctx, "SELECT config FROM node_identity WHERE id=1").Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &c)
	}
	return c, e
}
func (n *Node) Changed() <-chan struct{} { return n.changed }

type Change struct {
	Resource string `json:"resource"`
	NodeID   string `json:"node_id,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

func (n *Node) notify() { n.notifyChange(Change{Resource: "nodes"}) }

func (n *Node) notifyChange(change Change) {
	select {
	case n.changed <- struct{}{}:
	default:
	}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	// 目录合并和执行进展不一定递增目标配置版本，事件单独编号。
	change.Revision = n.eventSequence.Add(1)
	n.liveMu.Lock()
	for ch := range n.subscribers {
		select {
		case ch <- change:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- Change{Resource: "all"}:
			default:
			}
		}
	}
	n.liveMu.Unlock()
}
func (n *Node) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 32)
	n.liveMu.Lock()
	n.subscribers[ch] = struct{}{}
	n.liveMu.Unlock()
	return ch, func() { n.liveMu.Lock(); delete(n.subscribers, ch); n.liveMu.Unlock() }
}
func (n *Node) Self(ctx context.Context) (Member, error) { return n.Member(ctx, n.ID) }
func (n *Node) Member(ctx context.Context, id string) (Member, error) {
	var m Member
	var body string
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM members WHERE id=?", id).Scan(&body)
	if e == nil {
		e = json.Unmarshal([]byte(body), &m)
	}
	return m, e
}
func (n *Node) Revoked(ctx context.Context, id string) bool {
	var yes int
	e := n.DB.QueryRowContext(ctx, "SELECT count(*) FROM revocations WHERE id=?", id).Scan(&yes)
	return e != nil || yes != 0
}
func (n *Node) Active(ctx context.Context, id string) bool {
	if id == n.ID {
		return true
	}
	if n.Revoked(ctx, id) || n.Revoked(ctx, n.ID) {
		return false
	}
	_, e := n.Member(ctx, id)
	return e == nil
}
func (n *Node) Directory(ctx context.Context) (Directory, error) {
	d := Directory{Members: []Member{}, Revocations: []Revocation{}}
	grants, grantErr := n.DB.QueryContext(ctx, "SELECT body FROM admissions ORDER BY id")
	if grantErr != nil {
		return d, grantErr
	}
	for grants.Next() {
		var body string
		var m Member
		if grantErr = grants.Scan(&body); grantErr != nil {
			break
		}
		if grantErr = json.Unmarshal([]byte(body), &m); grantErr != nil {
			break
		}
		d.Admissions = append(d.Admissions, m)
	}
	rowsErr := grants.Err()
	grants.Close()
	if grantErr != nil {
		return d, grantErr
	}
	if rowsErr != nil {
		return d, rowsErr
	}
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM members ORDER BY id")
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var b string
		var m Member
		if e = rows.Scan(&b); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(b), &m); e != nil {
			break
		}
		d.Members = append(d.Members, m)
	}
	err := rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	if err != nil {
		return d, err
	}
	rows, e = n.DB.QueryContext(ctx, "SELECT body FROM revocations ORDER BY id")
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var b string
		var r Revocation
		if e = rows.Scan(&b); e != nil {
			return d, e
		}
		if e = json.Unmarshal([]byte(b), &r); e != nil {
			return d, e
		}
		d.Revocations = append(d.Revocations, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	if e = n.networkDirectory(ctx, &d); e != nil {
		return d, e
	}
	return d, nil
}
func (n *Node) appendTx(ctx context.Context, tx *sql.Tx, kind, object string, data any, at time.Time) error {
	var seq uint64
	if e := tx.QueryRowContext(ctx, "UPDATE node_identity SET sequence=sequence+1 WHERE id=1 RETURNING sequence").Scan(&seq); e != nil {
		return e
	}
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	r := Record{ID: fmt.Sprintf("%s~%s~%s~%020d", n.ID, kind, object, seq), Cluster: n.cluster, Owner: n.ID, Sequence: seq, Kind: kind, Object: object, Data: b, Created: at.UTC()}
	if kind == "operation" {
		var op Operation
		if e = json.Unmarshal(b, &op); e != nil {
			return e
		}
		r.QueueSequence = op.Sequence
	}
	r.Signature = ed25519.Sign(n.signing, recordBytes(r))
	body, e := json.Marshal(r)
	if e != nil {
		return e
	}
	databaseName := "opsd_state"
	if kind == "operation" {
		databaseName = "opsd_operations"
	}
	if kind == "metrics" {
		databaseName = "opsd_metrics_" + at.UTC().Format("20060102")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO records(id,kind,object,sequence,body,database_name) VALUES(?,?,?,?,?,?)", r.ID, kind, object, seq, string(body), databaseName)
	return e
}
func (n *Node) publishMemberTx(ctx context.Context, tx *sql.Tx, c Config) error {
	var version uint64
	if e := tx.QueryRowContext(ctx, "SELECT sequence+1 FROM node_identity WHERE id=1").Scan(&version); e != nil {
		return e
	}
	m := Member{ID: n.ID, Cluster: n.cluster, Name: c.Name, Address: c.Address, Region: c.Region, Public: identity.Encode(n.Key.Public), Signing: identity.Encode(n.signing.Public().(ed25519.PublicKey)), Recipient: n.age.Recipient().String(), Version: version, Issuer: n.ID}
	if c.Storage.Enabled {
		m.Storage = storageURL(c.Storage)
	}
	m.Signature = ed25519.Sign(n.signing, memberBytes(m))
	b, _ := json.Marshal(m)
	if _, e := tx.ExecContext(ctx, "INSERT INTO admissions(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", n.ID, string(b)); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "INSERT INTO members(id,body,version,issuer) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,version=excluded.version,issuer=excluded.issuer", n.ID, string(b), version, n.ID); e != nil {
		return e
	}
	return n.appendTx(ctx, tx, "member", n.ID, m, n.Now())
}
func (n *Node) publishAccountTx(ctx context.Context, tx *sql.Tx) error {
	var a Account
	var s string
	if e := tx.QueryRowContext(ctx, "SELECT username,entrance,secret,revision FROM account WHERE id=1").Scan(&a.Username, &a.Entrance, &s, &a.Revision); e != nil {
		return e
	}
	if e := json.Unmarshal([]byte(s), &a.Secret); e != nil {
		return e
	}
	return n.appendTx(ctx, tx, "account", n.ID, a, n.Now())
}
func (n *Node) recipients(ctx context.Context) ([]string, error) {
	d, e := n.Directory(ctx)
	if e != nil {
		return nil, e
	}
	r := []string{}
	for _, m := range d.Members {
		if !n.Revoked(ctx, m.ID) {
			r = append(r, m.Recipient)
		}
	}
	return r, nil
}
func (n *Node) Verify(ctx context.Context, r Record) error {
	m, e := n.Member(ctx, r.Owner)
	if e != nil {
		return e
	}
	key, e := identity.Decode(m.Signing)
	if e != nil {
		return e
	}
	if r.Cluster != n.cluster || r.Sequence == 0 || n.Revoked(ctx, r.Owner) || !ed25519.Verify(key, recordBytes(r), r.Signature) || r.ID != fmt.Sprintf("%s~%s~%s~%020d", r.Owner, r.Kind, r.Object, r.Sequence) {
		return fault("conflict", "数据签名或来源无效")
	}
	return nil
}
