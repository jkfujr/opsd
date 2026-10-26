package control

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/storage"
)

type fixture struct {
	t       *testing.T
	db      *sql.DB
	path    string
	service *Server
	server  *httptest.Server
	client  *http.Client
	clock   atomic.Int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "control.db")}
	f.clock.Store(time.Now().UTC().Truncate(time.Second).UnixNano())
	var err error
	f.db, err = storage.Open(t.Context(), f.path, storage.Control)
	if err != nil {
		t.Fatal(err)
	}
	if err := Initialize(t.Context(), f.db, "admin", "test-password", "/secret-test", "", ""); err != nil {
		t.Fatal(err)
	}
	f.start()
	jar, _ := cookiejar.New(nil)
	f.client = &http.Client{Jar: jar}
	t.Cleanup(func() { f.service.Close(); f.server.Close(); f.db.Close() })
	return f
}

func (f *fixture) start() {
	f.t.Helper()
	var err error
	f.service, err = New(context.Background(), f.db, Options{Assets: fstest.MapFS{"index.html": {Data: []byte("界面入口")}}, Now: func() time.Time { return time.Unix(0, f.clock.Load()) }})
	if err != nil {
		f.t.Fatal(err)
	}
	f.server = httptest.NewServer(f.service.Handler())
}

func (f *fixture) request(client *http.Client, method, path, body, csrf string, origin bool) (*http.Response, []byte) {
	f.t.Helper()
	request, err := http.NewRequestWithContext(f.t.Context(), method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "203.0.113.200")
	if origin {
		request.Header.Set("Origin", f.server.URL)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return response, data
}

func expect(t *testing.T, response *http.Response, data []byte, status int) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("状态应为 %d，实际 %d：%s", status, response.StatusCode, data)
	}
}

func (f *fixture) login(client *http.Client, entrance, username, password string) httpapi.CurrentSession {
	f.t.Helper()
	body, _ := json.Marshal(map[string]string{"entrance": entrance, "username": username, "password": password})
	response, data := f.request(client, "POST", "/api/v1/auth/login", string(body), "", true)
	expect(f.t, response, data, 200)
	var session httpapi.CurrentSession
	if err := json.Unmarshal(data, &session); err != nil {
		f.t.Fatal(err)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].MaxAge != 604800 {
		f.t.Fatalf("会话 Cookie 属性错误：%v", cookies)
	}
	if session.LoginAddress != entrance || session.CsrfToken == "" || session.Session.ExpiresAt.Sub(session.Session.CreatedAt) != 7*24*time.Hour || session.Session.RemoteAddress != "127.0.0.1" {
		f.t.Fatalf("会话期限、来源或入口错误：%+v", session)
	}
	return session
}

func TestHTTPAccountAndSessionLifecycle(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/", "/wrong-entrance", "/settings/account"} {
		response, data := f.request(f.client, "GET", path, "", "", false)
		expect(t, response, data, 404)
		if bytes.Contains(data, []byte("/secret-test")) {
			t.Fatal("未认证响应泄露入口")
		}
	}
	response, data := f.request(f.client, "GET", "/secret-test", "", "", false)
	expect(t, response, data, 200)
	response, data = f.request(f.client, "POST", "/api/v1/auth/login", `{"entrance":"/wrong-test","username":"admin","password":"test-password"}`, "", true)
	expect(t, response, data, 404)
	response, data = f.request(f.client, "POST", "/api/v1/auth/login", `{"entrance":"/secret-test","username":"admin","password":"test-password"}`, "", false)
	expect(t, response, data, 403)
	first := f.login(f.client, "/secret-test", "admin", "test-password")
	response, data = f.request(f.client, "POST", "/api/v1/enrollment-tokens", "", "", true)
	expect(t, response, data, 403)
	response, data = f.request(f.client, "POST", "/api/v1/enrollment-tokens", "", first.CsrfToken, false)
	expect(t, response, data, 403)
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar}
	second := f.login(other, "/secret-test", "admin", "test-password")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", f.server.URL+"/api/v1/events", nil)
	stream, err := other.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stream.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\n" {
			break
		}
	}
	response, data = f.request(f.client, "DELETE", "/api/v1/auth/sessions/"+second.Session.Id, "", first.CsrfToken, true)
	expect(t, response, data, 204)
	if _, err := reader.ReadString('\n'); err != io.EOF {
		t.Fatalf("撤销后事件连接未关闭：%v", err)
	}
	stream.Body.Close()
	response, data = f.request(other, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 401)
	response, data = f.request(f.client, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 200)
	response, data = f.request(f.client, "PATCH", "/api/v1/settings/account", `{"current_password":"wrong","username":"owner"}`, first.CsrfToken, true)
	expect(t, response, data, 403)
	response, data = f.request(f.client, "PATCH", "/api/v1/settings/account", `{"current_password":"test-password","username":"owner","password":"changed-password","entrance":"/new-secret"}`, first.CsrfToken, true)
	expect(t, response, data, 200)
	var changed httpapi.AccountChanged
	if err := json.Unmarshal(data, &changed); err != nil || changed.LoginAddress != "/new-secret" {
		t.Fatalf("入口变更结果错误：%s", data)
	}
	response, data = f.request(f.client, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 401)
	response, data = f.request(f.client, "GET", "/secret-test", "", "", false)
	expect(t, response, data, 404)
	f.login(f.client, "/new-secret", "owner", "changed-password")
	key := append([]byte(nil), f.service.key.Public...)
	f.service.Close()
	f.server.Close()
	f.db.Close()
	f.db, err = storage.Open(t.Context(), f.path, storage.Control)
	if err != nil {
		t.Fatal(err)
	}
	f.start()
	if !bytes.Equal(key, f.service.key.Public) {
		t.Fatal("重启更换了控制端身份")
	}
	response, data = f.request(f.client, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 200)
	response, data = f.request(other, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 401)
}

func TestLoginRateLimitAndAbsoluteExpiry(t *testing.T) {
	f := setup(t)
	for range 5 {
		response, data := f.request(f.client, "POST", "/api/v1/auth/login", `{"entrance":"/secret-test","username":"admin","password":"bad"}`, "", true)
		expect(t, response, data, 401)
	}
	response, data := f.request(f.client, "POST", "/api/v1/auth/login", `{"entrance":"/secret-test","username":"admin","password":"test-password"}`, "", true)
	expect(t, response, data, 429)
	f.clock.Add(int64(61 * time.Second))
	session := f.login(f.client, "/secret-test", "admin", "test-password")
	f.clock.Add(int64(6 * 24 * time.Hour))
	response, data = f.request(f.client, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 200)
	f.clock.Store(session.Session.ExpiresAt.UnixNano())
	response, data = f.request(f.client, "GET", "/api/v1/auth/session", "", "", false)
	expect(t, response, data, 401)
	f.service.passwordSlots <- struct{}{}
	f.service.passwordSlots <- struct{}{}
	response, data = f.request(f.client, "POST", "/api/v1/auth/login", `{"entrance":"/secret-test","username":"admin","password":"test-password"}`, "", true)
	expect(t, response, data, 429)
	<-f.service.passwordSlots
	<-f.service.passwordSlots
}
