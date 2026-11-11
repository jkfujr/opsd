package control

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/consoletheme"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/webassets"
)

type themeConsole struct {
	server    *Server
	node      *cluster.Node
	web       *httptest.Server
	client    *http.Client
	csrf      string
	directory string
}

func openThemeConsole(t *testing.T, assets fs.FS) *themeConsole {
	t.Helper()
	directory := t.TempDir()
	n, err := cluster.Open(t.Context(), filepath.Join(directory, "node.db"), cluster.Options{Username: "admin", Password: "test-password", Entrance: "/signin", Config: cluster.Config{Name: "主题验收节点", Console: true, API: true}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), n.DB, Options{Node: n, Assets: assets, NodeVersion: "0.1.0-dev", ThemeDir: filepath.Join(directory, "themes"), EnforceListenerState: true})
	if err != nil {
		n.Close()
		t.Fatal(err)
	}
	web := httptest.NewServer(s.Handler())
	jar, _ := cookiejar.New(nil)
	c := &themeConsole{server: s, node: n, web: web, client: &http.Client{Jar: jar}, directory: directory}
	t.Cleanup(func() { web.Close(); s.Close(); n.Close() })
	return c
}

func (c *themeConsole) request(t *testing.T, method, path, media string, body []byte, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	r, err := http.NewRequest(method, c.web.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", c.web.URL)
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	prepareTestRequest(t, c.client, r)
	response, err := c.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	validateTestResponse(t, r, response)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data, response.Header
}

func (c *themeConsole) login(t *testing.T) {
	t.Helper()
	code, body, _ := c.request(t, "POST", "/api/v2/auth/sessions", "application/json", []byte(`{"username":"admin","password":"test-password","entrance":"/signin"}`), nil)
	var session httpapi.CurrentSession
	if code != 201 || json.Unmarshal(body, &session) != nil {
		t.Fatal(code, string(body))
	}
	c.csrf = session.CsrfToken
}

func themeArchive(t *testing.T, id, login string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	manifest := consoletheme.Manifest{FormatVersion: 1, ID: id, Name: "测试主题", Version: "1.0.0", APIVersion: 2, MinOPSDVersion: "0.1.0-dev", Login: login}
	data, _ := json.Marshal(manifest)
	for name, body := range map[string][]byte{"theme.json": data, "dist/index.html": []byte(`<!doctype html><html><head><title>主题页面</title></head><body><script type="module" src="assets/app.js"></script></body></html>`), "dist/assets/app.js": []byte(`console.log('theme-assets')`)} {
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestConsoleThemeHTTPAndAuthentication(t *testing.T) {
	c := openThemeConsole(t, fstest.MapFS{"index.html": {Data: []byte("<html><head></head><body>内置页面</body></html>")}, "assets/builtin.js": {Data: []byte("builtin-assets")}})
	for _, path := range []string{"/", "/custom/deep", consoletheme.BuiltinPath} {
		if code, _, _ := c.request(t, "GET", path, "", nil, nil); code != 404 {
			t.Fatal("未登录页面泄露", path, code)
		}
	}
	archive := themeArchive(t, "first", "builtin")
	if code, _, _ := c.request(t, "POST", "/api/v2/console/theme-installations", "application/zip", archive, nil); code != 401 {
		t.Fatal(code)
	}
	c.login(t)
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"Origin": "https://outside.example"}} {
		if code, _, _ := c.request(t, "POST", "/api/v2/console/theme-installations", "application/zip", archive, headers); code != 403 {
			t.Fatal("主题上传未检查同源和 CSRF", code)
		}
	}
	if code, _, _ := c.request(t, "POST", "/api/v2/console/theme-installations", "application/json", archive, nil); code != 400 {
		t.Fatal(code)
	}
	code, body, _ := c.request(t, "POST", "/api/v2/console/theme-installations", "application/zip", archive, nil)
	var first httpapi.ThemeInstallation
	if code != 201 || json.Unmarshal(body, &first) != nil || !first.Available {
		t.Fatal(code, string(body))
	}
	if code, _, _ := c.request(t, "POST", "/api/v2/console/theme-installations", "application/zip", archive, nil); code != 409 {
		t.Fatal("重复安装未拒绝", code)
	}
	if code, body, _ = c.request(t, "GET", "/", "", nil, nil); code != 200 || !bytes.Contains(body, []byte("内置页面")) {
		t.Fatal("安装不应自动启用", code, string(body))
	}
	activate, _ := json.Marshal(httpapi.ThemeSelection{InstallationId: first.InstallationId})
	if code, body, _ = c.request(t, "PUT", "/api/v2/console/theme-selection", "application/json", activate, nil); code != 200 {
		t.Fatal(code, string(body))
	}
	for _, path := range []string{"/", "/custom/deep/route"} {
		code, body, _ := c.request(t, "GET", path, "", nil, nil)
		if code != 200 || !bytes.Contains(body, []byte(`<base href="`+consoletheme.AssetBase(first.InstallationId)+`">`)) {
			t.Fatal(path, code, string(body))
		}
	}
	if code, body, _ = c.request(t, "GET", "/signin", "", nil, nil); code != 200 || !bytes.Contains(body, []byte("内置页面")) {
		t.Fatal("默认登录应使用内置", code, string(body))
	}
	asset := consoletheme.AssetBase(first.InstallationId) + "assets/app.js"
	code, body, header := c.request(t, "GET", asset, "", nil, nil)
	if code != 200 || !strings.Contains(header.Get("Content-Type"), "javascript") || !strings.Contains(header.Get("Cache-Control"), "immutable") || !bytes.Contains(body, []byte("theme-assets")) {
		t.Fatal(code, string(body), header)
	}
	for _, path := range []string{consoletheme.AssetBase(first.InstallationId) + "missing.js", consoletheme.AssetBase(first.InstallationId) + "assets/", "/api/v2/nonexistent", "/__opsd/unknown"} {
		if code, _, _ := c.request(t, "GET", path, "", nil, nil); code != 404 {
			t.Fatal("资源不能返回 SPA 入口", path, code)
		}
	}
	if code, _, _ := c.request(t, "DELETE", "/api/v2/console/theme-installations/"+first.InstallationId, "", nil, nil); code != 409 {
		t.Fatal("当前主题不能删除", code)
	}
	secondArchive := themeArchive(t, "second", "theme")
	code, body, _ = c.request(t, "POST", "/api/v2/console/theme-installations", "application/zip", secondArchive, nil)
	var second httpapi.ThemeInstallation
	if code != 201 || json.Unmarshal(body, &second) != nil {
		t.Fatal(code, string(body))
	}
	activate, _ = json.Marshal(httpapi.ThemeSelection{InstallationId: second.InstallationId})
	if code, _, _ := c.request(t, "PUT", "/api/v2/console/theme-selection", "application/json", activate, nil); code != 200 {
		t.Fatal(code)
	}
	if code, body, _ = c.request(t, "GET", "/signin", "", nil, nil); code != 200 || !bytes.Contains(body, []byte("主题页面")) {
		t.Fatal("主题登录未启用", code, string(body))
	}
	for _, path := range []string{"/signin?builtin=1", consoletheme.BuiltinPath, consoletheme.BuiltinPath + "settings/appearance"} {
		if code, body, _ = c.request(t, "GET", path, "", nil, nil); code != 200 || !bytes.Contains(body, []byte("内置页面")) {
			t.Fatal("恢复入口受主题影响", path, code, string(body))
		}
	}
	if code, body, _ = c.request(t, "GET", "/assets/builtin.js", "", nil, nil); code != 200 || string(body) != "builtin-assets" {
		t.Fatal("主题覆盖了内置资源", code, string(body))
	}
	if err := os.Remove(filepath.Join(c.directory, "themes", "installed", second.InstallationId, "dist", "assets", "app.js")); err != nil {
		t.Fatal(err)
	}
	if code, body, _ = c.request(t, "GET", "/custom/deep/route", "", nil, nil); code != 200 || !bytes.Contains(body, []byte("内置页面")) {
		t.Fatal("缺失资源未使用内置", code, string(body))
	}
	code, body, _ = c.request(t, "GET", "/api/v2/console/theme-installations", "", nil, nil)
	var list httpapi.ThemeList
	if code != 200 || json.Unmarshal(body, &list) != nil || list.ActiveInstallationId != second.InstallationId || list.EffectiveInstallationId != "" {
		t.Fatal(code, string(body))
	}

	var created httpapi.APITokenCreated
	input := map[string]any{"name": "主题权限检查", "enabled": true, "revision": 0, "grants": []any{map[string]any{"module": "nodes", "level": "manage", "scope": "all", "resources": []string{}}}, "source_ips": []string{}}
	if code := apiCall(t, c.client, c.web.URL, "POST", "/api/v2/nodes/"+c.node.ID+"/api-tokens", "", "", c.csrf, input, &created); code != 201 {
		t.Fatal("创建测试令牌失败", code)
	}
	for _, method := range []string{"GET", "PUT"} {
		path := "/api/v2/console/theme-installations"
		if method == "PUT" {
			path = "/api/v2/console/theme-selection"
		}
		if code := apiCall(t, c.client, c.web.URL, method, path, created.Value, "", c.csrf, map[string]any{"installation_id": ""}, nil); code != 403 {
			t.Fatal("令牌不能管理主题", method, code)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"installation_id":null}`} {
		if code, _, _ := c.request(t, "PUT", "/api/v2/console/theme-selection", "application/json", []byte(body), nil); code != 422 {
			t.Fatal("未明确选择时不应切换主题", body, code)
		}
	}
	if code, _, _ := c.request(t, "PUT", "/api/v2/console/theme-selection", "application/json", []byte(`{"installation_id":"builtin"}`), nil); code != 200 {
		t.Fatal(code)
	}
	if code, _, _ := c.request(t, "DELETE", "/api/v2/console/theme-installations/"+second.InstallationId, "", nil, nil); code != 204 {
		t.Fatal(code)
	}
}

func TestBuiltinConsoleAssetsAndRoutes(t *testing.T) {
	assets := webassets.Assets()
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	c := openThemeConsole(t, assets)
	c.node.Start()
	routes := []string{"/tools", "/tools/tasks", "/tools/terminal", "/terminal?node=example#session", "/__opsd/builtin/tools/tasks"}
	for _, path := range routes {
		if code, _, _ := c.request(t, "GET", path, "", nil, nil); code != 404 {
			t.Fatal("未登录页面泄露", path, code)
		}
	}
	c.login(t)
	for _, path := range routes {
		if code, body, _ := c.request(t, "GET", path, "", nil, nil); code != 200 || !bytes.Equal(body, index) {
			t.Fatal("内置页面不可用", path, code)
		}
	}
	if code, _, _ := c.request(t, "GET", "/tools/missing", "", nil, nil); code != 404 {
		t.Fatal("未知工具路径未拒绝", code)
	}
	references := regexp.MustCompile(`(?:src|href)="(/assets/[^"\s]+)"`).FindAllSubmatch(index, -1)
	if len(references) == 0 || bytes.Contains(index, []byte(`="./assets/`)) {
		t.Fatal("内置构建未使用绝对资源地址")
	}
	for _, reference := range references {
		path := string(reference[1])
		content, err := fs.ReadFile(assets, strings.TrimPrefix(path, "/"))
		if err != nil {
			t.Fatal(path, err)
		}
		if code, body, header := c.request(t, "GET", path, "", nil, nil); code != 200 || !bytes.Equal(body, content) || !strings.Contains(header.Get("Cache-Control"), "immutable") {
			t.Fatal("内置资源读取不一致", path, code)
		}
	}
	if code, _, _ := c.request(t, "GET", "/assets/missing.js", "", nil, nil); code != 404 {
		t.Fatal("缺失资源不能返回页面", code)
	}
	if code, body, _ := c.request(t, "HEAD", "/tools/tasks", "", nil, nil); code != 200 || len(body) != 0 {
		t.Fatal("HEAD 页面应无正文", code)
	}
	for _, enabled := range []bool{false, true} {
		config, err := c.node.Config(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		config.Console = enabled
		payload, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		operation, err := c.node.Submit(t.Context(), c.node.ID, cluster.Request{ID: cluster.NewOperationID(c.node.ID), Kind: "config", Payload: payload})
		if err != nil {
			t.Fatal("控制台配置未受理", err)
		}
		waitAPIOperation(t, c.node, operation.ID)
		want := 404
		if enabled {
			want = 200
		}
		for _, path := range []string{"/signin", "/tools/tasks", string(references[0][1])} {
			if code, _, _ := c.request(t, "GET", path, "", nil, nil); code != want {
				t.Fatal("控制台启停状态不符", enabled, path, code)
			}
		}
	}
}

func TestThemeDirectoryFailureKeepsBuiltinConsole(t *testing.T) {
	directory := t.TempDir()
	n, err := cluster.Open(t.Context(), filepath.Join(directory, "node.db"), cluster.Options{Username: "admin", Password: "test-password", Entrance: "/signin"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	broken := filepath.Join(directory, "themes")
	if err = os.WriteFile(broken, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), n.DB, Options{Node: n, Assets: fstest.MapFS{"index.html": {Data: []byte("builtin-available")}}, ThemeDir: broken, NodeVersion: "0.1.0-dev"})
	if err != nil {
		t.Fatal("主题目录错误不应停止控制台", err)
	}
	defer s.Close()
	request := httptest.NewRequest("GET", "http://localhost/signin?builtin=1", nil)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "builtin-available" {
		t.Fatal(response.Code, response.Body.String())
	}
	if err = s.themes.Activate(t.Context(), ""); err != nil {
		t.Fatal("主题目录错误不应阻止切回内置", err)
	}
}
