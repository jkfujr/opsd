package cluster

import (
	"context"
	"crypto/ed25519"
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestSingleNodeTrustIncludesItsAddressesAndPendingState(t *testing.T) {
	n, _ := openTestNode(t)
	if _, err := n.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	v, err := n.LocalTrustRules(t.Context())
	if err != nil || len(v.Rules) != 2 || v.Rules[0].Members[0].ID != n.ID || v.Rules[0].State != "pending" {
		t.Fatal(v, err)
	}
	p, _ := n.PublicAddresses(t.Context(), n.ID)
	_, err = n.PatchPublic(t.Context(), PublicAddressesPatch{ExpectedRevision: p.Revision, IPv4: &PublicFamilyPatch{Mode: "manual", Addresses: []string{"8.8.8.8"}}, IPv6: &PublicFamilyPatch{Mode: "manual", Addresses: []string{"2606:4700:4700::1111"}}})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := n.Directory(t.Context())
	target := desiredTrust(d, true)
	if !slices.Equal(target.IPv4, []string{"8.8.8.8"}) || !slices.Equal(target.IPv6, []string{"2606:4700:4700::1111"}) {
		t.Fatal(target)
	}
	v, err = n.LocalTrustRules(t.Context())
	if err != nil || len(v.Rules) != 2 {
		t.Fatal(v, err)
	}
	for _, r := range v.Rules {
		if len(r.Members) != 1 || r.Members[0].ID != n.ID || r.Address == "" {
			t.Fatal("本机规则关联缺失", r)
		}
	}
}

func TestSharedTrustAddressIncludesSelfAndOnlyActiveMembers(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	manualAddress(t, a, "8.8.8.8")
	manualAddress(t, b, "8.8.8.8")
	d, _ := b.Directory(t.Context())
	if err := a.merge(t.Context(), d, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	read, err := a.LocalTrustRules(t.Context())
	if err != nil || len(read.Rules) != 1 || len(read.Rules[0].Members) != 2 {
		t.Fatal(read, err)
	}
	if err = a.Revoke(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	read, err = a.LocalTrustRules(t.Context())
	if err != nil || len(read.Rules) != 1 || len(read.Rules[0].Members) != 1 || read.Rules[0].Members[0].ID != a.ID {
		t.Fatal("撤销改变了本机的共享来源", read, err)
	}
	manualAddress(t, a, "9.9.9.9")
	d, _ = a.Directory(t.Context())
	if got := desiredTrust(d, true); !slices.Equal(got.IPv4, []string{"9.9.9.9"}) {
		t.Fatal("地址替换保留了旧来源", got)
	}
	if got := desiredTrust(d, false); len(got.IPv4)+len(got.IPv6) != 0 {
		t.Fatal("关闭后保留了互信来源", got)
	}
}

type trustExecutor struct {
	lostExecutor
	target protocol.PeerTrustTarget
}

func (x *trustExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	x.target = *p.(*protocol.PeerTrustTarget)
	return &protocol.PeerTrustUpdated{TargetRevision: x.target.Revision}, nil
}

func TestRevokedNodeRemovesItsPersistedTrust(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	manualAddress(t, a, "8.8.8.8")
	if _, e := a.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true}); e != nil {
		t.Fatal(e)
	}
	d, _ := a.Directory(t.Context())
	if e := b.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	x := &trustExecutor{}
	b.Executor = x
	b.syncTrust(t.Context())
	if !x.target.Enabled || len(x.target.IPv4) != 1 {
		t.Fatal("互信目标未生成", x.target)
	}
	if e := a.Revoke(t.Context(), b.ID); e != nil {
		t.Fatal(e)
	}
	d, _ = a.Directory(t.Context())
	if e := b.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	b.syncTrust(t.Context())
	if x.target.Enabled || len(x.target.IPv4)+len(x.target.IPv6) != 0 || x.target.Revision != 2 {
		t.Fatal("撤销后未移除互信", x.target)
	}
}

func manualAddress(t *testing.T, n *Node, ip string) {
	t.Helper()
	v, e := n.PublicAddresses(t.Context(), n.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = n.PatchPublic(t.Context(), PublicAddressesPatch{ExpectedRevision: v.Revision, IPv4: &PublicFamilyPatch{Mode: "manual", Addresses: []string{ip}}, IPv6: &PublicFamilyPatch{Mode: "manual", Addresses: []string{}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestTrustPolicyAndAddressesConvergeWithoutDatabase(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	c, _ := openTestNode(t)
	joinTest(t, b, a)
	joinTest(t, c, a)
	d, _ := a.Directory(t.Context())
	if e := b.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	manualAddress(t, b, "8.8.8.8")
	manualAddress(t, c, "8.8.8.8")
	for _, n := range []*Node{b, c} {
		d, _ = n.Directory(t.Context())
		if e := a.merge(t.Context(), d, false); e != nil {
			t.Fatal(e)
		}
	}
	p, e := a.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	d, _ = a.Directory(t.Context())
	target := desiredTrust(d, true)
	if len(target.IPv4) != 1 {
		t.Fatal("共享 IP 未去重", target)
	}
	if _, e = a.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: false}); e == nil {
		t.Fatal("接受过期设置版本")
	}
	if e = b.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	q, e := b.ChangeTrust(t.Context(), TrustPolicyPatch{ExpectedVersion: p.Version, Enabled: false})
	if e != nil {
		t.Fatal(e)
	}
	db, _ := b.Directory(t.Context())
	if e = a.merge(t.Context(), db, false); e != nil {
		t.Fatal(e)
	}
	if e = a.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	read, _ := a.TrustPolicy(t.Context())
	if read != q {
		t.Fatal("旧版本恢复了互信", read, q)
	}
	if e = a.Revoke(t.Context(), b.ID); e != nil {
		t.Fatal(e)
	}
	d, _ = a.Directory(t.Context())
	if len(desiredTrust(d, true).IPv4) != 1 {
		t.Fatal("撤销误删共享 IP")
	}
	if e = a.Revoke(t.Context(), c.ID); e != nil {
		t.Fatal(e)
	}
	d, _ = a.Directory(t.Context())
	if len(desiredTrust(d, true).IPv4) != 0 {
		t.Fatal("撤销后仍信任旧 IP")
	}
}
func TestAddressConflictFailureAndManualOverride(t *testing.T) {
	n, _ := openTestNode(t)
	now := time.Now().UTC()
	n.ObservePublic = func(_ context.Context, f protocol.AddressFamily) []protocol.AddressObservation {
		ip := "8.8.8.8"
		if f == protocol.IPv6 {
			ip = "2606:4700::1111"
		}
		return []protocol.AddressObservation{{Source: "peer", Address: ip, ObservedAt: now}}
	}
	v, e := n.RefreshPublic(t.Context())
	if e != nil || v.IPv4.Addresses[0] != "8.8.8.8" {
		t.Fatal(v, e)
	}
	n.ObservePublic = func(context.Context, protocol.AddressFamily) []protocol.AddressObservation {
		return []protocol.AddressObservation{{Source: "peer", Address: "1.1.1.1", ObservedAt: now}, {Source: "ipify", Address: "9.9.9.9", ObservedAt: now}}
	}
	v, e = n.RefreshPublic(t.Context())
	if e != nil || v.IPv4.State != "conflict" || v.IPv4.Addresses[0] != "8.8.8.8" {
		t.Fatal(v, e)
	}
	n.ObservePublic = func(context.Context, protocol.AddressFamily) []protocol.AddressObservation { return nil }
	v, e = n.RefreshPublic(t.Context())
	if e != nil || v.IPv4.State != "failed" || v.IPv4.Addresses[0] != "8.8.8.8" {
		t.Fatal("观测失败未保留已确认地址", v, e)
	}
	manualAddress(t, n, "9.9.9.9")
	v, e = n.RefreshPublic(t.Context())
	if e != nil || v.IPv4.Addresses[0] != "9.9.9.9" || v.IPv4.Mode != "manual" {
		t.Fatal(v, e)
	}
}
func TestSignedNetworkRecordsRejectTamperingAndConcurrentEnableLoses(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	manualAddress(t, b, "8.8.8.8")
	d, _ := b.Directory(t.Context())
	d.Addresses[0].IPv4.Addresses = []string{"1.1.1.1"}
	if e := a.merge(t.Context(), d, false); e == nil {
		t.Fatal("接受篡改公网地址")
	}
	_, e := a.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	p := TrustPolicy{Cluster: a.cluster, TrustPolicyState: TrustPolicyState{Enabled: false, Version: TrustVersion{Counter: 1, NodeID: b.ID}}}
	p.Signature = ed25519.Sign(b.signing, trustBytes(p))
	d, _ = b.Directory(t.Context())
	d.Trust = &p
	if e = a.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	q, _ := a.TrustPolicy(t.Context())
	if q.Enabled {
		t.Fatal("并发开启覆盖了关闭")
	}
}
