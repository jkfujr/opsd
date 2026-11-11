package consoletheme

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (r rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	u := *request.URL
	u.Scheme, u.Host = r.target.Scheme, r.target.Host
	copy.URL = &u
	return r.base.RoundTrip(copy)
}

func TestGitHubReleaseSources(t *testing.T) {
	m := openManager(t)
	archive := makeArchive(t, manifest())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Error("Release 请求缺少 JSON Accept")
			}
			tag := "v1.0.0"
			if strings.Contains(r.URL.Path, "/tags/") {
				tag = strings.SplitN(r.URL.Path, "/tags/", 2)[1]
			}
			if tag == "missing" {
				fmt.Fprint(w, `{"tag_name":"missing","assets":[]}`)
				return
			}
			if tag == "mismatch" {
				fmt.Fprint(w, `{"tag_name":"other","assets":[]}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []any{map[string]any{"name": "opsd-theme.zip", "browser_download_url": "https://github.com/example/theme/releases/download/v1.0.0/opsd-theme.zip", "size": len(archive)}}})
			return
		}
		if r.URL.Path != "/example/theme/releases/download/v1.0.0/opsd-theme.zip" {
			t.Error("下载地址不符", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/octet-stream" {
			t.Error("主题包请求不应要求 JSON")
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Write(archive)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	m.client = &http.Client{Transport: rewriteTransport{target, http.DefaultTransport}, CheckRedirect: downloadClient().CheckRedirect}
	item, err := m.InstallGitHub(t.Context(), "https://github.com/example/theme.git", "")
	if err != nil || !item.Available || item.Source != "https://github.com/example/theme/releases/tag/v1.0.0" || calls.Load() != 2 {
		t.Fatal(item, err, calls.Load())
	}
	if err = m.Delete(t.Context(), item.InstallationID); err != nil {
		t.Fatal(err)
	}
	item, err = m.InstallGitHub(t.Context(), "https://github.com/example/theme/", "v2/preview")
	if err != nil || !strings.HasSuffix(item.Source, "v2%2Fpreview") {
		t.Fatal(item, err)
	}
	for _, tag := range []string{"missing", "mismatch"} {
		if _, err = m.InstallGitHub(t.Context(), "https://github.com/example/theme", tag); errorCode(err) != "invalid_request" {
			t.Fatal(tag, err)
		}
	}
	before := calls.Load()
	for _, address := range []string{"http://github.com/example/theme", "https://gitlab.com/example/theme", "https://github.com:443/example/theme", "https://user@github.com/example/theme", "https://github.com/example/theme?token=secret", "https://github.com/example/theme/tree/main", "file:///tmp/theme", "https://127.0.0.1/example/theme"} {
		if _, err = m.InstallGitHub(t.Context(), address, ""); errorCode(err) != "invalid_request" {
			t.Fatal(address, err)
		}
	}
	if calls.Load() != before {
		t.Fatal("无效仓库地址触发了网络请求")
	}
}

func TestDownloadAddressesAndRedirects(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.100.201.1", "198.18.0.1", "0.1.2.3", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1"} {
		if publicAddress(netip.MustParseAddr(value)) {
			t.Fatal("非公网地址被允许", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(value)) {
			t.Fatal(value)
		}
	}
	client := downloadClient()
	defer client.CloseIdleConnections()
	for _, address := range []string{"http://github.com/file", "https://127.0.0.1/file", "https://example.com/file", "https://github.com.evil.example/file", "https://user@github.com/file", "https://github.com:8443/file"} {
		u, _ := url.Parse(address)
		if client.CheckRedirect(&http.Request{URL: u}, nil) == nil {
			t.Fatal("不应允许下载重定向", address)
		}
	}
	for _, address := range []string{"https://github.com/example/theme", "https://release-assets.githubusercontent.com/asset"} {
		u, _ := url.Parse(address)
		if err := client.CheckRedirect(&http.Request{URL: u}, nil); err != nil {
			t.Fatal(address, err)
		}
	}
}
