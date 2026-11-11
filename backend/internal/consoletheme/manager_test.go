package consoletheme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jkfujr/opsd/internal/storage"
)

const testVersion = "0.1.0-dev"

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
	size uint64
}

func manifest() Manifest {
	return Manifest{FormatVersion: 1, ID: "example", Name: "示例主题", Version: "1.0.0", APIVersion: 2, MinOPSDVersion: testVersion}
}

func makeArchive(t *testing.T, m Manifest, extra ...zipEntry) []byte {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return archiveEntries(t, append([]zipEntry{{name: "theme.json", body: string(data)}, {name: "dist/index.html", body: `<!doctype html><HTML><HEAD data-title="示例"></HEAD><body><script type="module" src="assets/app.js"></script></body></HTML>`}, {name: "dist/assets/app.js", body: "window.theme = 'original';"}}, extra...))
}

func archiveEntries(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		var file io.Writer
		var err error
		if entry.size != 0 {
			header.UncompressedSize64 = entry.size
			file, err = w.CreateRaw(header)
		} else {
			file, err = w.CreateHeader(header)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openManager(t *testing.T) *Manager {
	t.Helper()
	directory := t.TempDir()
	db, err := storage.Open(t.Context(), filepath.Join(directory, "node.db"), storage.Node)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := Open(t.Context(), db, filepath.Join(directory, "themes"), testVersion)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func errorCode(err error) string {
	var fault *Error
	if errors.As(err, &fault) {
		return fault.Code
	}
	return ""
}

func TestInstalledAPIV1ThemeKeepsSelectionAndFallsBack(t *testing.T) {
	m := openManager(t)
	item, err := m.Install(t.Context(), bytes.NewReader(makeArchive(t, manifest())), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(t.Context(), item.InstallationID); err != nil {
		t.Fatal(err)
	}
	legacy := item.Manifest
	legacy.APIVersion = 1
	data, _ := json.Marshal(legacy)
	if _, err = m.db.ExecContext(t.Context(), "UPDATE console_themes SET manifest_json=? WHERE installation_id=?", string(data), item.InstallationID); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(t.Context())
	if err != nil || len(list.Items) != 1 || list.ActiveInstallationID != item.InstallationID || list.EffectiveInstallationID != "" || list.Items[0].Available || list.Items[0].Problem == "" {
		t.Fatalf("旧主题记录或内置恢复无效：%+v %v", list, err)
	}
	if _, index, err := m.ActiveIndex(t.Context()); err != nil || len(index) != 0 {
		t.Fatal("API v1 主题仍被加载", err)
	}
}

func TestInstallSelectionResourcesAndReopen(t *testing.T) {
	m := openManager(t)
	archive := makeArchive(t, manifest())
	item, err := m.Install(t.Context(), bytes.NewReader(archive), "upload")
	if err != nil || !item.Available || item.Manifest.Login != "builtin" {
		t.Fatal(item, err)
	}
	list, err := m.List(t.Context())
	if err != nil || len(list.Items) != 1 || list.ActiveInstallationID != "" {
		t.Fatal(list, err)
	}
	if _, err = m.Install(t.Context(), bytes.NewReader(archive), "upload"); errorCode(err) != "conflict" {
		t.Fatal(err)
	}
	alias := manifest()
	alias.Version = "v1.0.0"
	if _, err = m.Install(t.Context(), bytes.NewReader(makeArchive(t, alias)), "upload"); errorCode(err) != "conflict" {
		t.Fatal("版本前缀不应绕过重复检查", err)
	}
	if err = m.Activate(t.Context(), item.InstallationID); err != nil {
		t.Fatal(err)
	}
	if err = m.Delete(t.Context(), item.InstallationID); errorCode(err) != "conflict" {
		t.Fatal(err)
	}
	_, index, err := m.ActiveIndex(t.Context())
	if err != nil || !bytes.Contains(index, []byte(`<base href="`+AssetBase(item.InstallationID)+`">`)) {
		t.Fatal(string(index), err)
	}
	if !bytes.Contains(index, []byte(`src="assets/app.js"`)) {
		t.Fatal("入口脚本不应被改写", string(index))
	}
	for _, name := range []string{"../theme.json", "/theme.json", "assets/../../node.db", "assets\\app.js", "assets"} {
		f, err := m.OpenAsset(t.Context(), item.InstallationID, name)
		if f != nil {
			f.Close()
		}
		if err == nil {
			t.Fatal("不应提供资源", name)
		}
	}
	f, err := m.OpenAsset(t.Context(), item.InstallationID, "assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(content) != "window.theme = 'original';" {
		t.Fatal(string(content), err)
	}

	var database string
	if err = m.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&database); err != nil {
		t.Fatal(err)
	}
	if err = m.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.Context(), database, storage.Node)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err = Open(t.Context(), db, m.root, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	list, err = m.List(t.Context())
	if err != nil || list.ActiveInstallationID != item.InstallationID || list.EffectiveInstallationID != item.InstallationID {
		t.Fatal(list, err)
	}

	second := manifest()
	second.Version = "2.0.0"
	second.Login = "theme"
	other, err := m.Install(t.Context(), bytes.NewReader(makeArchive(t, second)), "upload")
	if err != nil || other.InstallationID == item.InstallationID {
		t.Fatal(other, err)
	}
	if err = m.Delete(t.Context(), other.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.OpenAsset(t.Context(), other.InstallationID, "assets/app.js"); err == nil {
		t.Fatal("删除后不应再提供旧安装的资源")
	}
	if err = os.Remove(filepath.Join(m.directory(item.InstallationID), "dist", "assets", "app.js")); err != nil {
		t.Fatal(err)
	}
	_, index, err = m.ActiveIndex(t.Context())
	if err != nil || index != nil {
		t.Fatal("主题文件缺失应交给内置界面", string(index), err)
	}
	list, err = m.List(t.Context())
	if err != nil || list.EffectiveInstallationID != "" || list.Items[0].Available || list.Items[0].Problem == "" {
		t.Fatal(list, err)
	}
	if err = m.Activate(t.Context(), item.InstallationID); errorCode(err) != "invalid_request" {
		t.Fatal(err)
	}
	if err = m.Activate(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if err = m.Delete(t.Context(), item.InstallationID); err != nil {
		t.Fatal(err)
	}
}

func TestRejectArchivesAndPreserveCurrentSelection(t *testing.T) {
	m := openManager(t)
	good, err := m.Install(t.Context(), bytes.NewReader(makeArchive(t, manifest())), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(t.Context(), good.InstallationID); err != nil {
		t.Fatal(err)
	}
	base := manifest()
	base.ID = "candidate"
	valid, _ := json.Marshal(base)
	core := []zipEntry{{name: "theme.json", body: string(valid)}, {name: "dist/index.html", body: "<html><head></head><body>测试</body></html>"}}
	cases := map[string][]byte{
		"非 ZIP":     []byte("not a zip"),
		"无清单":       archiveEntries(t, core[1:]),
		"无入口":       archiveEntries(t, core[:1]),
		"重复清单":      archiveEntries(t, append(append([]zipEntry{}, core...), core[0])),
		"大小写冲突":     makeArchive(t, base, zipEntry{name: "dist/A.js"}, zipEntry{name: "dist/a.js"}),
		"目录穿越":      makeArchive(t, base, zipEntry{name: "dist/../../escape"}),
		"绝对路径":      makeArchive(t, base, zipEntry{name: "/escape"}),
		"反斜杠":       makeArchive(t, base, zipEntry{name: `dist\escape`}),
		"设备路径":      makeArchive(t, base, zipEntry{name: "dist/NUL.txt"}),
		"符号链接":      makeArchive(t, base, zipEntry{name: "dist/link", body: "../../outside", mode: fs.ModeSymlink | 0777}),
		"设备文件":      makeArchive(t, base, zipEntry{name: "dist/device", mode: fs.ModeDevice | 0600}),
		"超大文件":      makeArchive(t, base, zipEntry{name: "dist/large", size: maxFileSize + 1}),
		"文件目录冲突先文件": makeArchive(t, base, zipEntry{name: "dist/conflict"}, zipEntry{name: "dist/conflict/app.js"}),
		"文件目录冲突后文件": makeArchive(t, base, zipEntry{name: "dist/conflict/app.js"}, zipEntry{name: "dist/conflict"}),
		"越界预览":      nil,
		"自设基址":      archiveEntries(t, []zipEntry{core[0], {name: "dist/index.html", body: `<head><base href="/"></head>`}}),
		"缺少 head":   archiveEntries(t, []zipEntry{core[0], {name: "dist/index.html", body: `<html><body>测试</body></html>`}}),
	}
	for name, change := range map[string]func(*Manifest){
		"格式版本":    func(m *Manifest) { m.FormatVersion = 2 },
		"API 版本":  func(m *Manifest) { m.APIVersion = 1 },
		"最低版本":    func(m *Manifest) { m.MinOPSDVersion = "99.0.0" },
		"无效版本":    func(m *Manifest) { m.Version = "invalid" },
		"简写版本":    func(m *Manifest) { m.Version = "1.0" },
		"无效 ID":   func(m *Manifest) { m.ID = "../invalid" },
		"无效登录":    func(m *Manifest) { m.Login = "iframe" },
		"越界预览":    func(m *Manifest) { m.Preview = "../theme.json" },
		"预览文件不存在": func(m *Manifest) { m.Preview = "preview.png" },
		"预览大小写":   func(m *Manifest) { m.Preview = "ASSETS/app.js" },
	} {
		candidate := base
		change(&candidate)
		cases[name] = makeArchive(t, candidate)
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Install(t.Context(), bytes.NewReader(archive), "upload"); errorCode(err) != "invalid_request" {
				t.Fatalf("应拒绝主题包：%v", err)
			}
			list, err := m.List(t.Context())
			if err != nil || len(list.Items) != 1 || list.ActiveInstallationID != good.InstallationID {
				t.Fatal("无效安装影响了已有选择", list, err)
			}
		})
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stage-") {
			t.Fatal("失败安装遗留临时目录", entry.Name())
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestInterruptedInstallAndDeletionRecovery(t *testing.T) {
	m := openManager(t)
	if _, err := m.Install(t.Context(), io.MultiReader(bytes.NewReader([]byte("partial")), failingReader{}), "upload"); err == nil {
		t.Fatal("中断上传不应成功")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Install(ctx, bytes.NewReader(makeArchive(t, manifest())), "upload"); err == nil {
		t.Fatal("取消安装不应成功")
	}
	good, err := m.Install(t.Context(), bytes.NewReader(makeArchive(t, manifest())), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(t.Context(), good.InstallationID); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(m.root, ".stage-interrupted")
	orphan := m.directory(strings.Repeat("a", 32))
	trash := filepath.Join(m.root, ".trash-"+good.InstallationID)
	committedTrash := filepath.Join(m.root, ".trash-"+strings.Repeat("b", 32))
	for _, directory := range []string{staged, orphan, committedTrash} {
		if err = os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Rename(m.directory(good.InstallationID), trash); err != nil {
		t.Fatal(err)
	}
	m, err = Open(t.Context(), m.db, m.root, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{staged, orphan, trash, committedTrash} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("中断目录未清理", removed, err)
		}
	}
	list, err := m.List(t.Context())
	if err != nil || list.EffectiveInstallationID != good.InstallationID {
		t.Fatal("恢复改变了已登记的选择", list, err)
	}
}

func TestConcurrentDuplicateInstall(t *testing.T) {
	m := openManager(t)
	archive := makeArchive(t, manifest())
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := m.Install(t.Context(), bytes.NewReader(archive), "upload"); errors <- err })
	}
	wg.Wait()
	close(errors)
	success, conflict := 0, 0
	for err := range errors {
		if err == nil {
			success++
		} else if errorCode(err) == "conflict" {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	entries, err := os.ReadDir(filepath.Join(m.root, "installed"))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}

func TestRejectTooManyFilesAndInvalidatedHTML(t *testing.T) {
	m := openManager(t)
	var entries []zipEntry
	for i := range maxFiles + 1 {
		entries = append(entries, zipEntry{name: fmt.Sprintf("dist/%d.js", i)})
	}
	if _, err := m.Install(t.Context(), bytes.NewReader(archiveEntries(t, entries)), "upload"); errorCode(err) != "invalid_request" {
		t.Fatal(err)
	}
	item, err := m.Install(t.Context(), bytes.NewReader(makeArchive(t, manifest())), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(t.Context(), item.InstallationID); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(m.directory(item.InstallationID), "dist", "index.html")
	index, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	// 大小保持原值，确认实际入口解析失败也会反映在可用状态中。
	index = bytes.ReplaceAll(index, []byte("HEAD"), []byte("BODY"))
	if err = os.WriteFile(filename, index, 0600); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(t.Context())
	if err != nil || list.Items[0].Available || list.EffectiveInstallationID != "" {
		t.Fatal(list, err)
	}
	_, data, err := m.ActiveIndex(t.Context())
	if err != nil || data != nil {
		t.Fatal("无效入口未使用内置", string(data), err)
	}
}
