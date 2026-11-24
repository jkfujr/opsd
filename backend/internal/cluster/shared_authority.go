package cluster

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func sharedObject(kind, id string, revision uint64, value any) sharedsql.Object {
	body, _ := json.Marshal(value)
	return sharedsql.Object{Kind: kind, ID: id, Revision: revision, Body: body}
}
func (n *Node) syncSharedDirectory(ctx context.Context, s *sharedsql.Store) error {
	groups := map[string][]sharedsql.Object{}
	for _, kind := range []string{"admission", "member", "revocation", "database", "address", "trust"} {
		values, err := s.Objects(ctx, kind)
		if err != nil {
			return err
		}
		groups[kind] = values
	}
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, o := range groups["admission"] {
		if _, err = tx.ExecContext(ctx, "INSERT INTO admissions(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", o.ID, string(o.Body)); err != nil {
			return err
		}
	}
	for _, o := range groups["member"] {
		var m Member
		if err = json.Unmarshal(o.Body, &m); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO members(id,body,version,issuer) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,version=excluded.version,issuer=excluded.issuer WHERE members.version<excluded.version", m.ID, string(o.Body), m.Version, m.Issuer); err != nil {
			return err
		}
	}
	for _, o := range groups["revocation"] {
		if _, err = tx.ExecContext(ctx, "INSERT INTO revocations(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", o.ID, string(o.Body)); err != nil {
			return err
		}
	}
	for _, o := range groups["database"] {
		if _, err = tx.ExecContext(ctx, "INSERT INTO managed_database_clusters(id,body,revision) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,revision=excluded.revision WHERE managed_database_clusters.revision<=excluded.revision", o.ID, string(o.Body), o.Revision); err != nil {
			return err
		}
	}
	for _, o := range groups["address"] {
		if _, err = tx.ExecContext(ctx, "INSERT INTO public_addresses(owner,revision,body) VALUES(?,?,?) ON CONFLICT(owner) DO UPDATE SET revision=excluded.revision,body=excluded.body WHERE public_addresses.revision<excluded.revision", o.ID, o.Revision, string(o.Body)); err != nil {
			return err
		}
	}
	for _, o := range groups["trust"] {
		var policy TrustPolicy
		if err = json.Unmarshal(o.Body, &policy); err != nil {
			return err
		}
		current, e := readPolicy(ctx, tx)
		if e != nil {
			return e
		}
		if current.Version.Counter > policy.Version.Counter {
			continue
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO peer_trust_policy(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(o.Body)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	n.mu.Lock()
	for _, o := range groups["revocation"] {
		if p := n.connections[o.ID]; p != nil {
			p.channel.Close()
		}
	}
	n.mu.Unlock()
	n.signalNetwork()
	return nil
}
func (n *Node) publishSharedMember(ctx context.Context, s *sharedsql.Store) error {
	m, err := n.Self(ctx)
	if err != nil {
		return err
	}
	old, err := s.Object(ctx, "member", n.ID)
	if err == nil && old.Revision >= m.Version {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error { return t.Import(ctx, sharedObject("member", n.ID, m.Version, m)) })
}

func (n *Node) publishSharedAddress(ctx context.Context, s *sharedsql.Store) error {
	address, err := readPublic(ctx, n.DB, n.ID)
	if err != nil || address.Revision == 0 {
		return err
	}
	old, err := s.Object(ctx, "address", n.ID)
	if err == nil && old.Revision >= address.Revision {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		return t.Import(ctx, sharedObject("address", n.ID, address.Revision, address))
	})
}

func (n *Node) sharedChangeTrust(ctx context.Context, request TrustPolicyPatch) (TrustPolicyState, error) {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return TrustPolicyState{}, err
	}
	var policy TrustPolicy
	changed := false
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		policy = TrustPolicy{}
		changed = false
		old, e := t.Object(ctx, "trust", "policy")
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil {
			if e = json.Unmarshal(old.Body, &policy); e != nil {
				return e
			}
		}
		if policy.Version != request.ExpectedVersion {
			return fault("precondition_failed", "互信设置已经改变，请刷新")
		}
		if policy.Enabled == request.Enabled {
			return nil
		}
		policy = TrustPolicy{Cluster: n.cluster, TrustPolicyState: TrustPolicyState{Version: TrustVersion{Counter: policy.Version.Counter + 1, NodeID: n.ID}, Enabled: request.Enabled}}
		policy.Signature = ed25519.Sign(n.signing, trustBytes(policy))
		changed = true
		return t.Put(ctx, sharedObject("trust", "policy", policy.Version.Counter, policy), old.Revision)
	})
	if err == nil && changed {
		var tx *sql.Tx
		tx, err = n.DB.BeginTx(ctx, nil)
		if err == nil {
			err = n.appendTx(ctx, tx, "peer_trust", n.ID, policy, n.Now())
			if err == nil {
				err = tx.Commit()
			}
			tx.Rollback()
		}
	}
	if err == nil {
		err = n.syncSharedDirectory(ctx, s)
		n.signalNetwork()
		n.notifyChange(Change{Resource: "peer_trust"})
	}
	return policy.TrustPolicyState, sharedFault(err)
}

func (n *Node) syncSharedCandidate(ctx context.Context, s *sharedsql.Store) error {
	c, err := n.Config(ctx)
	if err != nil {
		return err
	}
	value := c.CoordinatorCandidate || c.Console
	old, err := s.Object(ctx, "candidate", n.ID)
	body, _ := json.Marshal(value)
	if err == nil && string(old.Body) == string(body) {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		current, e := t.Object(ctx, "candidate", n.ID)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		return t.Put(ctx, sharedsql.Object{Kind: "candidate", ID: n.ID, Revision: current.Revision + 1, Body: body}, current.Revision)
	})
}
func (n *Node) sharedToken(ctx context.Context) (string, string, time.Time, error) {
	s, err := n.sharedStore(ctx)
	expires := n.Now().Add(24 * time.Hour).UTC()
	if err != nil {
		return "", "", expires, err
	}
	token, id := identity.Random(), identity.Random()
	value := struct {
		Hash    string    `json:"hash"`
		Expires time.Time `json:"expires"`
		Member  string    `json:"member,omitempty"`
	}{Hash: hex.EncodeToString(Digest(token)), Expires: expires}
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if w := requeststate.Current(ctx); w != nil {
			prior, e := t.Receipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest})
			if e == nil {
				var result struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(prior.Result, &result)
				id = result.ID
				return fault("secret_unavailable", "原接入码已生成，请查询原请求")
			}
			if e != sql.ErrNoRows {
				return e
			}
			body, _ := json.Marshal(map[string]any{"id": id, "expires_at": expires})
			if e = t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "enrollment", ResourceID: id, Result: body}); e != nil {
				return e
			}
		}
		return t.Put(ctx, sharedObject("enrollment", hex.EncodeToString(Digest(token)), 1, value), 0)
	})
	return token, id, expires, sharedFault(err)
}
func (n *Node) sharedAcceptMember(ctx context.Context, m Member, token string) error {
	if m.ManagementVersion < 1 {
		return fault("unsupported", "节点版本不支持共享管理")
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if _, e := t.Object(ctx, "revocation", m.ID); e == nil {
			return fault("forbidden", "节点已撤销")
		} else if e != sql.ErrNoRows {
			return e
		}
		if prior, e := t.Object(ctx, "member", m.ID); e == nil {
			var old Member
			if json.Unmarshal(prior.Body, &old) != nil || old.Public != m.Public || old.Signing != m.Signing || old.Recipient != m.Recipient {
				return fault("forbidden", "节点身份不匹配")
			}
			return nil
		} else if e != sql.ErrNoRows {
			return e
		}
		o, e := t.Object(ctx, "enrollment", hex.EncodeToString(Digest(token)))
		if e != nil {
			return fault("forbidden", "接入码无效")
		}
		var enrollment struct {
			Hash    string    `json:"hash"`
			Expires time.Time `json:"expires"`
			Member  string    `json:"member,omitempty"`
		}
		if json.Unmarshal(o.Body, &enrollment) != nil || enrollment.Member != "" || !enrollment.Expires.After(n.Now()) {
			return fault("forbidden", "接入码已消费或已过期")
		}
		enrollment.Member = m.ID
		if e = t.Put(ctx, sharedObject(o.Kind, o.ID, o.Revision+1, enrollment), o.Revision); e != nil {
			return e
		}
		m.Cluster = n.cluster
		m.Issuer = n.ID
		m.Version = 1
		m.Signature = ed25519.Sign(n.signing, memberBytes(m))
		if e = t.Put(ctx, sharedObject("admission", m.ID, 1, m), 0); e != nil {
			return e
		}
		return t.Put(ctx, sharedObject("member", m.ID, 1, m), 0)
	})
	if err == nil {
		err = n.syncSharedDirectory(ctx, s)
		n.notify()
	}
	return err
}
func (n *Node) sharedRevoke(ctx context.Context, id string, expected *uint64) error {
	if id == n.ID {
		return fault("invalid_request", "不能撤销当前节点")
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	r := Revocation{ID: id, Issuer: n.ID, Cluster: n.cluster}
	r.Signature = ed25519.Sign(n.signing, revocationBytes(r))
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if w := requeststate.Current(ctx); w != nil {
			_, e := t.Receipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest})
			if e == nil {
				return nil
			}
			if e != sql.ErrNoRows {
				return e
			}
		}
		m, e := t.Object(ctx, "member", id)
		if e != nil {
			return e
		}
		var member Member
		if e = json.Unmarshal(m.Body, &member); e != nil {
			return e
		}
		if expected != nil && member.ConfigurationRevision != *expected {
			return sharedsql.ErrConflict
		}
		if _, e = t.Object(ctx, "revocation", id); e == sql.ErrNoRows {
			if e = t.Put(ctx, sharedObject("revocation", id, 1, r), 0); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if w := requeststate.Current(ctx); w != nil {
			body, _ := json.Marshal(map[string]string{"id": id})
			return t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "node", ResourceID: id, Result: body})
		}
		return nil
	})
	if err == nil {
		err = n.syncSharedDirectory(ctx, s)
		n.notify()
	}
	return err
}
func (n *Node) sharedSaveDatabase(ctx context.Context, c ManagedDatabaseCluster) (ManagedDatabaseCluster, error) {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return c, err
	}
	original := c
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		c = original
		if w := requeststate.Current(ctx); w != nil {
			prior, e := t.Receipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest})
			if e == nil {
				return json.Unmarshal(prior.Result, &c)
			}
			if e != sql.ErrNoRows {
				return e
			}
		}
		old, e := t.Object(ctx, "database", c.ID)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if old.Revision != c.Revision {
			return sharedsql.ErrConflict
		}
		if requeststate.Current(ctx) != nil {
			var active int
			if e = t.QueryRowContext(ctx, "SELECT COUNT(*) FROM shared_flows WHERE resource_id=? AND terminal=0", c.ID).Scan(&active); e != nil {
				return e
			}
			if active != 0 {
				return fault("node_busy", "数据库仍有未结束流程")
			}
		}
		if e == nil {
			var previous ManagedDatabaseCluster
			if e = json.Unmarshal(old.Body, &previous); e != nil {
				return e
			}
			if previous.Coordinator != n.ID || c.Coordinator != previous.Coordinator {
				return fault("forbidden", "请先转交数据库协调权")
			}
			c.CoordinatorEpoch = previous.CoordinatorEpoch
		}
		if c.CoordinatorEpoch == 0 {
			c.CoordinatorEpoch = 1
		}
		sharedMemberPolicy(&c)
		c.Revision++
		c.UpdatedAt = n.Now().UTC()
		c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
		o := sharedObject("database", c.ID, c.Revision, c)
		if !c.Deleted {
			o.Identity = c.ClusterID
		}
		if state := n.sharedState(); state != nil && c.ID == state.Binding.ClusterID && c.Deleted {
			return fault("forbidden", "OPSD 数据库绑定正在使用")
		}
		if e = t.Put(ctx, o, old.Revision); e != nil {
			return e
		}
		if w := requeststate.Current(ctx); w != nil {
			return t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "database", ResourceID: c.ID, Result: o.Body})
		}
		return nil
	})
	if err == nil {
		body, _ := json.Marshal(c)
		_, err = n.DB.ExecContext(ctx, "INSERT INTO managed_database_clusters(id,body,revision) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,revision=excluded.revision", c.ID, string(body), c.Revision)
		n.notifyChange(Change{Resource: "databases"})
	}
	return c, sharedFault(err)
}
func (n *Node) checkSharedRecovery(ctx context.Context, s *sharedsql.Store) error {
	var high uint64
	err := s.DB.QueryRowContext(ctx, "SELECT sequence FROM shared_watermarks WHERE owner=?", n.ID).Scan(&high)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var local uint64
	if err = n.DB.QueryRowContext(ctx, "SELECT sequence FROM node_identity WHERE id=1").Scan(&local); err != nil {
		return err
	}
	if local < high {
		return fmt.Errorf("本机记录序号 %d 低于共享版本 %d，请先执行 opsd data recover", local, high)
	}
	return nil
}
func (n *Node) uploadShared(ctx context.Context, s *sharedsql.Store) error {
	n.sharedMu.Lock()
	waiting := n.sharedLocal != nil && n.sharedLocal.Phase == "active" && !n.sharedRecovered
	n.sharedMu.Unlock()
	if waiting {
		return fault("dependency_unavailable", "正在核对共享版本与原操作结果")
	}
	if n.Revoked(ctx, n.ID) {
		return fault("forbidden", "节点已撤销")
	}
	rows, err := n.DB.QueryContext(ctx, "SELECT body FROM records WHERE uploaded=0 ORDER BY sequence LIMIT 100")
	if err != nil {
		return err
	}
	var records []Record
	for rows.Next() {
		var body []byte
		var r Record
		if err = rows.Scan(&body); err != nil {
			break
		}
		if err = json.Unmarshal(body, &r); err != nil {
			break
		}
		records = append(records, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		body, _ := json.Marshal(r)
		if err = s.PutRecord(ctx, r.ID, r.Owner, r.Kind, r.Object, r.Sequence, r.Created, body); err != nil {
			return err
		}
		if _, err = n.DB.ExecContext(ctx, "UPDATE records SET uploaded=1 WHERE id=?", r.ID); err != nil {
			return err
		}
	}
	if n.sharedActive() {
		return s.PruneMetrics(ctx)
	}
	return nil
}
func (n *Node) sharedWriteAllowed(ctx context.Context) error {
	if !n.sharedEnabled() {
		return nil
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	phase, err := s.Phase(ctx)
	if err != nil {
		return err
	}
	if phase != "active" {
		return sharedsql.ErrUnavailable
	}
	return nil
}
func sharedFault(err error) error {
	var f *Fault
	if errors.As(err, &f) {
		return err
	}
	if errors.Is(err, sharedsql.ErrCommitUnknown) {
		return fault("outcome_unknown", err.Error())
	}
	if errors.Is(err, sharedsql.ErrConflict) {
		return fault("conflict", err.Error())
	}
	// 迁移前的旧回执无法确认归属：只阻止重复执行，不返回旧结果。
	if errors.Is(err, sharedsql.ErrOutcomeUnknown) {
		return fault("outcome_unknown", err.Error())
	}
	if err != nil {
		return fault("dependency_unavailable", err.Error())
	}
	return nil
}
