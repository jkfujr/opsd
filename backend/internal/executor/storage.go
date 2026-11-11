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

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/localstate"
)

const CouchImage = "couchdb:3.5.0"

var storageLock sync.Mutex

type storageIdentity struct {
	Cookie      string                `json:"cookie"`
	Credentials cluster.Credentials   `json:"credentials"`
	Config      cluster.StorageConfig `json:"config"`
}

func (s *Server) storage(ctx context.Context, c cluster.StorageConfig) (cluster.Credentials, error) {
	storageLock.Lock()
	defer storageLock.Unlock()
	if s.Host == nil {
		s.Host = firewall.NativeHost{}
	}
	if c.Enabled && (net.ParseIP(c.Address) == nil || net.ParseIP(c.Address).IsUnspecified() || c.Port < 1 || c.Port > 65535) {
		return cluster.Credentials{}, fmt.Errorf("数据库地址无效")
	}
	path := filepath.Join(s.Directory, "couchdb.json")
	var state storageIdentity
	e := localstate.Read(path, &state)
	if e != nil && !os.IsNotExist(e) {
		return state.Credentials, e
	}
	if os.IsNotExist(e) {
		state.Credentials = cluster.Credentials{Username: "opsd_admin", Password: identity.Random()}
		state.Cookie = identity.Random()
		if e = localstate.Write(path, state, 0600); e != nil {
			return state.Credentials, e
		}
	}
	// 先读取容器身份；未知的 inspect 结果不能当作不存在并重新创建。
	list, e := s.Host.Run(ctx, "docker", "container", "ls", "-aq", "--filter", "name=^opsd-couchdb$")
	if e != nil {
		return state.Credentials, e
	}
	exists := strings.TrimSpace(string(list)) != ""
	if exists {
		raw, err := s.Host.Run(ctx, "docker", "inspect", "opsd-couchdb")
		if err != nil {
			return state.Credentials, err
		}
		var containers []struct {
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
		if !c.Enabled {
			_, e = s.Host.Run(ctx, "docker", "stop", "opsd-couchdb")
			return state.Credentials, e
		}
		if state.Config != c {
			if containers[0].State.Running {
				return state.Credentials, fmt.Errorf("数据库地址已部署，修改前须先停用")
			}
			if _, e = s.Host.Run(ctx, "docker", "rm", "opsd-couchdb"); e != nil {
				return state.Credentials, e
			}
		} else {
			if !containers[0].State.Running {
				if _, e = s.Host.Run(ctx, "docker", "start", "opsd-couchdb"); e != nil {
					return state.Credentials, e
				}
			}
			return state.Credentials, s.waitStorage(ctx, c)
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
		ip := net.ParseIP(c.Address)
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
	_, e = s.Host.Run(ctx, "docker", "run", "-d", "--name", "opsd-couchdb", "--label", "opsd.storage="+s.Directory, "--restart", "unless-stopped", "--network", "host", "--env-file", envPath, "--mount", "type=bind,src="+filepath.Join(dir, "data")+",dst=/opt/couchdb/data", "--mount", "type=bind,src="+configDir+",dst=/opt/couchdb/etc/local.d", CouchImage)
	if e == nil {
		e = s.waitStorage(ctx, c)
	}
	return state.Credentials, e
}
func (s *Server) waitStorage(ctx context.Context, c cluster.StorageConfig) error {
	if !s.prepare {
		return waitDatabase(ctx, c)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		connection, e := net.DialTimeout("tcp", net.JoinHostPort(c.Address, fmt.Sprint(c.Port)), 200*time.Millisecond)
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
func waitDatabase(ctx context.Context, c cluster.StorageConfig) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, e := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(c.Address, fmt.Sprint(c.Port))+"/_up", nil)
		if e != nil {
			return e
		}
		response, e := client.Do(request)
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("数据库未就绪")
		case <-ticker.C:
		}
	}
}
