package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestPublicEchoTriesPublicCandidatesAndRetainsPeerFailure(t *testing.T) {
	d := Directory{Members: []Member{{ID: "a", Name: "成员 A", Address: "http://100.100.201.1:8444"}, {ID: "b", Name: "成员 B", Address: "http://100.100.201.2:8444"}}, Addresses: []AddressRecord{{PublicAddresses: PublicAddresses{NodeID: "a", IPv4: PublicFamily{Addresses: []string{"8.8.8.8"}}}}, {PublicAddresses: PublicAddresses{NodeID: "b", IPv4: PublicFamily{Addresses: []string{"1.1.1.1"}}}}}}
	calls := 0
	probe := func(ctx context.Context, member Member, endpoint string, _ protocol.AddressFamily) (string, error) {
		calls++
		if strings.Contains(endpoint, "100.100.") {
			t.Fatal("公网回显使用了组网地址", endpoint)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2100*time.Millisecond {
			t.Fatal("缺少成员独立期限")
		}
		if member.ID == "a" {
			return "", errors.New("连接来源未取得公网地址")
		}
		return "9.9.9.9", nil
	}
	got, err := probePublicCandidates(t.Context(), protocol.IPv4, "self", d, probe)
	if err != nil || got != "9.9.9.9" || calls != 2 {
		t.Fatal(got, calls, err)
	}
	_, err = probePublicCandidates(t.Context(), protocol.IPv4, "self", d, func(context.Context, Member, string, protocol.AddressFamily) (string, error) {
		return "", errors.New("连接来源未取得公网地址")
	})
	if err == nil || !strings.Contains(err.Error(), "连接来源未取得公网地址") {
		t.Fatal("成员错误被覆盖", err)
	}
	for i := range 5 {
		d.Members = append(d.Members, Member{ID: string(rune('c' + i)), Name: "其他成员", Address: "http://9.9.9.9:8444"})
	}
	calls = 0
	_, _ = probePublicCandidates(t.Context(), protocol.IPv4, "self", d, func(context.Context, Member, string, protocol.AddressFamily) (string, error) {
		calls++
		return "", errors.New("超时")
	})
	if calls != 3 {
		t.Fatal("成员尝试没有上限", calls)
	}
}

func TestNetworkConfigurationRejectsUnreachableBindingAndAllowsNAT(t *testing.T) {
	for _, c := range []Config{{Listen: "100.100.201.41:8444", Address: "http://108.165.123.22:8444"}, {Listen: "127.0.0.1:8444", Address: "http://8.8.8.8:8444"}, {Listen: "0.0.0.0:8444", Address: "http://[2606:4700::1111]:8444"}} {
		var faultValue *Fault
		if !errors.As(networkConfigurationError(c), &faultValue) || faultValue.Code != "restart_required" {
			t.Fatal("未明确返回重启要求", c)
		}
	}
	for _, c := range []Config{{Listen: ":8444", Address: "http://8.8.8.8:8444"}, {Listen: "10.0.0.2:8444", Address: "http://8.8.8.8:8444"}, {Listen: "100.100.201.41:8444", Address: "http://100.100.201.41:8444"}} {
		if err := networkConfigurationError(c); err != nil {
			t.Fatal("拒绝了有效监听或 NAT 配置", c, err)
		}
	}
}

func TestPublicDiscoveryFailurePreservesConfirmationTime(t *testing.T) {
	before := time.Now().Add(-time.Hour).UTC()
	value := PublicFamily{Mode: "auto", State: "confirmed", Addresses: []string{"8.8.8.8"}, UpdatedAt: &before}
	failed := reconcileObservation(value, []protocol.AddressObservation{{Source: "ipify", Error: &protocol.Error{Code: "dependency_unavailable", Message: "连接超时"}}}, time.Now())
	if failed.State != "failed" || !failed.UpdatedAt.Equal(before) || failed.Addresses[0] != "8.8.8.8" {
		t.Fatal(failed)
	}
}
