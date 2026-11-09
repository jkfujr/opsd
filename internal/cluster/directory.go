package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
)

func Digest(s string) []byte { b := sha256.Sum256([]byte(s)); return b[:] }
func (n *Node) Token(ctx context.Context) (string, string, time.Time, error) {
	token, id := identity.Random(), identity.Random()
	expires := n.Now().Add(24 * time.Hour).UTC()
	_, e := n.DB.ExecContext(ctx, "INSERT INTO enrollment_tokens(id,token_hash,created_at,expires_at) VALUES(?,?,?,?)", id, Digest(token), n.Now().Unix(), expires.Unix())
	return token, id, expires, e
}
func (n *Node) acceptMember(ctx context.Context, m Member, token string) error {
	if e := validateMember(m); e != nil {
		return e
	}
	if n.Revoked(ctx, m.ID) {
		return fault("forbidden", "节点已撤销")
	}
	if old, e := n.Member(ctx, m.ID); e == nil {
		if old.Public != m.Public || old.Signing != m.Signing || old.Recipient != m.Recipient {
			return fault("forbidden", "节点身份不匹配")
		}
		return nil
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, "UPDATE enrollment_tokens SET consumed_at=?,member_id=? WHERE token_hash=? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>?", n.Now().Unix(), m.ID, Digest(token), n.Now().Unix())
	if e != nil {
		return e
	}
	affected, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if affected != 1 {
		return fault("forbidden", "接入码无效、已消费或已过期")
	}
	m.Cluster = n.cluster
	m.Issuer = n.ID
	m.Version = 1
	m.Signature = ed25519.Sign(n.signing, memberBytes(m))
	b, _ := json.Marshal(m)
	if _, e = tx.ExecContext(ctx, "INSERT INTO admissions(id,body) VALUES(?,?)", m.ID, string(b)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO members(id,body,version,issuer) VALUES(?,?,?,?)", m.ID, string(b), m.Version, m.Issuer); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "admission", m.ID, m, n.Now()); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	n.notify()
	return nil
}
func (n *Node) merge(ctx context.Context, d Directory, bootstrap bool) error {
	if len(d.Members) > 1000 || len(d.Revocations) > 1000 {
		return fault("invalid_request", "目录过大")
	}
	current, e := n.Directory(ctx)
	if e != nil {
		return e
	}
	if !bootstrap {
		d = directoryDelta(d, directorySummary(current))
		if emptyDirectory(d) {
			return nil
		}
	}
	known := map[string]Member{}
	revoked := map[string]bool{}
	for _, m := range current.Members {
		known[m.ID] = m
	}
	for _, r := range current.Revocations {
		revoked[r.ID] = true
	}
	changed := false
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	grants := append([]Member(nil), d.Admissions...)
	for len(grants) > 0 {
		next := []Member{}
		progress := false
		for _, m := range grants {
			if m.Cluster != n.cluster {
				return fault("forbidden", "集群身份不匹配")
			}
			if e = validateMember(m); e != nil {
				return e
			}
			if old, ok := known[m.ID]; ok {
				if old.Public != m.Public || old.Signing != m.Signing || old.Recipient != m.Recipient {
					return fault("forbidden", "成员授权中的公钥发生变化")
				}
				continue
			}
			issuer, ok := known[m.Issuer]
			if !ok && !bootstrap {
				next = append(next, m)
				continue
			}
			if !bootstrap {
				key, err := identity.Decode(issuer.Signing)
				if err != nil || revoked[m.Issuer] || !ed25519.Verify(key, memberBytes(m), m.Signature) {
					return fault("forbidden", "成员授权无效")
				}
			}
			body, _ := json.Marshal(m)
			if _, e = tx.ExecContext(ctx, "INSERT INTO admissions(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", m.ID, string(body)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO members(id,body,version,issuer) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", m.ID, string(body), m.Version, m.Issuer); e != nil {
				return e
			}
			known[m.ID] = m
			progress = true
			changed = true
		}
		if len(next) > 0 && !progress {
			return fault("forbidden", "成员授权链不完整")
		}
		grants = next
	}
	remaining := append([]Member(nil), d.Members...)
	for len(remaining) > 0 {
		next := []Member{}
		progress := false
		for _, m := range remaining {
			if m.Cluster != n.cluster {
				return fault("forbidden", "集群身份不匹配")
			}
			if e = validateMember(m); e != nil {
				return e
			}
			if old, ok := known[m.ID]; ok {
				if old.Public != m.Public || old.Signing != m.Signing || old.Recipient != m.Recipient {
					return fault("forbidden", "节点公钥发生变化")
				}
				if old.Version >= m.Version {
					continue
				}
			}
			issuer, ok := known[m.Issuer]
			if !ok && !bootstrap {
				next = append(next, m)
				continue
			}
			if !bootstrap {
				key, err := identity.Decode(issuer.Signing)
				if err != nil || revoked[m.Issuer] || !ed25519.Verify(key, memberBytes(m), m.Signature) {
					return fault("forbidden", "成员签名无效")
				}
				if _, exists := known[m.ID]; exists && m.Issuer != m.ID {
					return fault("forbidden", "节点配置只能由其本人更新")
				}
			}
			if revoked[m.ID] {
				continue
			}
			b, _ := json.Marshal(m)
			if _, e = tx.ExecContext(ctx, "INSERT INTO members(id,body,version,issuer) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,version=excluded.version,issuer=excluded.issuer WHERE members.version<excluded.version", m.ID, string(b), m.Version, m.Issuer); e != nil {
				return e
			}
			known[m.ID] = m
			progress = true
			changed = true
		}
		if len(next) > 0 && !progress {
			return fault("forbidden", "成员签发者未知")
		}
		remaining = next
	}
	for _, r := range d.Revocations {
		if revoked[r.ID] {
			continue
		}
		issuer, ok := known[r.Issuer]
		key, err := identity.Decode(issuer.Signing)
		if !ok || err != nil || revoked[r.Issuer] || r.Cluster != n.cluster || !ed25519.Verify(key, revocationBytes(r), r.Signature) {
			return fault("forbidden", "撤销签名无效")
		}
		b, _ := json.Marshal(r)
		if _, e = tx.ExecContext(ctx, "INSERT INTO revocations(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", r.ID, string(b)); e != nil {
			return e
		}
		revoked[r.ID] = true
		changed = true
	}
	if e = n.mergeNetworkTx(ctx, tx, d, known, revoked); e != nil {
		return e
	}
	if e = n.mergeDatabaseClusters(ctx, tx, d.Databases, known, revoked); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if changed || len(d.Addresses) > 0 || d.Trust != nil {
		n.signalNetwork()
		n.notifyChange(Change{Resource: "public_addresses"})
		n.notifyChange(Change{Resource: "peer_trust"})
	}
	if len(d.Databases) > 0 {
		n.notifyChange(Change{Resource: "databases"})
	}
	n.mu.Lock()
	for id, p := range n.connections {
		if revoked[id] || revoked[n.ID] {
			p.channel.Close()
		}
	}
	n.mu.Unlock()
	if changed {
		n.notify()
	}
	return nil
}
func (n *Node) Revoke(ctx context.Context, id string) error {
	if id == n.ID {
		return fault("invalid_request", "不能撤销当前节点")
	}
	if _, e := n.Member(ctx, id); e != nil {
		return e
	}
	if n.Revoked(ctx, id) {
		return nil
	}
	r := Revocation{ID: id, Issuer: n.ID, Cluster: n.cluster}
	r.Signature = ed25519.Sign(n.signing, revocationBytes(r))
	b, _ := json.Marshal(r)
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO revocations(id,body) VALUES(?,?)", id, string(b)); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "revocation", id, r, n.Now()); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	n.mu.Lock()
	if p := n.connections[id]; p != nil {
		p.channel.Close()
	}
	n.mu.Unlock()
	n.notify()
	return nil
}
func (n *Node) joinDirectory(ctx context.Context, d Directory) error {
	if len(d.Members) == 0 {
		return fmt.Errorf("接入目录为空")
	}
	cluster := d.Members[0].Cluster
	var own *Member
	for i := range d.Members {
		if d.Members[i].ID == n.ID {
			own = &d.Members[i]
		}
	}
	if own == nil {
		return fmt.Errorf("接入尚未受理")
	}
	var count int
	if e := n.DB.QueryRowContext(ctx, "SELECT count(*) FROM queue").Scan(&count); e != nil {
		return e
	}
	if count != 0 {
		return fault("conflict", "已有操作的独立节点不能直接加入其他集群")
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM records", "DELETE FROM members", "DELETE FROM revocations", "DELETE FROM admissions", "DELETE FROM public_addresses", "DELETE FROM peer_trust_policy", "DELETE FROM peer_trust_target", "DELETE FROM peer_trust_status"} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE node_identity SET cluster=?,sequence=MAX(sequence,?) WHERE id=1", cluster, own.Version); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	n.cluster = cluster
	if e = n.merge(ctx, d, true); e != nil {
		return e
	}
	c, e := n.Config(ctx)
	if e != nil {
		return e
	}
	tx, e = n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = n.publishMemberTx(ctx, tx, c); e != nil {
		return e
	}
	if e = n.publishAccountTx(ctx, tx); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "config", n.ID, c, n.Now()); e != nil {
		return e
	}
	return tx.Commit()
}
func (n *Node) Rewrap(ctx context.Context) error {
	var secretJSON string
	var revision uint64
	if e := n.DB.QueryRowContext(ctx, "SELECT secret,revision FROM account WHERE id=1").Scan(&secretJSON, &revision); e != nil {
		return e
	}
	var secret Secret
	if e := json.Unmarshal([]byte(secretJSON), &secret); e != nil {
		return e
	}
	plain, e := unseal(secret, n.age)
	if e != nil {
		return e
	}
	recipients, e := n.recipients(ctx)
	if e != nil {
		return e
	}
	// 只补齐接收方；正常同步不轮换密码或反复重新封装。
	var count int
	if e = n.DB.QueryRowContext(ctx, "SELECT count(*) FROM storage_credentials WHERE id='account_recipients' AND secret=?", fmt.Sprint(recipients)).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return nil
	}
	secret, e = seal(plain, recipients)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(secret)
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, "UPDATE account SET secret=? WHERE id=1 AND revision=?", string(b), revision)
	if e != nil {
		return e
	}
	affected, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if affected != 1 {
		return nil
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO storage_credentials(id,secret) VALUES('account_recipients',?) ON CONFLICT(id) DO UPDATE SET secret=excluded.secret", fmt.Sprint(recipients)); e != nil {
		return e
	}
	if e = n.publishAccountTx(ctx, tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (n *Node) Account(ctx context.Context) (Account, error) {
	var a Account
	var b string
	e := n.DB.QueryRowContext(ctx, "SELECT username,entrance,secret,revision FROM account WHERE id=1").Scan(&a.Username, &a.Entrance, &b, &a.Revision)
	if e == nil {
		e = json.Unmarshal([]byte(b), &a.Secret)
	}
	return a, e
}
func (n *Node) ReadConfig(ctx context.Context, target string) (Config, error) {
	var c Config
	e := n.Call(ctx, target, "config.read", nil, &c)
	if e != nil {
		r, err := n.latest(ctx, target, "config", target)
		if err != nil {
			return c, err
		}
		if err = json.Unmarshal(r.Data, &c); err != nil {
			return c, err
		}
		c.Cached = true
		c.LastSyncedAt = &r.Created
	}
	if e = n.observe(ctx, "config:"+target, c.Revision); e != nil {
		return Config{}, e
	}
	return c, nil
}
func (n *Node) ReadAccount(ctx context.Context, target string) (Account, error) {
	var a Account
	e := n.Call(ctx, target, "account.read", nil, &a)
	if e != nil {
		r, err := n.latest(ctx, target, "account", target)
		if err != nil {
			return a, err
		}
		if err = json.Unmarshal(r.Data, &a); err != nil {
			return a, err
		}
		a.Cached = true
		a.LastSyncedAt = &r.Created
	}
	if e = n.observe(ctx, "account:"+target, a.Revision); e != nil {
		return Account{}, e
	}
	return a, nil
}
func (n *Node) Password(ctx context.Context, target string) (AccountChange, error) {
	a, e := n.ReadAccount(ctx, target)
	if e != nil {
		return AccountChange{}, e
	}
	plain, e := unseal(a.Secret, n.age)
	return AccountChange{Username: a.Username, Entrance: a.Entrance, Password: string(plain)}, e
}
func sqlMissing(e error) bool { return e == sql.ErrNoRows }
