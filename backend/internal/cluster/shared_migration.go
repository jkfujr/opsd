package cluster

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type SharedInitialize struct {
	ClusterID string `json:"cluster_id"`
	Database  string `json:"database"`
}
type sharedMigration struct {
	Stage   string                  `json:"stage"`
	Nodes   map[string]SharedLocal  `json:"nodes"`
	Cursors map[string]string       `json:"cursors"`
	Sources map[string][]string     `json:"sources"`
	Source  DatabaseRef             `json:"source"`
	Secrets map[string]sharedAccess `json:"secrets,omitempty"`
}
type sharedExportRequest struct {
	Source string `json:"source"`
	After  string `json:"after"`
}
type sharedExport struct {
	Directory Directory           `json:"directory"`
	Records   []Record            `json:"records"`
	Flows     []sharedsql.Flow    `json:"flows"`
	Receipts  []sharedsql.Receipt `json:"receipts,omitempty"`
	Sources   []string            `json:"sources"`
	Next      string              `json:"next"`
}

func (n *Node) StartSharedInitialization(ctx context.Context, input SharedInitialize, key string) (Operation, error) {
	if input.Database == "" {
		input.Database = protocol.SharedDatabaseName
	}
	if input.Database != protocol.SharedDatabaseName || input.ClusterID == "" {
		return Operation{}, fault("invalid_request", "OPSD 数据库名称或集群无效")
	}
	if state := n.sharedState(); state != nil && (state.Binding.ClusterID != input.ClusterID || state.Binding.Database != input.Database) {
		return Operation{}, fault("conflict", "节点已经绑定其他 OPSD 数据库")
	}
	body, _ := json.Marshal(input)
	r := Request{Kind: "shared_initialize", Key: key, Payload: body, ResourceID: input.ClusterID}
	if w := requeststate.Current(ctx); w != nil {
		r.Actor, r.InputDigest = w.Actor, w.Digest
	}
	return n.enqueue(ctx, n.ID, r)
}
func (n *Node) advanceSharedInitialization(ctx context.Context, op *Operation, plain []byte) error {
	n.sharedWorkMu.Lock()
	defer n.sharedWorkMu.Unlock()
	state := n.sharedState()
	if state == nil {
		var input SharedInitialize
		if err := json.Unmarshal(plain, &input); err != nil {
			return err
		}
		c, err := n.DatabaseCluster(ctx, input.ClusterID)
		if err != nil {
			return err
		}
		if err = n.checkSharedConnections(ctx, c); err != nil {
			return err
		}
		if c.ProxyRouting == nil {
			groups := protocol.ProxyHostgroups{Writer: 10, BackupWriter: 20, Reader: 30, Offline: 40}
			if len(c.Proxies) > 0 {
				ref := c.Proxies[0]
				var observed protocol.ProxyRoutingObservation
				if err = n.Call(ctx, ref.NodeID, "database.proxy_routing.read", DatabaseRPC{InstanceID: ref.InstanceID}, &observed); err != nil {
					return err
				}
				groups = observed.Hostgroups
			}
			c.ProxyRouting = &protocol.ProxyRoutingPolicy{Enabled: true, Automatic: true, Hostgroups: groups, Backends: []protocol.ProxyBackend{}, Retired: []protocol.ProxyEndpoint{}, Accounts: []protocol.ProxyAccountBinding{}}
			sharedMemberPolicy(&c)
		}
		d, err := n.Directory(ctx)
		if err != nil {
			return err
		}
		binding := SharedBinding{ClusterID: c.ID, GaleraID: c.ClusterID, Database: input.Database, Revision: 1, Issuer: n.ID}
		binding.Signature = ed25519.Sign(n.signing, bindingBytes(binding))
		migration := &sharedMigration{Stage: "freeze", Nodes: map[string]SharedLocal{}, Cursors: map[string]string{}, Sources: map[string][]string{}}
		dataKey, err := age.GenerateX25519Identity()
		if err != nil {
			return err
		}
		for _, m := range d.Members {
			if !n.Active(ctx, m.ID) {
				continue
			}
			if m.ManagementVersion < 1 {
				return fault("unsupported", m.Name+" 需要先更新 OPSD 程序")
			}
			migration.Nodes[m.ID] = SharedLocal{DataKey: dataKey.String(), Binding: binding, Cluster: c, Phase: "preparing", Access: Credentials{Username: protocol.SharedUsername(m.ID), Password: identity.Random()}, Monitor: Credentials{Username: protocol.SharedMonitorUser(m.ID), Password: identity.Random()}}
		}
		if len(c.Members) == 0 {
			return fmt.Errorf("数据库集群没有成员")
		}
		migration.Source = c.Members[0]
		for _, ref := range c.Members {
			if ref.NodeID == n.ID {
				migration.Source = ref
				break
			}
		}
		for id, value := range migration.Nodes {
			value.AccountSource = migration.Source
			migration.Nodes[id] = value
		}
		own := migration.Nodes[n.ID]
		own.Migration = migration
		state = &own
		if err = n.saveSharedLocal(ctx, own); err != nil {
			return err
		}
	}
	if state.Migration == nil {
		return fault("conflict", "已有 OPSD 数据库绑定，请查询迁移状态")
	}
	m := state.Migration
	ids := make([]string, 0, len(m.Nodes))
	for id := range m.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	save := func() error { return n.saveSharedLocal(ctx, *state) }
	wait := func(message string) error {
		if op.State == protocol.Applying && op.WaitingReason == message && op.Error == nil {
			return nil
		}
		op.State = protocol.Applying
		op.WaitingReason = message
		op.Error = nil
		op.Revision++
		return n.saveOperation(ctx, op, "submitted")
	}
	maintenance := func(mode, node string) (bool, error) {
		p := &protocol.SharedDatabaseAction{Mode: mode, Cluster: n.cluster, Database: state.Binding.Database, NodeID: node}
		if node != "" {
			p.PasswordHash = databaseengine.NativePassword(m.Nodes[node].Access.Password)
			p.MonitorHash = databaseengine.NativePassword(m.Nodes[node].Monitor.Password)
		}
		a := protocol.DatabaseAction{Kind: "shared_database", ClusterID: state.Binding.GaleraID, Shared: p}
		return n.ensureDatabaseStep(ctx, m.Source, stepID(m.Source.NodeID, "shared-maintenance/"+n.cluster+"/"+state.Binding.Database+"/"+mode+"/"+node), a)
	}
	switch m.Stage {
	case "freeze":
		for _, id := range ids {
			if err := n.Call(ctx, id, "shared.prepare", m.Nodes[id], nil); err != nil {
				return err
			}
		}
		m.Stage = "initialize"
		if err := save(); err != nil {
			return err
		}
	case "initialize":
		ready, err := maintenance("initialize", "")
		if err != nil {
			return err
		}
		if !ready {
			return wait("initialize_database")
		}
		m.Stage = "accounts"
		if err = save(); err != nil {
			return err
		}
	case "accounts":
		for _, id := range ids {
			ready, err := maintenance("provision", id)
			if err != nil {
				return err
			}
			if !ready {
				return wait("prepare_accounts")
			}
		}
		m.Stage = "proxies"
		if err := save(); err != nil {
			return err
		}
	case "proxies":
		for _, id := range ids {
			value := m.Nodes[id]
			var status SharedStatus
			if err := n.Call(ctx, id, "shared.prepare", value, &status); err != nil {
				return err
			}
			if !status.ProxyReady {
				return wait("prepare_node_proxy")
			}
		}
		m.Stage = "import"
		if err := save(); err != nil {
			return err
		}
	case "import":
		s, err := n.sharedStore(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, known := m.Sources[id]; !known {
				var page sharedExport
				if err = n.Call(ctx, id, "shared.export", sharedExportRequest{Source: "directory"}, &page); err != nil {
					return err
				}
				if err = n.importSharedDirectory(ctx, s, page.Directory); err != nil {
					return err
				}
				m.Sources[id] = append([]string{"records", "flows"}, page.Sources...)
				if err = save(); err != nil {
					return err
				}
			}
			for _, source := range m.Sources[id] {
				key := id + "/" + source
				cursor := m.Cursors[key]
				if cursor == "complete" {
					continue
				}
				var page sharedExport
				if err = n.Call(ctx, id, "shared.export", sharedExportRequest{Source: source, After: cursor}, &page); err != nil {
					return err
				}
				for _, record := range page.Records {
					if err = n.verifyStoredRecord(ctx, record); err != nil {
						return err
					}
					body, _ := json.Marshal(record)
					if err = s.PutRecord(ctx, record.ID, record.Owner, record.Kind, record.Object, record.Sequence, record.Created, body); err != nil {
						return err
					}
				}
				for _, f := range page.Flows {
					if err = s.Transact(ctx, "", true, func(t *sharedsql.Tx) error {
						existing, e := t.Flow(ctx, f.ID)
						if e == nil {
							if existing.Revision != f.Revision || string(existing.Body) != string(f.Body) {
								return fmt.Errorf("流程内容冲突：%s", f.ID)
							}
							return nil
						}
						if e != sql.ErrNoRows {
							return e
						}
						return t.CreateFlow(ctx, f)
					}); err != nil {
						return err
					}
				}
				for _, receipt := range page.Receipts {
					if err = s.Transact(ctx, "", true, func(t *sharedsql.Tx) error {
						if prior, e := t.Receipt(ctx, receipt); e == nil {
							if prior.Kind != receipt.Kind || prior.ResourceID != receipt.ResourceID {
								return sharedsql.ErrConflict
							}
							return nil
						} else if e != sql.ErrNoRows {
							return e
						}
						return t.SaveReceipt(ctx, receipt)
					}); err != nil {
						return err
					}
				}
				m.Cursors[key] = page.Next
				if page.Next == "" {
					m.Cursors[key] = "complete"
				}
				if err = save(); err != nil {
					return err
				}
				return wait("import_shared_data")
			}
		}
		m.Stage = "verify"
		if err = save(); err != nil {
			return err
		}
	case "verify":
		store, err := n.sharedStore(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			var status SharedStatus
			if err = n.Call(ctx, id, "shared.flush", nil, &status); err != nil {
				return err
			}
			if status.Pending != 0 {
				return wait("upload_pending_records")
			}
			var page sharedExport
			if err = n.Call(ctx, id, "shared.export", sharedExportRequest{Source: "directory"}, &page); err != nil {
				return err
			}
			if err = n.importSharedDirectory(ctx, store, page.Directory); err != nil {
				return err
			}
		}
		m.Stage = "activate"
		if err = save(); err != nil {
			return err
		}
	case "activate":
		s, err := n.sharedStore(ctx)
		if err != nil {
			return err
		}
		if m.Secrets == nil {
			m.Secrets = map[string]sharedAccess{}
			for _, id := range ids {
				access, e := n.sealSharedAccess(ctx, id, m.Nodes[id])
				if e != nil {
					return e
				}
				m.Secrets[id] = access
			}
			if err = save(); err != nil {
				return err
			}
		}
		if err = s.Transact(ctx, "", true, func(t *sharedsql.Tx) error {
			for _, id := range ids {
				if e := t.Import(ctx, sharedObject("access", id, 1, m.Secrets[id])); e != nil {
					return e
				}
			}
			_, e := t.ExecContext(ctx, "UPDATE shared_metadata SET phase='active' WHERE id=1 AND phase='preparing'")
			return e
		}); err != nil {
			return err
		}
		state.Phase = "active"
		m.Stage = "complete"
		m.Nodes = nil
		m.Secrets = nil
		if err = save(); err != nil {
			return err
		}
	case "complete":
		op.State = protocol.Succeeded
		op.WaitingReason = ""
		op.Error = nil
		op.Revision++
		return n.saveOperation(ctx, op, "finished")
	}
	return wait(m.Stage)
}
func (n *Node) importSharedDirectory(ctx context.Context, s *sharedsql.Store, d Directory) error {
	return s.Transact(ctx, "", true, func(t *sharedsql.Tx) error {
		for _, v := range d.Admissions {
			if err := t.Import(ctx, sharedObject("admission", v.ID, 1, v)); err != nil {
				return err
			}
		}
		for _, v := range d.Members {
			if err := t.Import(ctx, sharedObject("member", v.ID, v.Version, v)); err != nil {
				return err
			}
			if err := t.Import(ctx, sharedObject("candidate", v.ID, v.Version, v.CoordinatorCandidate)); err != nil {
				return err
			}
		}
		for _, v := range d.Revocations {
			if err := t.Import(ctx, sharedObject("revocation", v.ID, 1, v)); err != nil {
				return err
			}
		}
		for _, v := range d.Databases {
			if v.CoordinatorEpoch == 0 {
				v.CoordinatorEpoch = 1
			}
			o := sharedObject("database", v.ID, v.Revision, v)
			if !v.Deleted {
				o.Identity = v.ClusterID
			}
			if err := t.Import(ctx, o); err != nil {
				return err
			}
		}
		for _, v := range d.Addresses {
			if err := t.Import(ctx, sharedObject("address", v.NodeID, v.Revision, v)); err != nil {
				return err
			}
		}
		if d.Trust != nil {
			if err := t.Import(ctx, sharedObject("trust", "policy", d.Trust.Version.Counter, d.Trust)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (n *Node) verifyStoredRecord(ctx context.Context, r Record) error {
	member, err := n.Member(ctx, r.Owner)
	if err != nil {
		return err
	}
	key, err := identity.Decode(member.Signing)
	if err != nil || r.Cluster != n.cluster || r.Sequence == 0 || r.ID != fmt.Sprintf("%s~%s~%s~%020d", r.Owner, r.Kind, r.Object, r.Sequence) || !ed25519.Verify(key, recordBytes(r), r.Signature) {
		return fault("conflict", "历史记录签名无效："+r.ID)
	}
	return nil
}
func (n *Node) exportShared(ctx context.Context, q sharedExportRequest) (sharedExport, error) {
	out := sharedExport{Records: []Record{}, Flows: []sharedsql.Flow{}, Sources: []string{}}
	if !n.sharedEnabled() {
		return out, fault("conflict", "节点尚未进入共享数据迁移")
	}
	switch q.Source {
	case "directory":
		dir, err := n.Directory(ctx)
		if err != nil {
			return out, err
		}
		out.Directory = dir
		c, err := n.Config(ctx)
		if err != nil {
			return out, err
		}
		if c.Storage.Enabled {
			d, err := n.adminDatabase(ctx)
			if err != nil {
				return out, err
			}
			var names []string
			if _, err = d.request(ctx, "GET", "/_all_dbs", nil, &names); err != nil {
				return out, err
			}
			for _, name := range names {
				if name == "opsd_state" || name == "opsd_operations" || strings.HasPrefix(name, "opsd_metrics_") {
					out.Sources = append(out.Sources, "couch/"+name)
				}
			}
		}
		return out, nil
	case "records":
		rows, err := n.DB.QueryContext(ctx, "SELECT body FROM records WHERE id>? ORDER BY id LIMIT 100", q.After)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		for rows.Next() {
			var body []byte
			var r Record
			if err = rows.Scan(&body); err != nil {
				return out, err
			}
			if err = json.Unmarshal(body, &r); err != nil {
				return out, err
			}
			out.Records = append(out.Records, r)
		}
		if len(out.Records) == 100 {
			out.Next = out.Records[len(out.Records)-1].ID
		}
		return out, rows.Err()
	case "flows":
		rows, err := n.DB.QueryContext(ctx, "SELECT id,body,payload,origin,request_key,request_digest FROM queue WHERE kind='database' AND id>? ORDER BY id LIMIT 1", q.After)
		if err != nil {
			return out, err
		}
		type row struct {
			id, body, payload, origin, key string
			digest                         []byte
		}
		items := []row{}
		for rows.Next() {
			var v row
			if err = rows.Scan(&v.id, &v.body, &v.payload, &v.origin, &v.key, &v.digest); err != nil {
				break
			}
			items = append(items, v)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return out, err
		}
		recipients, err := n.sharedRecipients(ctx)
		if err != nil {
			return out, err
		}
		for _, v := range items {
			var op Operation
			var encrypted Secret
			if err = json.Unmarshal([]byte(v.body), &op); err != nil {
				return out, err
			}
			if v.key != "" {
				out.Receipts = append(out.Receipts, sharedsql.Receipt{Actor: v.origin, Key: v.key, Digest: hex.EncodeToString(v.digest), Kind: "operation", ResourceID: op.ID, Result: json.RawMessage(v.body), CreatedAt: op.CreatedAt})
			}
			var cached string
			if e := n.DB.QueryRowContext(ctx, "SELECT body FROM shared_flow_exports WHERE id=? AND revision=?", op.ID, op.Revision).Scan(&cached); e == nil {
				var flow sharedsql.Flow
				if err = json.Unmarshal([]byte(cached), &flow); err != nil {
					return out, err
				}
				out.Flows = append(out.Flows, flow)
				continue
			} else if e != sql.ErrNoRows {
				return out, e
			}
			if err = json.Unmarshal([]byte(v.payload), &encrypted); err != nil {
				return out, err
			}
			plain, e := unseal(encrypted, n.age)
			if e != nil {
				return out, e
			}
			input, e := seal(plain, recipients)
			if e != nil {
				return out, e
			}
			payload := sharedFlowPayload{Operation: op, Input: input}
			if flow, e := n.loadDatabaseWorkflow(ctx, v.id); e == nil {
				body, _ := json.Marshal(flow)
				secret, e := seal(body, recipients)
				if e != nil {
					return out, e
				}
				payload.Progress = &secret
			} else if e != sql.ErrNoRows {
				return out, e
			}
			body, _ := json.Marshal(payload)
			flow := sharedsql.Flow{ID: op.ID, ResourceID: op.ResourceID, Coordinator: n.ID, Epoch: 1, Revision: op.Revision, Terminal: operations.Terminal(op.State), Body: body}
			body, _ = json.Marshal(flow)
			if _, err = n.DB.ExecContext(ctx, "INSERT INTO shared_flow_exports(id,revision,body) VALUES(?,?,?)", op.ID, op.Revision, string(body)); err != nil {
				return out, err
			}
			out.Flows = append(out.Flows, flow)
		}
		if len(items) == 1 {
			out.Next = items[len(items)-1].id
		}
		return out, nil
	default:
		if !strings.HasPrefix(q.Source, "couch/opsd_") {
			return out, fault("invalid_request", "迁移来源无效")
		}
		d, err := n.adminDatabase(ctx)
		if err != nil {
			return out, err
		}
		name := strings.TrimPrefix(q.Source, "couch/")
		values := url.Values{"include_docs": {"true"}, "limit": {"100"}}
		if q.After != "" {
			cursor, _ := json.Marshal(q.After)
			values.Set("startkey", string(cursor))
			values.Set("skip", "1")
		}
		var page struct {
			Rows []struct {
				ID  string `json:"id"`
				Doc Record `json:"doc"`
			} `json:"rows"`
		}
		if _, err = d.request(ctx, "GET", "/"+name+"/_all_docs?"+values.Encode(), nil, &page); err != nil {
			return out, err
		}
		for _, row := range page.Rows {
			if strings.HasPrefix(row.ID, "_design/") {
				continue
			}
			out.Records = append(out.Records, row.Doc)
		}
		if len(page.Rows) == 100 {
			out.Next = page.Rows[len(page.Rows)-1].ID
		}
		return out, nil
	}
}
