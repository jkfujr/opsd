package executor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
)

type storageHost struct {
	firewall.Host
	ids, name, directory string
	fail                 bool
	writes               int
}

func (h *storageHost) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if h.fail {
		return nil, errors.New("Docker 读取失败")
	}
	switch args[0] {
	case "container":
		return []byte(h.ids), nil
	case "inspect":
		return json.Marshal([]any{map[string]any{"Id": strings.Repeat("a", 64), "Name": "/" + h.name, "Config": map[string]any{"Labels": map[string]string{"opsd.storage": h.directory}}, "State": map[string]bool{"Running": true}}})
	case "stop":
		if args[1] != strings.Repeat("a", 64) {
			return nil, errors.New("未使用完整 ID")
		}
		h.writes++
		return nil, nil
	}
	h.writes++
	return nil, errors.New("意外写入")
}

func TestStorageKeepsIdentityAcrossLegacyNameAndRestart(t *testing.T) {
	directory := t.TempDir()
	state := storageIdentity{Credentials: cluster.Credentials{Username: "admin", Password: "private"}, Cookie: "cookie", Config: cluster.StorageConfig{Address: "192.0.2.10", Port: 5984}}
	path := filepath.Join(directory, "couchdb.json")
	if err := localstate.Write(path, state, 0600); err != nil {
		t.Fatal(err)
	}
	host := &storageHost{ids: strings.Repeat("a", 64), name: "opsd-couchdb", directory: directory}
	s := &Server{Directory: directory, Host: host}
	if credentials, err := s.storage(t.Context(), state.Config); err != nil || credentials != state.Credentials {
		t.Fatal(credentials, err)
	}
	var saved storageIdentity
	if err := localstate.Read(path, &saved); err != nil || saved.StorageID == "" || saved.FullID != host.ids || saved.Cookie != state.Cookie || !reflect.DeepEqual(saved.Config, state.Config) {
		t.Fatal("旧实例身份改变", err)
	}
	host.name = protocol.ManagedContainerName("data", "couchdb", saved.StorageID)
	s = &Server{Directory: directory, Host: host}
	if credentials, err := s.storage(t.Context(), state.Config); err != nil || credentials != state.Credentials {
		t.Fatal("改名或重启后无法读取原容器", err)
	}
	for _, ids := range []string{"", host.ids + "\n" + strings.Repeat("b", 64)} {
		host.ids = ids
		before := host.writes
		if _, err := s.storage(t.Context(), state.Config); err == nil || host.writes != before {
			t.Fatal("容器缺失或重复时仍执行写入", err)
		}
	}
	host.fail = true
	before := host.writes
	if _, err := s.storage(t.Context(), state.Config); err == nil || host.writes != before {
		t.Fatal("未知读取后仍重建容器", err)
	}
}
