package consoletheme

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jkfujr/opsd/internal/requeststate"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func repository(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("请填写公开 GitHub 仓库的 HTTPS 地址")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", invalid("仓库地址须为 https://github.com/作者/仓库")
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, part := range parts {
		if !repositoryPart.MatchString(part) || part == "." || part == ".." || len(part) > 100 {
			return "", invalid("GitHub 仓库名称无效")
		}
	}
	return strings.Join(parts, "/"), nil
}

func githubURL(u *url.URL) bool {
	host := u.Hostname()
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (host == "api.github.com" || host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com"))
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "198.18.0.0/15"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

func downloadClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range addresses {
			if !publicAddress(ip) {
				return nil, errors.New("主题下载地址指向非公网地址")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var last error
		for _, ip := range addresses {
			if network == "tcp4" && !ip.Unmap().Is4() || network == "tcp6" && !ip.Is6() {
				continue
			}
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		if last == nil {
			last = errors.New("主题下载地址没有可用的公网解析结果")
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !githubURL(req.URL) {
			return errors.New("主题下载重定向地址不受支持")
		}
		return nil
	}}
}

func unavailable(message string) error {
	return &Error{Code: "dependency_unavailable", Message: message}
}

func (m *Manager) download(ctx context.Context, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil || !githubURL(u) {
		return nil, invalid("主题下载地址不受支持")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "api.github.com" {
		r.Header.Set("Accept", "application/vnd.github+json")
	} else {
		r.Header.Set("Accept", "application/octet-stream")
	}
	r.Header.Set("User-Agent", "OPSD-theme-installer")
	response, err := m.client.Do(r)
	if err != nil {
		return nil, unavailable("无法连接 GitHub，请检查网络后重新读取主题列表")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return nil, invalid("未找到公开 Release 或主题附件")
		}
		return nil, unavailable("GitHub 下载失败，请稍后重新读取主题列表")
	}
	return response, nil
}

func (m *Manager) InstallGitHub(ctx context.Context, address, tag string) (Installation, error) {
	if write := requeststate.Current(ctx); write != nil {
		if item, ok, err := m.replayInstallation(ctx, m.db, *write); ok || err != nil {
			return item, err
		}
	}
	var empty Installation
	repo, err := repository(address)
	if err != nil {
		return empty, err
	}
	tag = strings.TrimSpace(tag)
	if len(tag) > 256 || strings.ContainsAny(tag, "\x00\r\n") {
		return empty, invalid("Release 标签无效")
	}
	endpoint := "https://api.github.com/repos/" + repo + "/releases/latest"
	if tag != "" {
		endpoint = "https://api.github.com/repos/" + repo + "/releases/tags/" + url.PathEscape(tag)
	}
	response, err := m.download(ctx, endpoint)
	if err != nil {
		return empty, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	response.Body.Close()
	if err != nil || len(data) > 1<<20 {
		return empty, unavailable("GitHub Release 信息无法读取")
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &release) != nil || release.TagName == "" || release.Draft || tag == "" && release.Prerelease || tag != "" && release.TagName != tag {
		return empty, invalid("未找到可安装的公开 Release")
	}
	download := ""
	for _, asset := range release.Assets {
		if asset.Name != "opsd-theme.zip" {
			continue
		}
		if download != "" {
			return empty, invalid("Release 包含多个 opsd-theme.zip 附件")
		}
		if asset.Size > MaxArchiveSize {
			return empty, invalid("主题 ZIP 不能超过 100 MiB")
		}
		download = asset.URL
	}
	if download == "" {
		return empty, invalid("Release 缺少 opsd-theme.zip，可改为上传主题 ZIP")
	}
	response, err = m.download(ctx, download)
	if err != nil {
		return empty, err
	}
	defer response.Body.Close()
	return m.Install(ctx, response.Body, "https://github.com/"+repo+"/releases/tag/"+url.PathEscape(release.TagName))
}
