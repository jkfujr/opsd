package control

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestConsoleAuthTargetQueueAndAccount(t *testing.T) {
	n, e := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Password: "test-password", Username: "admin", Entrance: "/signin"})
	if e != nil {
		t.Fatal(e)
	}
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
		r, e := http.NewRequest(method, web.URL+"/api/v1"+path, reader)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", web.URL)
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.Header.Set("Idempotency-Key", "configuration")
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
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
	if status := request("POST", "/auth/login", httpapi.LoginRequest{Username: "admin", Password: &password, Entrance: "/signin"}, &session); status != 200 {
		t.Fatal(status)
	}
	csrf = session.CsrfToken
	var list httpapi.NodeList
	if status := request("GET", "/nodes", nil, &list); status != 200 || len(list.Items) != 1 || !list.Items[0].Local {
		t.Fatal(status, list)
	}
	var config cluster.Config
	if status := request("GET", "/nodes/"+n.ID+"/settings", nil, &config); status != 200 || config.Console {
		t.Fatal(status, config)
	}
	config.Region = "东京"
	var accepted cluster.Operation
	if status := request("PATCH", "/nodes/"+n.ID+"/settings", config, &accepted); status != 202 {
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
	if status := request("POST", "/nodes/"+n.ID+"/account/password", nil, &revealed); status != 200 || revealed.Password != "test-password" {
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
