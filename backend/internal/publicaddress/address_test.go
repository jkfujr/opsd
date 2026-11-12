package publicaddress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicIPClassification(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111", "::ffff:8.8.8.8"} {
		if _, e := Parse(ip); e != nil {
			t.Fatalf("%s：%v", ip, e)
		}
	}
	for _, ip := range []string{"10.1.2.3", "100.100.201.91", "192.168.1.2", "127.0.0.1", "169.254.1.1", "192.0.2.1", "198.18.0.1", "255.255.255.255", "fd00::1", "fe80::1", "::1", "2001:db8::1", "8.8.8.8/24"} {
		if _, e := Parse(ip); e == nil {
			t.Fatalf("误接受 %s", ip)
		}
	}
}

func TestEchoBackupRetainsSourceFailureAndAddressFamily(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path == "/primary" {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write([]byte("2606:4700:4700::1111\n"))
	}))
	defer server.Close()
	result := observe(t.Context(), "ipv6", []string{server.URL + "/primary", server.URL + "/backup"}, server.Client())
	if len(result) != 2 || result[0].Error == nil || result[1].Source != "icanhazip" || result[1].Address != "2606:4700:4700::1111" || result[1].Error != nil || len(requests) != 2 {
		t.Fatal(result, requests)
	}
	wrong := observe(t.Context(), "ipv4", []string{server.URL + "/backup"}, server.Client())
	if len(wrong) != 1 || wrong[0].Error == nil {
		t.Fatal("接受了错误地址族", wrong)
	}
}

func TestEchoHonorsCallerDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := observe(ctx, "ipv4", []string{server.URL, server.URL}, server.Client())
	if len(result) != 1 || result[0].Error == nil || time.Since(start) > time.Second {
		t.Fatal("发现未遵守总期限", result)
	}
}
