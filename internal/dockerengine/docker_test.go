//go:build linux

package dockerengine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type dockerServer struct {
	id     string
	labels map[string]string
	tty    bool
	text   string
	writes int
}

func fakeDocker(t *testing.T) (*Manager, *dockerServer) {
	t.Helper()
	fixture := &dockerServer{id: strings.Repeat("a", 64), labels: map[string]string{"com.docker.compose.project": "示例项目", "com.docker.compose.service": "web"}, text: "stdout 输出\nstderr 输出\n"}
	path := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/_ping" {
			w.Header().Set("API-Version", "1.54")
			w.WriteHeader(200)
			return
		}
		if strings.HasPrefix(p, "/v") {
			if i := strings.Index(p[1:], "/"); i >= 0 {
				p = p[i+1:]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case p == "/info":
			json.NewEncoder(w).Encode(map[string]any{"ID": "engine-a", "OSType": "linux", "ServerVersion": "29.4.2"})
		case p == "/containers/json":
			json.NewEncoder(w).Encode([]any{map[string]any{"Id": fixture.id, "Names": []string{"/同名容器"}, "Image": "busybox:1.37.0", "State": "running", "Status": "Up (healthy)", "Labels": fixture.labels, "Ports": []any{map[string]any{"IP": "::", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"}}}})
		case p == "/containers/"+fixture.id+"/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": fixture.id, "Name": "/同名容器", "Created": "2026-11-06T01:00:00Z", "RestartCount": 2, "State": map[string]any{"Status": "running", "StartedAt": "2026-11-06T01:00:01Z"}, "Config": map[string]any{"Image": "busybox:1.37.0", "Labels": fixture.labels, "Tty": fixture.tty, "Env": []string{"TOKEN=不得返回的凭据"}}, "HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "unless-stopped"}}, "Mounts": []any{}, "NetworkSettings": map[string]any{"Networks": map[string]any{}, "Ports": map[string]any{}}})
		case p == "/containers/"+fixture.id+"/logs":
			if r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("tail") == "" {
				t.Error("日志请求缺少读取边界")
			}
			if fixture.tty {
				fmt.Fprint(w, fixture.text)
			} else {
				header := make([]byte, 8)
				header[0] = 1
				binary.BigEndian.PutUint32(header[4:], uint32(len(fixture.text)))
				w.Write(header)
				fmt.Fprint(w, fixture.text)
			}
		case r.Method == "POST" && strings.HasPrefix(p, "/containers/"+fixture.id+"/"):
			fixture.writes++
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"No such container"}`)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return &Manager{Socket: path}, fixture
}
func TestLocalDockerIdentityAndReadOnlyComponents(t *testing.T) {
	m, fixture := fakeDocker(t)
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")
	list := m.List(t.Context())
	if list.State != "ready" || len(list.Containers) != 1 || list.Containers[0].Project != "示例项目" || list.Containers[0].Health != "healthy" {
		t.Fatal("列表未读取本机 Docker", list)
	}
	ref := list.Containers[0].ContainerRef
	detail, err := m.Inspect(t.Context(), ref)
	if err != nil || detail.Name != "同名容器" || detail.RestartPolicy != "unless-stopped" {
		t.Fatal(detail, err)
	}
	raw, _ := json.Marshal(detail)
	if strings.Contains(string(raw), "不得返回的凭据") {
		t.Fatal("详情返回了环境凭据")
	}
	wrong := ref
	wrong.EngineID = "engine-b"
	if err = m.Change(t.Context(), protocol.ContainerAction{ContainerRef: wrong, Action: "restart"}); err == nil {
		t.Fatal("接受了其他 Engine 身份")
	}
	wrong = ref
	wrong.ContainerID = strings.Repeat("b", 64)
	if _, err = m.Inspect(t.Context(), wrong); err == nil {
		t.Fatal("按同名容器替换了已删除 ID")
	}
	for _, labels := range []map[string]string{{"opsd.storage": "/var/lib/opsd/executor"}, {"com.docker.swarm.task.id": "任务一"}} {
		fixture.labels = labels
		if err = m.Change(t.Context(), protocol.ContainerAction{ContainerRef: ref, Action: "stop"}); err == nil {
			t.Fatal("受控组件允许从容器入口修改")
		}
	}
	if fixture.writes != 0 {
		t.Fatal("身份或只读检查后仍调用了 Docker 写接口")
	}
}
func TestDockerLogTTYMultiplexAndLimit(t *testing.T) {
	m, fixture := fakeDocker(t)
	ref := protocol.ContainerRef{EngineID: "engine-a", ContainerID: fixture.id}
	for _, tty := range []bool{false, true} {
		fixture.tty = tty
		value, err := m.Logs(t.Context(), protocol.ContainerLogsRead{ContainerRef: ref, Tail: 200})
		if err != nil || value.Text != fixture.text || value.Truncated {
			t.Fatal("日志解码错误", tty, value, err)
		}
	}
	fixture.tty = false
	fixture.text = strings.Repeat("长日志内容", MaxLogBytes/5)
	value, err := m.Logs(t.Context(), protocol.ContainerLogsRead{ContainerRef: ref, Tail: 1000})
	if err != nil || !value.Truncated || len(value.Text) > MaxLogBytes+3 {
		t.Fatal("日志读取未受限", len(value.Text), value.Truncated, err)
	}
	if _, err = m.Logs(t.Context(), protocol.ContainerLogsRead{ContainerRef: ref, Tail: 1001}); err == nil {
		t.Fatal("接受了超出上限的日志请求")
	}
}
func TestDockerNativeLifecycleAndLogs(t *testing.T) {
	if os.Getenv("OPSD_DOCKER_TEST") != "1" {
		t.Skip("未启用真实 Docker 验收")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("Docker 验收失败：%v：%s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	m := New()
	state := m.List(ctx)
	if state.State != "ready" {
		t.Fatal(state)
	}
	for _, tty := range []bool{false, true} {
		args := []string{"run", "-d", "--name", fmt.Sprintf("opsd-native-%d-%t", os.Getpid(), tty)}
		if tty {
			args = append(args, "-t")
		}
		args = append(args, "busybox:1.37.0", "sh", "-c", "trap 'exit 0' TERM INT; while :; do echo '容器标准输出'; echo '容器错误输出' >&2; sleep 1; done")
		id := run(args...)
		t.Cleanup(func() { exec.Command("docker", "rm", "-f", id).Run() })
		ref := protocol.ContainerRef{EngineID: state.EngineID, ContainerID: id}
		initial, err := m.Inspect(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if err = m.Change(ctx, protocol.ContainerAction{ContainerRef: ref, Action: "stop"}); err != nil {
			t.Fatal(err)
		}
		stopped, err := m.Inspect(ctx, ref)
		if err != nil || stopped.State != "exited" {
			t.Fatal(stopped, err)
		}
		if err = m.Change(ctx, protocol.ContainerAction{ContainerRef: ref, Action: "start"}); err != nil {
			t.Fatal(err)
		}
		if err = m.Change(ctx, protocol.ContainerAction{ContainerRef: ref, Action: "restart"}); err != nil {
			t.Fatal(err)
		}
		after, err := m.Inspect(ctx, ref)
		if err != nil || after.State != "running" || after.StartedAt == nil || initial.StartedAt == nil || !after.StartedAt.After(*initial.StartedAt) {
			t.Fatal(after, err)
		}
		logs, err := m.Logs(ctx, protocol.ContainerLogsRead{ContainerRef: ref, Tail: 200})
		if err != nil || !strings.Contains(logs.Text, "容器标准输出") || !strings.Contains(logs.Text, "容器错误输出") {
			t.Fatal(logs, err)
		}
	}
}
