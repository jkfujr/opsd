package executor

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
)

const CouchImage = "couchdb:3.5.0"

var storageLock sync.Mutex

type storageIdentity struct {
	StorageID   string                 `json:"storage_id,omitempty"`
	FullID      string                 `json:"full_id,omitempty"`
	Old         string                 `json:"old,omitempty"`
	New         string                 `json:"new,omitempty"`
	Cookie      string                 `json:"cookie"`
	Credentials protocol.Credentials   `json:"credentials"`
	Config      protocol.StorageConfig `json:"config"`
}

func (s *Server) storage(ctx context.Context, c protocol.StorageConfig) (protocol.Credentials, error) {
	storageLock.Lock()
	defer storageLock.Unlock()
	if s.Host == nil {
		s.Host = firewall.NativeHost{}
	}
	var endpoint *net.TCPAddr
	if c.Enabled {
		var err error
		endpoint, err = storageEndpoint(c)
		if err != nil {
			return protocol.Credentials{}, err
		}
	}
	path := filepath.Join(s.Directory, "couchdb.json")
	var state storageIdentity
	e := localstate.Read(path, &state)
	if e != nil && !os.IsNotExist(e) {
		return state.Credentials, e
	}
	if os.IsNotExist(e) {
		state.Credentials = protocol.Credentials{Username: "opsd_admin", Password: identity.Random()}
		state.Cookie = identity.Random()
		if e = localstate.Write(path, state, 0600); e != nil {
			return state.Credentials, e
		}
	}
	if state.StorageID == "" {
		state.StorageID = identity.Random()
		if e = localstate.Write(path, state, 0600); e != nil {
			return state.Credentials, e
		}
	}
	name := protocol.ManagedContainerName("data", "couchdb", state.StorageID)
	// 先读取容器身份；未知的 inspect 结果不能当作不存在并重新创建。
	list, e := s.Host.Run(ctx, "docker", "container", "ls", "-aq", "--no-trunc", "--filter", "label=opsd.storage="+s.Directory)
	if e != nil {
		return state.Credentials, e
	}
	ids := strings.Fields(string(list))
	if len(ids) > 1 {
		return state.Credentials, fmt.Errorf("当前节点存在多个数据库容器，请核对原实例")
	}
	exists := len(ids) == 1
	if !exists && state.FullID != "" {
		return state.Credentials, fmt.Errorf("原数据库容器已不存在，请核对保存的完整 ID")
	}
	if exists {
		raw, err := s.Host.Run(ctx, "docker", "inspect", ids[0])
		if err != nil {
			return state.Credentials, err
		}
		var containers []struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Config struct {
				Image  string            `json:"Image"`
				Labels map[string]string `json:"Labels"`
			}
			State struct {
				Running bool `json:"Running"`
			}
		}
		if json.Unmarshal(raw, &containers) != nil || len(containers) != 1 || containers[0].Config.Labels["opsd.storage"] != s.Directory {
			return state.Credentials, fmt.Errorf("同名容器不属于当前节点")
		}
		current := containers[0]
		if len(current.ID) != 64 || state.FullID != "" && state.FullID != current.ID || current.Name != "/"+name && current.Name != "/opsd-couchdb" {
			return state.Credentials, fmt.Errorf("数据库容器身份或名称已经变化")
		}
		if state.FullID != current.ID {
			state.FullID = current.ID
			if e = localstate.Write(path, state, 0600); e != nil {
				return state.Credentials, e
			}
		}
		if !c.Enabled {
			_, e = s.Host.Run(ctx, "docker", "stop", current.ID)
			return state.Credentials, e
		}
		if state.Config != c {
			if containers[0].State.Running {
				return state.Credentials, fmt.Errorf("数据库地址已部署，修改前须先停用")
			}
			if _, e = s.Host.Run(ctx, "docker", "rm", current.ID); e != nil {
				return state.Credentials, e
			}
			state.FullID, state.Old, state.New = "", "", ""
			if e = localstate.Write(path, state, 0600); e != nil {
				return state.Credentials, e
			}
		} else {
			if !containers[0].State.Running {
				if _, e = s.Host.Run(ctx, "docker", "start", current.ID); e != nil {
					return state.Credentials, e
				}
			}
			return state.Credentials, s.waitStorage(ctx, endpoint)
		}
	}
	if !c.Enabled {
		return state.Credentials, nil
	}
	dir := filepath.Join(s.Directory, "couchdb")
	configDir := filepath.Join(dir, "config")
	if e = os.MkdirAll(configDir, 0755); e != nil {
		return state.Credentials, e
	}
	if e = os.Chown(configDir, 5984, 5984); e != nil {
		return state.Credentials, e
	}
	if e = os.MkdirAll(filepath.Join(dir, "data"), 0750); e != nil {
		return state.Credentials, e
	}
	if e = os.Chown(filepath.Join(dir, "data"), 5984, 5984); e != nil {
		return state.Credentials, e
	}
	mode, count := "true", 1
	if c.NativeCluster {
		mode, count = "false", 3
	}
	configuration := "[couchdb]\nsingle_node = " + mode + "\n[cluster]\nn = " + fmt.Sprint(count) + "\nq = 1\n[chttpd]\nbind_address = " + c.Address + "\nport = " + fmt.Sprint(c.Port) + "\n"
	if e = os.WriteFile(filepath.Join(configDir, "00-opsd.ini"), []byte(configuration), 0644); e != nil {
		return state.Credentials, e
	}
	if e = os.Chown(filepath.Join(configDir, "00-opsd.ini"), 5984, 5984); e != nil {
		return state.Credentials, e
	}
	state.Config = c
	if e = localstate.Write(path, state, 0600); e != nil {
		return state.Credentials, e
	}
	// 密码只经临时环境文件传递，不放进命令参数或日志。
	env, e := os.CreateTemp(s.Directory, ".couchdb-env-")
	if e != nil {
		return state.Credentials, e
	}
	envPath := env.Name()
	defer os.Remove(envPath)
	if e = env.Chmod(0600); e == nil {
		ip := endpoint.IP
		parts := []string{}
		if v := ip.To4(); v != nil {
			for _, b := range v {
				parts = append(parts, fmt.Sprint(b))
			}
		} else {
			v := ip.To16()
			for i := 0; i < 16; i += 2 {
				parts = append(parts, fmt.Sprint(binary.BigEndian.Uint16(v[i:i+2])))
			}
		}
		flags := ""
		if ip.To4() == nil {
			flags = "-proto_dist inet6_tcp "
		}
		_, e = fmt.Fprintf(env, "COUCHDB_USER=%s\nCOUCHDB_PASSWORD=%s\nCOUCHDB_ERLANG_COOKIE=%s\nNODENAME=%s\nERL_EPMD_ADDRESS=%s\nERL_EPMD_PORT=%d\nERL_FLAGS=%s-kernel inet_dist_use_interface {%s} -kernel inet_dist_listen_min %d -kernel inet_dist_listen_max %d\n", state.Credentials.Username, state.Credentials.Password, state.Cookie, c.Address, c.Address, 20000+c.Port%40000, flags, strings.Join(parts, ","), 20001+c.Port%40000, 20001+c.Port%40000)
	}
	closeErr := env.Close()
	if e != nil {
		return state.Credentials, e
	}
	if closeErr != nil {
		return state.Credentials, closeErr
	}
	created, e := s.Host.Run(ctx, "docker", "run", "-d", "--name", name, "--label", "opsd.storage="+s.Directory, "--restart", "unless-stopped", "--network", "host", "--env-file", envPath, "--mount", "type=bind,src="+filepath.Join(dir, "data")+",dst=/opt/couchdb/data", "--mount", "type=bind,src="+configDir+",dst=/opt/couchdb/etc/local.d", CouchImage)
	if e == nil {
		state.FullID = strings.TrimSpace(string(created))
		if len(state.FullID) != 64 {
			return state.Credentials, fmt.Errorf("数据库容器创建结果缺少完整 ID，请核对原容器")
		}
		e = localstate.Write(path, state, 0600)
		if e == nil {
			e = s.waitStorage(ctx, endpoint)
		}
	}
	return state.Credentials, e
}
func storageEndpoint(c protocol.StorageConfig) (*net.TCPAddr, error) {
	ip := net.ParseIP(c.Address)
	if ip == nil || ip.IsUnspecified() || c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("数据库地址无效")
	}
	endpoint := &net.TCPAddr{IP: ip, Port: c.Port}
	if ip.IsLoopback() {
		return endpoint, nil
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("读取本机数据库绑定地址：%w", err)
	}
	for _, address := range addresses {
		local, _, _ := net.ParseCIDR(address.String())
		if ip.Equal(local) {
			return endpoint, nil
		}
	}
	return nil, fmt.Errorf("数据库须绑定本机地址")
}

func (s *Server) waitStorage(ctx context.Context, endpoint *net.TCPAddr) error {
	if !s.prepare {
		return waitDatabase(ctx, endpoint)
	}
	dialer := net.Dialer{Timeout: 200 * time.Millisecond, LocalAddr: &net.TCPAddr{IP: endpoint.IP}}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		connection, e := dialer.DialContext(ctx, "tcp", endpoint.String())
		if e == nil {
			connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("数据库接口未启动")
}
func waitDatabase(ctx context.Context, endpoint *net.TCPAddr) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: endpoint.IP}}
	client := &http.Client{
		Transport: &http.Transport{DialContext: dialer.DialContext},
		Timeout:   time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint.String()+"/_up", nil)
		if e != nil {
			return e
		}
		response, e := client.Do(request)
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return nil
			}
			if response.StatusCode >= 300 && response.StatusCode < 400 {
				return fmt.Errorf("数据库就绪检查拒绝重定向")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("数据库未就绪")
		case <-ticker.C:
		}
	}
}
