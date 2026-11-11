package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

type noFirewallRead struct{ calls int }

func (x *noFirewallRead) Call(context.Context, protocol.Payload) (protocol.Payload, error) {
	x.calls++
	return nil, fmt.Errorf("普通请求不能调用执行器")
}
func (*noFirewallRead) Storage(context.Context, cluster.StorageConfig) (cluster.Credentials, error) {
	return cluster.Credentials{}, nil
}

func TestFirewallConfigHTTPReadsAndAtomicSave(t *testing.T) {
	x := &noFirewallRead{}
	n, err := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Username: "admin", Password: "test-password", Entrance: "/signin", Executor: x})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	definition := protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{{Family: protocol.IPv4, Action: "deny"}}}
	state := protocol.ManagedFirewallState{FirewallDefinition: definition, Revision: 1, ConfirmedRevision: 1, State: "in_sync"}
	body, _ := json.Marshal(state)
	confirmed, _ := json.Marshal(definition)
	if _, err = n.DB.Exec("INSERT INTO firewall_config(id,body,confirmed) VALUES(1,?,?)", string(body), string(confirmed)); err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), n.DB, Options{Node: n})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var durations []time.Duration
	handler := s.Handler()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		handler.ServeHTTP(w, r)
		if r.Method == "GET" && r.URL.Path == "/api/v2/nodes/"+n.ID+"/firewall/configuration" {
			durations = append(durations, time.Since(start))
		}
	}))
	defer web.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := ""
	request := func(method, path, key string, data, out any) int {
		t.Helper()
		b, _ := json.Marshal(data)
		r, err := http.NewRequest(method, web.URL+"/api/v2"+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", web.URL)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		prepareTestRequest(t, client, r)
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		validateTestResponse(t, r, res)
		defer res.Body.Close()
		if out != nil && res.StatusCode < 400 {
			if err = json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode
	}
	password := "test-password"
	var session httpapi.CurrentSession
	if code := request("POST", "/auth/sessions", "", httpapi.LoginRequest{Username: "admin", Password: &password, Entrance: "/signin"}, &session); code != 201 || session.LocalNodeId != n.ID {
		t.Fatal(code, session)
	}
	csrf = session.CsrfToken
	for range 25 {
		var result httpapi.ManagedFirewallState
		if code := request("GET", "/nodes/"+n.ID+"/firewall/configuration", "", nil, &result); code != 200 || result.Revision != 1 {
			t.Fatal(code, result)
		}
	}
	patch := protocol.FirewallConfigPatch{ExpectedRevision: 1, Change: protocol.FirewallChange{Kind: "set_default_policy", Action: "allow"}}
	var saved, again httpapi.Operation
	path := "/nodes/" + n.ID + "/firewall"
	if code := request("POST", path+"/changes", "same-request", patch, &saved); code != 202 || saved.State != "accepted" {
		t.Fatal(code, saved)
	}
	if code := request("POST", path+"/changes", "same-request", patch, &again); code != 202 || again.Id != saved.Id {
		t.Fatal(code, again)
	}
	if code := request("POST", path+"/changes", "stale-request", patch, nil); code != 409 {
		t.Fatal("旧配置应被拒绝", code)
	}
	if code := request("POST", path+"/actions", "retry", map[string]any{"action": "sync", "expected_revision": 2}, nil); code != 409 {
		t.Fatal("未结束操作不能再次执行", code)
	}
	if x.calls != 0 {
		t.Fatal("保存或读取同步调用了执行器", x.calls)
	}
	var configs, operations int
	if err = n.DB.QueryRow("SELECT count(*) FROM queue WHERE kind='firewall_config'").Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err = n.DB.QueryRow("SELECT count(*) FROM records WHERE kind='firewall'").Scan(&configs); err != nil {
		t.Fatal(err)
	}
	if operations != 1 || configs != 1 {
		t.Fatal("配置、操作及上传记录没有一次提交", operations, configs)
	}
	slices.Sort(durations)
	t.Logf("本机配置 GET 服务端耗时（25 次）：中位数 %s，P95 %s，最大 %s；执行器调用 %d", durations[12], durations[23], durations[24], x.calls)
}
