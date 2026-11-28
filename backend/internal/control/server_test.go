package control

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestSessionCookieTransport(t *testing.T) {
	for _, tc := range []struct {
		name, proxyIP string
		tls, proxy    bool
	}{
		{name: "直连 HTTP 忽略其他来源的转发头", proxyIP: "127.0.0.2"},
		{name: "直连 HTTPS", tls: true},
		{name: "代理终止 HTTPS", proxyIP: "127.0.0.1", tls: true, proxy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const password = "test-password"
			n, err := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Password: password, Username: "admin", Entrance: "/signin"})
			if err != nil {
				t.Fatal(err)
			}
			defer n.Close()
			proxyIP, _ := netip.ParseAddr(tc.proxyIP)
			s, err := New(t.Context(), n.DB, Options{Node: n, TrustedProxyIP: proxyIP})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			handler := s.Handler()
			if tc.proxy {
				upstream := httptest.NewServer(handler)
				defer upstream.Close()
				target, _ := url.Parse(upstream.URL)
				handler = &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
					r.SetURL(target)
					r.Out.Host = r.In.Host
					r.SetXForwarded()
				}}
			}
			web := httptest.NewUnstartedServer(handler)
			if tc.tls {
				web.StartTLS()
			} else {
				web.Start()
			}
			defer web.Close()
			client := web.Client()
			client.Jar, _ = cookiejar.New(nil)
			request := func(method, path, body, csrf string) *http.Response {
				t.Helper()
				r, err := http.NewRequestWithContext(t.Context(), method, web.URL+httpapi.APIBasePath+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Origin", web.URL)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				if !tc.tls {
					r.Header.Set("X-Forwarded-Proto", "https")
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				return response
			}
			login := request(http.MethodPost, "/auth/sessions", `{"username":"admin","password":"`+password+`","entrance":"/signin"}`, "")
			defer login.Body.Close()
			cookies := login.Cookies()
			if login.StatusCode != http.StatusCreated || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Secure != tc.tls {
				t.Fatalf("登录状态或 Cookie 属性不符，HTTP %d", login.StatusCode)
			}
			var session httpapi.CurrentSession
			if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
				t.Fatal(err)
			}
			blocked := request(http.MethodDelete, "/auth/sessions/current", "", "")
			blocked.Body.Close()
			if blocked.StatusCode != http.StatusForbidden {
				t.Fatalf("缺少 CSRF 时应拒绝，HTTP %d", blocked.StatusCode)
			}
			logout := request(http.MethodDelete, "/auth/sessions/current", "", session.CsrfToken)
			logout.Body.Close()
			cookies = logout.Cookies()
			if logout.StatusCode != http.StatusNoContent || len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Secure != tc.tls {
				t.Fatalf("退出状态或清除 Cookie 属性不符，HTTP %d", logout.StatusCode)
			}
		})
	}
}

func TestConsoleAuthTargetQueueAndAccount(t *testing.T) {
	n, e := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Password: "test-password", Username: "admin", Entrance: "/signin"})
	if e != nil {
		t.Fatal(e)
	}
	registerNode(t, n)
	defer n.Close()
	n.Start()
	s, e := New(t.Context(), n.DB, Options{Node: n})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	web := httptest.NewServer(s.Handler())
	defer web.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	var csrf string
	request := func(method, path string, body any, out any) int {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		r, e := http.NewRequest(method, web.URL+"/api/v2"+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", web.URL)
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.Header.Set("Idempotency-Key", "configuration-"+strings.ReplaceAll(path, "/", "-"))
		prepareTestRequest(t, client, r)
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		validateTestResponse(t, r, response)
		defer response.Body.Close()
		if out != nil && response.StatusCode < 400 {
			if e = json.NewDecoder(response.Body).Decode(out); e != nil {
				t.Fatal(e)
			}
		}
		return response.StatusCode
	}
	if status := request("GET", "/nodes", nil, nil); status != 401 {
		t.Fatal(status)
	}
	password := "test-password"
	var session httpapi.CurrentSession
	if status := request("POST", "/auth/sessions", httpapi.LoginRequest{Username: "admin", Password: &password, Entrance: "/signin"}, &session); status != 201 {
		t.Fatal(status)
	}
	csrf = session.CsrfToken
	var list httpapi.NodeList
	if status := request("GET", "/nodes", nil, &list); status != 200 || len(list.Items) != 1 || !list.Items[0].Local {
		t.Fatal(status, list)
	}
	var config cluster.Config
	if status := request("GET", "/nodes/"+n.ID+"/configuration", nil, &config); status != 200 || config.Console {
		t.Fatal(status, config)
	}
	config.Region = "东京"
	var accepted cluster.Operation
	if status := request("PATCH", "/nodes/"+n.ID+"/configuration", config, &accepted); status != 202 {
		t.Fatal(status)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, e := n.GetOperation(t.Context(), accepted.ID)
		if e == nil && op.State == protocol.Succeeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	saved, _ := n.Config(t.Context())
	if saved.Region != "东京" {
		t.Fatal(saved)
	}
	var revealed httpapi.NodePassword
	if status := request("POST", "/nodes/"+n.ID+"/account/actions", map[string]any{"action": "reveal_password", "expected_revision": 1}, &revealed); status != 200 || revealed.Password != "test-password" {
		t.Fatal(status)
	}
	newPassword := "new-password"
	currentPassword := "test-password"
	var changed httpapi.AccountChanged
	if status := request("PATCH", "/settings/account", httpapi.AccountChange{CurrentPassword: &currentPassword, Password: &newPassword}, &changed); status != 200 || changed.LoginAddress != "/signin" {
		t.Fatal(status, changed)
	}
	if status := request("GET", "/nodes", nil, nil); status != 401 {
		t.Fatal("旧会话未撤销", status)
	}
}

// grantAllCapabilities 让测试节点模拟"已配好授权的集群"。
func grantAllCapabilities(n *cluster.Node, origins ...string) {
	set := make(map[cluster.Capability]bool)
	for _, name := range cluster.CapabilityNames() {
		set[cluster.Capability(name)] = true
	}
	if n.Capabilities == nil {
		n.Capabilities = cluster.SourceCapabilities{}
	}
	for _, origin := range origins {
		n.Capabilities[origin] = set
	}
}

// authorizePeers 让给定节点两两互相拥有全部能力（按真实节点 ID 授权）。
func authorizePeers(nodes ...*cluster.Node) {
	for _, target := range nodes {
		for _, source := range nodes {
			if target != source {
				grantAllCapabilities(target, source.ID)
			}
		}
	}
}

// controlNodes 记录当前用例创建的节点，用于两两授权（按真实节点 ID）。
var controlNodes = map[*testing.T][]*cluster.Node{}

// registerNode 登记节点并重建两两授权，模拟"已配好授权的集群"。
func registerNode(t *testing.T, n *cluster.Node) {
	t.Helper()
	n.Log = t.Logf
	list := append(controlNodes[t], n)
	controlNodes[t] = list
	t.Cleanup(func() { delete(controlNodes, t) })
	authorizePeers(list...)
}
