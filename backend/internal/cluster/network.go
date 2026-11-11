package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/publicaddress"
	"github.com/jkfujr/opsd/internal/transport"
)

type PublicFamily struct {
	Mode         string                        `json:"mode"`
	Addresses    []string                      `json:"addresses"`
	State        string                        `json:"state"`
	Observations []protocol.AddressObservation `json:"observations"`
	UpdatedAt    *time.Time                    `json:"updated_at,omitempty"`
	Error        string                        `json:"error,omitempty"`
}
type PublicAddresses struct {
	Cached   bool         `json:"cached,omitempty"`
	NodeID   string       `json:"node_id"`
	Revision uint64       `json:"revision"`
	IPv4     PublicFamily `json:"ipv4"`
	IPv6     PublicFamily `json:"ipv6"`
}
type AddressRecord struct {
	Cluster string `json:"cluster"`
	PublicAddresses
	Signature []byte `json:"signature"`
}
type PublicFamilyPatch struct {
	Mode      string   `json:"mode"`
	Addresses []string `json:"addresses"`
}
type PublicAddressesPatch struct {
	ExpectedRevision      uint64             `json:"expected_revision"`
	ExpectedConfiguration string             `json:"expected_configuration,omitempty"`
	IPv4                  *PublicFamilyPatch `json:"ipv4,omitempty"`
	IPv6                  *PublicFamilyPatch `json:"ipv6,omitempty"`
}

// PublicConfigurationVersion 只包含保存的模式与手工地址，采集时间不改变配置版本。
func PublicConfigurationVersion(v PublicAddresses) string {
	family := func(f PublicFamily) PublicFamilyPatch {
		p := PublicFamilyPatch{Mode: f.Mode, Addresses: []string{}}
		if f.Mode == "manual" {
			p.Addresses = f.Addresses
		}
		return p
	}
	b, _ := json.Marshal([]PublicFamilyPatch{family(v.IPv4), family(v.IPv6)})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type TrustVersion struct {
	Counter uint64 `json:"counter"`
	NodeID  string `json:"node_id"`
}
type TrustPolicyState struct {
	Version TrustVersion `json:"version"`
	Enabled bool         `json:"enabled"`
}
type TrustPolicy struct {
	Cluster string `json:"cluster"`
	TrustPolicyState
	Signature []byte `json:"signature"`
}
type TrustPolicyPatch struct {
	ExpectedVersion TrustVersion `json:"expected_version"`
	Enabled         bool         `json:"enabled"`
}
type TrustMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type TrustRule struct {
	Address string                 `json:"address,omitempty"`
	Family  protocol.AddressFamily `json:"family"`
	Members []TrustMember          `json:"members"`
	State   string                 `json:"state"`
	Error   string                 `json:"error,omitempty"`
}
type TrustRules struct {
	NodeID          string                             `json:"node_id"`
	Enabled         bool                               `json:"enabled"`
	PolicyVersion   TrustVersion                       `json:"policy_version"`
	TargetRevision  uint64                             `json:"target_revision"`
	AppliedRevision uint64                             `json:"applied_revision"`
	State           string                             `json:"state"`
	Rules           []TrustRule                        `json:"rules"`
	Results         []protocol.ScopeResult             `json:"results"`
	CheckedAt       *time.Time                         `json:"checked_at,omitempty"`
	Migration       *protocol.PeerTrustMigrationStatus `json:"migration,omitempty"`
}
type TrustStatus struct {
	Policy TrustPolicyState `json:"policy"`
	Nodes  []TrustRules     `json:"nodes"`
}

func emptyPublic(id string) PublicAddresses {
	f := func() PublicFamily {
		return PublicFamily{Mode: "auto", Addresses: []string{}, State: "pending", Observations: []protocol.AddressObservation{}}
	}
	return PublicAddresses{NodeID: id, IPv4: f(), IPv6: f()}
}
func addressBytes(v AddressRecord) []byte { v.Signature = nil; b, _ := json.Marshal(v); return b }
func trustBytes(v TrustPolicy) []byte     { v.Signature = nil; b, _ := json.Marshal(v); return b }

func readPolicy(ctx context.Context, q firewallQuerier) (TrustPolicy, error) {
	var b string
	var p TrustPolicy
	e := q.QueryRowContext(ctx, "SELECT body FROM peer_trust_policy WHERE id=1").Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return p, nil
	}
	if e == nil {
		e = json.Unmarshal([]byte(b), &p)
	}
	return p, e
}
func (n *Node) TrustPolicy(ctx context.Context) (TrustPolicyState, error) {
	p, e := readPolicy(ctx, n.DB)
	return p.TrustPolicyState, e
}
func policyWins(a, b TrustPolicy) bool {
	if a.Version.Counter != b.Version.Counter {
		return a.Version.Counter > b.Version.Counter
	}
	if a.Enabled != b.Enabled {
		return !a.Enabled
	}
	return a.Version.NodeID > b.Version.NodeID
}
func (n *Node) ChangeTrust(ctx context.Context, request TrustPolicyPatch) (TrustPolicyState, error) {
	if !n.Active(ctx, n.ID) {
		return TrustPolicyState{}, fault("forbidden", "当前节点已撤销")
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return TrustPolicyState{}, e
	}
	defer tx.Rollback()
	p, e := readPolicy(ctx, tx)
	if e != nil {
		return p.TrustPolicyState, e
	}
	if p.Version != request.ExpectedVersion {
		return p.TrustPolicyState, fault("precondition_failed", "互信设置已经改变，请刷新")
	}
	if p.Enabled == request.Enabled {
		return p.TrustPolicyState, nil
	}
	p = TrustPolicy{Cluster: n.cluster, TrustPolicyState: TrustPolicyState{Version: TrustVersion{Counter: p.Version.Counter + 1, NodeID: n.ID}, Enabled: request.Enabled}}
	p.Signature = ed25519.Sign(n.signing, trustBytes(p))
	b, _ := json.Marshal(p)
	if _, e = tx.ExecContext(ctx, "INSERT INTO peer_trust_policy(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(b)); e == nil {
		e = n.appendTx(ctx, tx, "peer_trust", n.ID, p, n.Now())
	}
	if e == nil {
		e = tx.Commit()
	}
	if e == nil {
		n.signalNetwork()
		n.notifyChange(Change{Resource: "peer_trust"})
	}
	return p.TrustPolicyState, e
}
func readPublic(ctx context.Context, q firewallQuerier, id string) (AddressRecord, error) {
	v := AddressRecord{PublicAddresses: emptyPublic(id)}
	var b string
	e := q.QueryRowContext(ctx, "SELECT body FROM public_addresses WHERE owner=?", id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e == nil {
		e = json.Unmarshal([]byte(b), &v)
	}
	return v, e
}
func (n *Node) PublicAddresses(ctx context.Context, id string) (PublicAddresses, error) {
	if !n.Active(ctx, id) {
		return PublicAddresses{}, fault("not_found", "节点不存在或已撤销")
	}
	v, e := readPublic(ctx, n.DB, id)
	return v.PublicAddresses, e
}
func (n *Node) ReadPublicAddresses(ctx context.Context, id string) (PublicAddresses, error) {
	var out PublicAddresses
	if e := n.Call(ctx, id, "public_addresses.read", nil, &out); e == nil {
		return out, nil
	}
	out, e := n.PublicAddresses(ctx, id)
	out.Cached = true
	return out, e
}
func (n *Node) savePublicTx(ctx context.Context, tx *sql.Tx, v AddressRecord) error {
	v.Cluster = n.cluster
	v.Revision++
	v.Signature = nil
	v.Signature = ed25519.Sign(n.signing, addressBytes(v))
	b, _ := json.Marshal(v)
	_, e := tx.ExecContext(ctx, "INSERT INTO public_addresses(owner,revision,body) VALUES(?,?,?) ON CONFLICT(owner) DO UPDATE SET revision=excluded.revision,body=excluded.body", n.ID, v.Revision, string(b))
	if e == nil {
		e = n.appendTx(ctx, tx, "public_addresses", n.ID, v, n.Now())
	}
	return e
}
func (n *Node) PatchPublic(ctx context.Context, request PublicAddressesPatch) (PublicAddresses, error) {
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return PublicAddresses{}, e
	}
	defer tx.Rollback()
	v, e := readPublic(ctx, tx, n.ID)
	if e != nil {
		return v.PublicAddresses, e
	}
	if request.ExpectedConfiguration != "" && PublicConfigurationVersion(v.PublicAddresses) != request.ExpectedConfiguration || request.ExpectedConfiguration == "" && v.Revision != request.ExpectedRevision {
		return v.PublicAddresses, fault("precondition_failed", "公网地址已经改变，请刷新")
	}
	for _, item := range []struct {
		family string
		patch  *PublicFamilyPatch
		value  *PublicFamily
	}{{"ipv4", request.IPv4, &v.IPv4}, {"ipv6", request.IPv6, &v.IPv6}} {
		if item.patch == nil {
			continue
		}
		p := item.patch
		if (p.Mode != "auto" && p.Mode != "manual") || len(p.Addresses) > 16 {
			return v.PublicAddresses, fault("invalid_request", "公网地址模式或数量无效")
		}
		if p.Mode == "manual" {
			addresses := []string{}
			for _, text := range p.Addresses {
				a, err := publicaddress.Parse(text)
				if err != nil || a.Is6() != (item.family == "ipv6") {
					return v.PublicAddresses, fault("invalid_request", "请输入对应地址族的精确公网 IP")
				}
				if !slices.Contains(addresses, a.String()) {
					addresses = append(addresses, a.String())
				}
			}
			sort.Strings(addresses)
			item.value.Addresses = addresses
			item.value.State = "confirmed"
		} else {
			item.value.State = "pending"
		}
		item.value.Mode = p.Mode
		item.value.Error = ""
		now := n.Now().UTC()
		item.value.UpdatedAt = &now
	}
	if e = n.savePublicTx(ctx, tx, v); e == nil {
		e = n.advertisePublicTx(ctx, tx, v.PublicAddresses)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		return v.PublicAddresses, e
	}
	n.signalNetwork()
	n.notifyChange(Change{Resource: "public_addresses", NodeID: n.ID})
	return n.PublicAddresses(ctx, n.ID)
}
func reconcileObservation(current PublicFamily, observations []protocol.AddressObservation, now time.Time) PublicFamily {
	if current.Mode == "manual" {
		return current
	}
	current.Observations = observations
	current.UpdatedAt = &now
	current.Error = ""
	values := []string{}
	for _, o := range observations {
		if o.Error == nil && o.Address != "" && !slices.Contains(values, o.Address) {
			values = append(values, o.Address)
		}
	}
	switch len(values) {
	case 0:
		if len(observations) == 1 && observations[0].Source == "local" && observations[0].Error != nil && observations[0].Error.Code == "address_unavailable" {
			current.State = "unavailable"
			current.Addresses = []string{}
			break
		}
		current.State = "failed"
		current.Error = "公网地址发现失败"
	case 1:
		current.Addresses = values
		current.State = "confirmed"
	default:
		current.State = "conflict"
		current.Error = "公网地址来源不一致"
	}
	return current
}
func (n *Node) RefreshPublic(ctx context.Context, expected ...uint64) (PublicAddresses, error) {
	if !n.addressMu.TryLock() {
		current, err := n.PublicAddresses(ctx, n.ID)
		if err == nil && len(expected) > 0 && current.Revision != expected[0] {
			err = fault("conflict", "公网地址状态已经改变，请重新读取")
		}
		return current, err
	}
	defer n.addressMu.Unlock()
	before, e := n.PublicAddresses(ctx, n.ID)
	if e != nil {
		return before, e
	}
	if len(expected) > 0 && before.Revision != expected[0] {
		return before, fault("conflict", "公网地址状态已经改变，请重新读取")
	}
	observations := make([][]protocol.AddressObservation, 2)
	var wg sync.WaitGroup
	for i, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		old := before.IPv4
		if i == 1 {
			old = before.IPv6
		}
		if old.Mode == "manual" {
			continue
		}
		wg.Add(1)
		go func(i int, family protocol.AddressFamily) {
			defer wg.Done()
			observations[i] = n.observePublic(ctx, family)
		}(i, family)
	}
	wg.Wait()
	if e = ctx.Err(); e != nil {
		return before, e
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return before, e
	}
	defer tx.Rollback()
	v, e := readPublic(ctx, tx, n.ID)
	if e != nil {
		return before, e
	}
	if len(expected) > 0 && v.Revision != expected[0] {
		return before, fault("conflict", "公网地址状态已经改变，请重新读取")
	}
	now := n.Now().UTC()
	v.IPv4 = reconcileObservation(v.IPv4, observations[0], now)
	v.IPv6 = reconcileObservation(v.IPv6, observations[1], now)
	if e = n.savePublicTx(ctx, tx, v); e == nil {
		e = n.advertisePublicTx(ctx, tx, v.PublicAddresses)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e == nil {
		n.notifyChange(Change{Resource: "public_addresses", NodeID: n.ID})
		n.notify()
	}
	if e != nil {
		return before, e
	}
	return n.PublicAddresses(ctx, n.ID)
}
func (n *Node) advertisePublicTx(ctx context.Context, tx *sql.Tx, v PublicAddresses) error {
	var body string
	if e := tx.QueryRowContext(ctx, "SELECT config FROM node_identity WHERE id=1").Scan(&body); e != nil {
		return e
	}
	var c Config
	if e := json.Unmarshal([]byte(body), &c); e != nil {
		return e
	}
	if c.AddressMode != "auto" {
		return nil
	}
	address := ""
	if len(v.IPv4.Addresses) > 0 {
		address = v.IPv4.Addresses[0]
	} else if len(v.IPv6.Addresses) > 0 {
		address = v.IPv6.Addresses[0]
	}
	if address == "" {
		return nil
	}
	u, e := url.Parse(c.Address)
	if e != nil {
		return e
	}
	port := u.Port()
	if port == "" {
		_, port, _ = net.SplitHostPort(c.Listen)
	}
	if port == "" {
		port = "8444"
	}
	u.Host = net.JoinHostPort(address, port)
	if c.Address == u.String() {
		return nil
	}
	c.Address = u.String()
	c.Revision++
	b, _ := json.Marshal(c)
	if _, e = tx.ExecContext(ctx, "UPDATE node_identity SET config=? WHERE id=1", string(b)); e == nil {
		e = n.publishMemberTx(ctx, tx, c)
	}
	return e
}
func (n *Node) observePublic(ctx context.Context, family protocol.AddressFamily) []protocol.AddressObservation {
	if n.ObservePublic != nil {
		return n.ObservePublic(ctx, family)
	}
	result := []protocol.AddressObservation{}
	now := n.Now().UTC()
	if family == protocol.IPv6 {
		available, err := publicaddress.HasIPv6()
		if err == nil && !available {
			return []protocol.AddressObservation{{Source: "local", ObservedAt: now, Error: &protocol.Error{Code: "address_unavailable", Message: "无公网 IPv6"}}}
		}
	}
	if address, e := n.probePublic(ctx, family); e == nil {
		result = append(result, protocol.AddressObservation{Source: "peer", Address: address, ObservedAt: now})
	} else {
		result = append(result, protocol.AddressObservation{Source: "peer", ObservedAt: now, Error: &protocol.Error{Code: "dependency_unavailable", Message: e.Error()}})
	}
	address, e := publicaddress.Echo(ctx, string(family))
	o := protocol.AddressObservation{Source: "ipify", Address: address, ObservedAt: now}
	if e != nil {
		o.Error = &protocol.Error{Code: "dependency_unavailable", Message: e.Error()}
	}
	return append(result, o)
}
func (n *Node) probePublic(ctx context.Context, family protocol.AddressFamily) (string, error) {
	d, e := n.Directory(ctx)
	if e != nil {
		return "", e
	}
	for _, m := range d.Members {
		if m.ID == n.ID || n.Revoked(ctx, m.ID) {
			continue
		}
		u, e := url.Parse(m.Address)
		if e != nil {
			continue
		}
		if a, err := netip.ParseAddr(u.Hostname()); err == nil && a.Is6() != (family == protocol.IPv6) {
			found := false
			for _, record := range d.Addresses {
				if record.NodeID == m.ID {
					f := record.IPv4
					if family == protocol.IPv6 {
						f = record.IPv6
					}
					if len(f.Addresses) > 0 {
						port := u.Port()
						if port == "" {
							port = "8444"
						}
						u.Host = net.JoinHostPort(f.Addresses[0], port)
						found = true
					}
					break
				}
			}
			if !found {
				continue
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		client := publicaddress.Client(string(family))
		defer client.CloseIdleConnections()
		socket, _, e := websocket.Dial(ctx, u.String()+"/peer", &websocket.DialOptions{HTTPClient: client})
		if e != nil {
			return "", fmt.Errorf("成员回显不可达")
		}
		key, e := identity.Decode(m.Public)
		if e != nil {
			socket.CloseNow()
			return "", e
		}
		channel, e := transport.Handshake(ctx, socket, n.Key, key, true)
		if e != nil {
			socket.CloseNow()
			return "", e
		}
		defer channel.Close()
		self, e := n.Self(ctx)
		if e != nil {
			return "", e
		}
		b, _ := json.Marshal(self)
		id := identity.Random()
		if e = channel.Send(ctx, protocol.Message{RequestID: id, Payload: &protocol.PeerHello{Member: b, ProbeFamily: family}}); e != nil {
			return "", e
		}
		reply, e := channel.Receive(ctx)
		if e != nil {
			return "", e
		}
		echo, ok := reply.Payload.(*protocol.AddressEcho)
		if !ok || reply.ReplyTo != id {
			return "", fmt.Errorf("成员回显应答无效")
		}
		a, e := publicaddress.Parse(echo.Address)
		if e != nil || a.Is6() != (family == protocol.IPv6) {
			return "", fmt.Errorf("成员回显未取得公网地址")
		}
		return a.String(), nil
	}
	return "", fmt.Errorf("没有可用的成员回显")
}

func (n *Node) networkDirectory(ctx context.Context, d *Directory) error {
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM public_addresses ORDER BY owner")
	if e != nil {
		return e
	}
	for rows.Next() {
		var b string
		var v AddressRecord
		if e = rows.Scan(&b); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			break
		}
		d.Addresses = append(d.Addresses, v)
	}
	err := rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if err != nil {
		return err
	}
	p, e := readPolicy(ctx, n.DB)
	if e == nil && p.Version.Counter > 0 {
		d.Trust = &p
	}
	return e
}
func (n *Node) mergeNetworkTx(ctx context.Context, tx *sql.Tx, d Directory, known map[string]Member, revoked map[string]bool) error {
	if len(d.Addresses) > 1000 {
		return fault("invalid_request", "公网地址目录过大")
	}
	for _, v := range d.Addresses {
		if revoked[v.NodeID] {
			continue
		}
		owner, ok := known[v.NodeID]
		key, e := identity.Decode(owner.Signing)
		if !ok || e != nil || v.Cluster != n.cluster || v.Revision == 0 || !ed25519.Verify(key, addressBytes(v), v.Signature) {
			return fault("forbidden", "公网地址签名无效")
		}
		for _, item := range []struct {
			f    PublicFamily
			ipv6 bool
		}{{v.IPv4, false}, {v.IPv6, true}} {
			if len(item.f.Addresses) > 16 || len(item.f.Observations) > 2 || (item.f.Mode != "auto" && item.f.Mode != "manual") {
				return fault("invalid_request", "公网地址记录无效")
			}
			for _, text := range item.f.Addresses {
				a, e := publicaddress.Parse(text)
				if e != nil || a.Is6() != item.ipv6 {
					return fault("invalid_request", "公网地址记录无效")
				}
			}
		}
		b, _ := json.Marshal(v)
		if _, e = tx.ExecContext(ctx, "INSERT INTO public_addresses(owner,revision,body) VALUES(?,?,?) ON CONFLICT(owner) DO UPDATE SET revision=excluded.revision,body=excluded.body WHERE public_addresses.revision<excluded.revision", v.NodeID, v.Revision, string(b)); e != nil {
			return e
		}
	}
	if p := d.Trust; p != nil && p.Version.Counter > 0 && !revoked[p.Version.NodeID] {
		owner, ok := known[p.Version.NodeID]
		key, e := identity.Decode(owner.Signing)
		if !ok || e != nil || p.Cluster != n.cluster || !ed25519.Verify(key, trustBytes(*p), p.Signature) {
			return fault("forbidden", "互信设置签名无效")
		}
		current, e := readPolicy(ctx, tx)
		if e != nil {
			return e
		}
		if policyWins(*p, current) {
			b, _ := json.Marshal(p)
			_, e = tx.ExecContext(ctx, "INSERT INTO peer_trust_policy(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(b))
			if e != nil {
				return e
			}
		}
	}
	return nil
}
func (n *Node) signalNetwork() {
	select {
	case n.networkWake <- struct{}{}:
	default:
	}
}
func (n *Node) networkLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var last time.Time
	fingerprint := ""
	addressMode := ""
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 25*time.Second)
		c, e := n.Config(ctx)
		p, _ := n.TrustPolicy(ctx)
		network := publicaddress.Fingerprint()
		addresses, _ := n.PublicAddresses(ctx, n.ID)
		pending := addresses.Revision > 0 && (addresses.IPv4.Mode == "auto" && addresses.IPv4.State == "pending" || addresses.IPv6.Mode == "auto" && addresses.IPv6.State == "pending")
		if e == nil && (c.AddressMode != "" || p.Enabled || pending) && (last.IsZero() || pending || c.AddressMode != addressMode || n.Now().Sub(last) >= 5*time.Minute || network != fingerprint) {
			_, _ = n.RefreshPublic(ctx)
			last = n.Now()
			fingerprint = network
			addressMode = c.AddressMode
		}
		n.syncTrust(ctx)
		n.migrateTrust(ctx)
		cancel()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		case <-n.networkWake:
		}
	}
}
func activeTrustMembers(d Directory) map[string]Member {
	members := map[string]Member{}
	revoked := map[string]bool{}
	for _, r := range d.Revocations {
		revoked[r.ID] = true
	}
	for _, m := range d.Members {
		if !revoked[m.ID] {
			members[m.ID] = m
		}
	}
	return members
}

func desiredTrust(d Directory, enabled bool) protocol.PeerTrustTarget {
	target := protocol.PeerTrustTarget{Enabled: enabled, IPv4: []string{}, IPv6: []string{}}
	if !enabled {
		return target
	}
	members := activeTrustMembers(d)
	for _, r := range d.Addresses {
		if _, ok := members[r.NodeID]; !ok {
			continue
		}
		for i, f := range []PublicFamily{r.IPv4, r.IPv6} {
			list := &target.IPv4
			if i == 1 {
				list = &target.IPv6
			}
			for _, ip := range f.Addresses {
				if !slices.Contains(*list, ip) {
					*list = append(*list, ip)
				}
			}
		}
	}
	sort.Strings(target.IPv4)
	sort.Strings(target.IPv6)
	return target
}
func equalTrustTarget(a, b protocol.PeerTrustTarget) bool {
	a.Revision = 0
	b.Revision = 0
	return reflect.DeepEqual(a, b)
}
func (n *Node) syncTrust(ctx context.Context) {
	if n.Executor == nil {
		return
	}
	d, e := n.Directory(ctx)
	if e != nil {
		return
	}
	p, _ := n.TrustPolicy(ctx)
	next := desiredTrust(d, p.Enabled && !n.Revoked(ctx, n.ID))
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	var b string
	var old protocol.PeerTrustTarget
	e = tx.QueryRowContext(ctx, "SELECT body FROM peer_trust_target WHERE id=1").Scan(&b)
	if e == nil {
		if json.Unmarshal([]byte(b), &old) != nil {
			return
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return
	}
	if old.Revision == 0 && !next.Enabled {
		return
	}
	next.Revision = old.Revision
	if !equalTrustTarget(next, old) || next.Revision == 0 {
		next.Revision++
		b, _ := json.Marshal(next)
		if _, e = tx.ExecContext(ctx, "INSERT INTO peer_trust_target(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(b)); e != nil {
			return
		}
	}
	if e = tx.Commit(); e != nil {
		return
	}
	reply, e := n.Executor.Call(ctx, &next)
	if e != nil {
		return
	}
	status, ok := reply.(*protocol.PeerTrustUpdated)
	if !ok {
		return
	}
	data, _ := json.Marshal(status)
	if _, e = n.DB.ExecContext(ctx, "INSERT INTO peer_trust_status(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(data)); e == nil {
		n.notifyChange(Change{Resource: "peer_trust", NodeID: n.ID})
	}
}
func (n *Node) LocalTrustRules(ctx context.Context) (TrustRules, error) {
	d, e := n.Directory(ctx)
	if e != nil {
		return TrustRules{}, e
	}
	p, e := n.TrustPolicy(ctx)
	if e != nil {
		return TrustRules{}, e
	}
	desired := desiredTrust(d, p.Enabled)
	out := TrustRules{NodeID: n.ID, Enabled: p.Enabled, PolicyVersion: p.Version, State: "pending", Rules: []TrustRule{}, Results: []protocol.ScopeResult{}}
	config, err := n.FirewallConfig(ctx)
	if err != nil {
		return out, err
	}
	out.Migration = config.Migration
	var body string
	var target protocol.PeerTrustTarget
	var status protocol.PeerTrustUpdated
	if e = n.DB.QueryRowContext(ctx, "SELECT body FROM peer_trust_target WHERE id=1").Scan(&body); e == nil {
		_ = json.Unmarshal([]byte(body), &target)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e = n.DB.QueryRowContext(ctx, "SELECT body FROM peer_trust_status WHERE id=1").Scan(&body); e == nil {
		_ = json.Unmarshal([]byte(body), &status)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	out.TargetRevision = target.Revision
	out.AppliedRevision = status.AppliedRevision
	out.Results = status.Results
	out.CheckedAt = status.CheckedAt
	if out.Results == nil {
		out.Results = []protocol.ScopeResult{}
	}
	current := equalTrustTarget(target, desired) && target.Revision == status.TargetRevision
	if !p.Enabled && target.Revision == 0 {
		out.State = "disabled"
	} else if current && status.AppliedRevision == target.Revision {
		out.State = "in_sync"
		if !p.Enabled {
			out.State = "disabled"
		}
	}
	for _, r := range out.Results {
		if current && (r.State == "failed" || r.State == "unsupported") {
			out.State = "failed"
		}
	}
	members := activeTrustMembers(d)
	for i, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		ips := desired.IPv4
		if i == 1 {
			ips = desired.IPv6
		}
		ips = append([]string(nil), ips...)
		for _, r := range status.Results {
			if r.Family == family {
				for _, ip := range r.Addresses {
					if !slices.Contains(ips, ip) {
						ips = append(ips, ip)
					}
				}
			}
		}
		sort.Strings(ips)
		for _, ip := range ips {
			row := TrustRule{Address: ip, Family: family, Members: []TrustMember{}, State: "pending"}
			wanted := p.Enabled && slices.Contains(func() []string {
				if i == 0 {
					return desired.IPv4
				}
				return desired.IPv6
			}(), ip)
			if !wanted {
				row.State = "removing"
			}
			present := 0
			inactive := false
			for _, r := range out.Results {
				if r.Family != family {
					continue
				}
				if r.State == "failed" || r.State == "unsupported" {
					row.State = r.State
					if r.Error != nil {
						row.Error = r.Error.Message
					}
				}
				if slices.Contains(r.Addresses, ip) && r.State == "in_sync" {
					present++
				}
				if r.State == "inactive" {
					inactive = true
				}
			}
			if wanted && current && present == 2 {
				row.State = "in_sync"
			} else if wanted && current && present == 1 && inactive {
				row.State = "saved"
			}
			for _, r := range d.Addresses {
				f := r.IPv4
				if i == 1 {
					f = r.IPv6
				}
				m, active := members[r.NodeID]
				if !active || !slices.Contains(f.Addresses, ip) {
					continue
				}
				row.Members = append(row.Members, TrustMember{ID: m.ID, Name: m.Name})
			}
			out.Rules = append(out.Rules, row)
		}
		if p.Enabled {
			for _, m := range d.Members {
				if _, active := members[m.ID]; !active {
					continue
				}
				f := emptyPublic(m.ID).IPv4
				for _, r := range d.Addresses {
					if r.NodeID == m.ID {
						f = r.IPv4
						if i == 1 {
							f = r.IPv6
						}
						break
					}
				}
				if len(f.Addresses) == 0 && f.Mode != "manual" {
					out.Rules = append(out.Rules, TrustRule{Family: family, Members: []TrustMember{{ID: m.ID, Name: m.Name}}, State: f.State, Error: f.Error})
				}
			}
		}
	}
	return out, nil
}
func (n *Node) TrustStatus(ctx context.Context) (TrustStatus, error) {
	p, e := n.TrustPolicy(ctx)
	out := TrustStatus{Policy: p, Nodes: []TrustRules{}}
	if e != nil {
		return out, e
	}
	d, e := n.Directory(ctx)
	if e != nil {
		return out, e
	}
	for _, m := range d.Members {
		if n.Revoked(ctx, m.ID) {
			continue
		}
		out.Nodes = append(out.Nodes, TrustRules{NodeID: m.ID, Enabled: p.Enabled, PolicyVersion: p.Version, State: "offline", Rules: []TrustRule{}, Results: []protocol.ScopeResult{}})
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i := range out.Nodes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			var v TrustRules
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if n.Call(c, out.Nodes[i].NodeID, "peer_trust.rules", nil, &v) == nil {
				out.Nodes[i] = v
			}
		}(i)
	}
	wg.Wait()
	return out, nil
}

// probeEcho 只回显认证短连接的来源，不接替业务连接，不采信转发头。
func (n *Node) probeEcho(ctx context.Context, w http.ResponseWriter, r *http.Request, channel *transport.Channel, first protocol.Message, hello *protocol.PeerHello) {
	if hello.ProbeFamily != protocol.IPv4 && hello.ProbeFamily != protocol.IPv6 {
		n.sendFailure(ctx, channel, first.RequestID, fault("invalid_request", "地址族无效"))
		return
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return
	}
	a, e := publicaddress.Parse(host)
	if e != nil || a.Is6() != (hello.ProbeFamily == protocol.IPv6) {
		n.sendFailure(ctx, channel, first.RequestID, fault("dependency_unavailable", "连接来源未取得公网地址"))
		return
	}
	_ = channel.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: first.RequestID, Payload: &protocol.AddressEcho{Family: hello.ProbeFamily, Address: a.String(), ObservedAt: n.Now().UTC()}})
}
