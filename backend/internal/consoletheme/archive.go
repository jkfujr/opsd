// Package consoletheme 管理本机控制台主题包，不参与节点通信或系统操作。
package consoletheme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/semver"
	"golang.org/x/net/html"
)

const (
	MaxArchiveSize = 100 << 20
	maxTotalSize   = 256 << 20
	maxFileSize    = 64 << 20
	maxIndexSize   = 1 << 20
	maxManifest    = 64 << 10
	maxFiles       = 4096
	BuiltinPath    = "/__opsd/builtin/"
	AssetsPath     = "/__opsd/themes/"
)

type Manifest struct {
	FormatVersion  int    `json:"format_version"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	APIVersion     int    `json:"api_version"`
	MinOPSDVersion string `json:"min_opsd_version"`
	Login          string `json:"login"`
	Author         string `json:"author,omitempty"`
	URL            string `json:"url,omitempty"`
	Preview        string `json:"preview,omitempty"`
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string     { return e.Message }
func invalid(message string) error { return &Error{Code: "invalid_request", Message: message} }

var themeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var installationID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var windowsDevice = regexp.MustCompile(`^(COM|LPT)[0-9]$`)

func version(value string) string { return "v" + strings.TrimPrefix(value, "v") }

func fullVersion(value string) bool {
	base := strings.SplitN(strings.SplitN(value, "-", 2)[0], "+", 2)[0]
	return strings.Count(base, ".") == 2 && semver.IsValid(version(value))
}

func (m Manifest) compatible(current string) error {
	if m.APIVersion != SupportedAPIVersion {
		return invalid("主题需要的 API 版本不受支持")
	}
	if !semver.IsValid(version(current)) || semver.Compare(version(current), version(m.MinOPSDVersion)) < 0 {
		return invalid("主题需要 OPSD " + m.MinOPSDVersion + " 或更新版本")
	}
	return nil
}

func readManifest(data []byte, current string) (Manifest, error) {
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, invalid("theme.json 格式无效")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return m, invalid("theme.json 必须只包含一个 JSON 对象")
	}
	if m.FormatVersion != 1 || !themeID.MatchString(m.ID) || m.ID == "builtin" {
		return m, invalid("主题格式版本或 ID 无效")
	}
	if strings.TrimSpace(m.Name) == "" || utf8.RuneCountInString(m.Name) > 128 || utf8.RuneCountInString(m.Author) > 128 {
		return m, invalid("主题名称不能为空，名称和作者最多 128 字符")
	}
	if !fullVersion(m.Version) || !fullVersion(m.MinOPSDVersion) {
		return m, invalid("主题版本和最低 OPSD 版本须使用语义化版本")
	}
	m.Version = strings.TrimPrefix(m.Version, "v")
	m.MinOPSDVersion = strings.TrimPrefix(m.MinOPSDVersion, "v")
	if m.Login == "" {
		m.Login = "builtin"
	}
	if m.Login != "builtin" && m.Login != "theme" {
		return m, invalid("登录模式须为 builtin 或 theme")
	}
	if m.URL != "" {
		u, e := url.Parse(m.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(m.URL) > 2048 {
			return m, invalid("主题项目地址须为 HTTPS 地址")
		}
	}
	if m.Preview != "" && (!validPath(m.Preview) || strings.HasSuffix(m.Preview, "/")) {
		return m, invalid("预览图须为 dist 内的相对文件路径")
	}
	return m, m.compatible(current)
}

type archiveFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func validPath(name string) bool {
	if !utf8.ValidString(name) || !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:\x00<>|?*\"") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || windowsDevice.MatchString(base) {
			return false
		}
	}
	return true
}

func extract(ctx context.Context, filename, target, current string) (Manifest, []archiveFile, error) {
	var m Manifest
	z, err := zip.OpenReader(filename)
	if err != nil {
		if z != nil {
			z.Close()
		}
		return m, nil, invalid("无法打开 ZIP 主题包")
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > maxFiles {
		return m, nil, invalid("主题包最多包含 4096 个文件和目录")
	}
	seen := map[string]bool{}
	regular := map[string]bool{}
	kinds := map[string]bool{}
	var total uint64
	var files []archiveFile
	var manifestFile, indexFile *zip.File
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		key := strings.ToLower(name)
		if !validPath(name) || seen[key] || (name != "theme.json" && name != "dist" && !strings.HasPrefix(name, "dist/")) {
			return m, nil, invalid("主题包含重复、越界或不受支持的文件路径")
		}
		seen[key] = true
		if f.Mode()&fs.ModeType != 0 && f.Mode()&fs.ModeType != fs.ModeDir {
			return m, nil, invalid("主题包只允许普通文件和目录")
		}
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if isFile, exists := kinds[parent]; exists && isFile {
				return m, nil, invalid("主题包中的文件与目录冲突")
			}
			kinds[parent] = false
		}
		isFile := !f.FileInfo().IsDir()
		if prior, exists := kinds[key]; exists && prior != isFile {
			return m, nil, invalid("主题包中的文件与目录冲突")
		}
		kinds[key] = isFile
		if f.FileInfo().IsDir() {
			continue
		}
		regular[name] = true
		if name == "dist" {
			return m, nil, invalid("dist 必须是目录")
		}
		if f.UncompressedSize64 > maxFileSize || total+f.UncompressedSize64 > maxTotalSize {
			return m, nil, invalid("主题包解压后超过大小限制")
		}
		total += f.UncompressedSize64
		files = append(files, archiveFile{Path: name, Size: int64(f.UncompressedSize64)})
		if name == "theme.json" {
			manifestFile = f
		}
		if name == "dist/index.html" {
			indexFile = f
		}
	}
	if manifestFile == nil || indexFile == nil {
		return m, nil, invalid("主题包根目录须包含 theme.json 和 dist/index.html")
	}
	manifest, err := zipRead(manifestFile, maxManifest)
	if err != nil {
		return m, nil, err
	}
	m, err = readManifest(manifest, current)
	if err != nil {
		return m, nil, err
	}
	index, err := zipRead(indexFile, maxIndexSize)
	if err != nil {
		return m, nil, err
	}
	if _, err := injectBase(index, "/__opsd/themes/check/"); err != nil {
		return m, nil, err
	}
	if m.Preview != "" && !regular["dist/"+m.Preview] {
		return m, nil, invalid("主题预览图不存在")
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		return m, nil, err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return m, nil, err
	}
	defer root.Close()
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return m, nil, err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(filepath.FromSlash(name), 0700); err != nil {
				return m, nil, err
			}
			continue
		}
		if err := root.MkdirAll(filepath.FromSlash(path.Dir(name)), 0700); err != nil {
			return m, nil, err
		}
		if err := extractFile(root, f); err != nil {
			return m, nil, err
		}
	}
	return m, files, nil
}

func zipRead(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, invalid(f.Name + " 超过大小限制")
	}
	r, err := f.Open()
	if err != nil {
		return nil, invalid("主题包内容无法读取")
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, invalid("主题包内容损坏或超过大小限制")
	}
	return data, nil
}

func extractFile(root *os.Root, entry *zip.File) error {
	r, err := entry.Open()
	if err != nil {
		return invalid("主题包内容无法读取")
	}
	defer r.Close()
	f, err := root.OpenFile(filepath.FromSlash(entry.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(r, maxFileSize+1))
	if err != nil || n != int64(entry.UncompressedSize64) {
		return invalid("主题包内容损坏或超过大小限制")
	}
	return f.Sync()
}

// 仅插入资源基址，保留主题原有的脚本和 HTML；已有 base 会改变该约定。
func injectBase(data []byte, base string) ([]byte, error) {
	z := html.NewTokenizer(bytes.NewReader(data))
	position, head := 0, -1
	for {
		kind := z.Next()
		position += len(z.Raw())
		if kind == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, invalid("主题入口 HTML 无法读取")
			}
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		tag, _ := z.TagName()
		if bytes.EqualFold(tag, []byte("base")) {
			return nil, invalid("主题入口不能自行设置 base 元素")
		}
		if head < 0 && kind == html.StartTagToken && bytes.EqualFold(tag, []byte("head")) {
			head = position
		}
	}
	if head < 0 {
		return nil, invalid("主题入口须包含 head 元素")
	}
	injection := []byte(fmt.Sprintf(`<base href="%s">`, stdhtml.EscapeString(base)))
	out := make([]byte, 0, len(data)+len(injection))
	out = append(out, data[:head]...)
	out = append(out, injection...)
	return append(out, data[head:]...), nil
}
