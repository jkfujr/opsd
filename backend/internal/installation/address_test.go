package installation

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/cluster"
	"testing"
)

func TestNodeAddressUsesPublicAddressAndListenPort(t *testing.T) {
	old := discoverAddress
	t.Cleanup(func() { discoverAddress = old })
	for _, test := range []struct{ ip, want string }{{"8.8.8.8", "http://8.8.8.8:9444"}, {"2606:4700::1111", "http://[2606:4700::1111]:9444"}} {
		discoverAddress = func(context.Context) (string, error) { return test.ip, nil }
		got, e := NodeAddress(t.Context(), cluster.Config{Listen: ":9444"}, "http://1.1.1.1:8444")
		if e != nil || got != test.want {
			t.Fatal(got, e)
		}
	}
}
func TestNodeAddressPreservesOverrideAndRefusesUnknown(t *testing.T) {
	old := discoverAddress
	t.Cleanup(func() { discoverAddress = old })
	discoverAddress = func(context.Context) (string, error) {
		return "", fmt.Errorf("公网发现失败，请使用 --address")
	}
	want := "http://100.100.1.2:8444"
	got, e := NodeAddress(t.Context(), cluster.Config{Address: want}, "invalid")
	if e != nil || got != want {
		t.Fatal(got, e)
	}
	if _, e = NodeAddress(t.Context(), cluster.Config{Listen: ":8444"}, ""); e == nil {
		t.Fatal("发现失败仍继续安装")
	}
	if _, e = NodeAddress(t.Context(), cluster.Config{Listen: ":0"}, ""); e == nil {
		t.Fatal("接受随机监听端口")
	}
}
