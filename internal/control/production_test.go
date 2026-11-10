//go:build linux

package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/transport"
)

// 此检查连接 CI 中实际安装的节点和 root 执行器，不启动浏览器。
func TestProductionBackendAPI(t *testing.T) {
	if os.Getenv("OPSD_API_TEST") != "1" {
		t.Skip("设置 OPSD_API_TEST=1 检查实际安装程序")
	}
	base := os.Getenv("OPSD_API_URL")
	if base == "" {
		base = "http://127.0.0.1:65533"
	}
	password := os.Getenv("OPSD_API_PASSWORD")
	if password == "" {
		t.Fatal("缺少 API 验收账号密码")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	csrf := ""
	activeTest := t
	call := func(method, path, key string, body, out any) int {
		t := activeTest
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(data)
		}
		r, err := http.NewRequestWithContext(t.Context(), method, base+"/api/v1"+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", base)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode >= 400 && response.StatusCode != 401 && response.StatusCode != 403 {
			t.Fatalf("%s %s：%d %s", method, path, response.StatusCode, data)
		}
		if out != nil && response.StatusCode < 400 {
			if err = json.Unmarshal(data, out); err != nil {
				t.Fatalf("%s：%v", path, err)
			}
		}
		return response.StatusCode
	}
	login := func() {
		t := activeTest
		t.Helper()
		var session httpapi.CurrentSession
		if call("POST", "/auth/login", "", map[string]any{"username": "api-check", "password": password, "entrance": "/signin"}, &session) != 200 {
			t.Fatal("登录失败")
		}
		csrf = session.CsrfToken
	}
	if call("GET", "/nodes", "", nil, nil) != 401 {
		t.Fatal("未登录请求未拒绝")
	}
	login()
	var nodes httpapi.NodeList
	call("GET", "/nodes", "", nil, &nodes)
	if len(nodes.Items) != 1 || !nodes.Items[0].Local || nodes.Items[0].Name != "api-check" {
		t.Fatalf("节点身份或重复安装结果无效：%+v", nodes.Items)
	}
	id := nodes.Items[0].Id
	waitOperation := func(operation string, check func(protocol.OperationState) bool) cluster.Operation {
		t := activeTest
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			var op cluster.Operation
			call("GET", "/operations/"+operation, "", nil, &op)
			if check(op.State) {
				return op
			}
			if op.State == protocol.AttentionRequired || op.State == protocol.Failed || op.State == protocol.Rejected {
				t.Fatalf("操作未完成：%+v", op)
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatal("操作等待超时：", operation)
		return cluster.Operation{}
	}
	succeeded := func(state protocol.OperationState) bool { return state == protocol.Succeeded }
	var report = map[string]any{"node_id": id}
	t.Cleanup(func() {
		if err := os.MkdirAll("../../artifacts/api", 0755); err != nil {
			t.Error(err)
			return
		}
		report["passed"] = !t.Failed()
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile("../../artifacts/api/result.json", append(data, '\n'), 0644); err != nil {
			t.Error(err)
		}
	})

	t.Run("任务执行幂等取消与日志", func(t *testing.T) {
		activeTest = t
		task := cluster.ScheduledTask{ID: "api-script", Name: "API 验收", Schedule: "30 2 * * *", Timezone: "Asia/Shanghai", Script: "printf '任务输出\\n'; sleep 60", Directory: "/tmp"}
		var save cluster.Operation
		call("POST", "/nodes/"+id+"/tasks", "task-save", task, &save)
		waitOperation(save.ID, succeeded)
		deadline := time.Now().Add(15 * time.Second)
		for {
			var list struct{ Items []cluster.ScheduledTask }
			call("GET", "/tasks?node_id="+id, "", nil, &list)
			if len(list.Items) == 1 {
				task = list.Items[0]
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("任务列表未更新")
			}
			time.Sleep(150 * time.Millisecond)
		}
		if task.Revision != 1 || task.Enabled || task.NextAt != nil {
			t.Fatal("停用任务状态无效", task)
		}
		var run, duplicate, rejected cluster.Operation
		call("POST", "/nodes/"+id+"/tasks/"+task.ID+"/run", "manual-run", nil, &run)
		call("POST", "/nodes/"+id+"/tasks/"+task.ID+"/run", "manual-run", nil, &duplicate)
		if run.ID != duplicate.ID {
			t.Fatal("相同请求重复受理")
		}
		call("POST", "/nodes/"+id+"/tasks/"+task.ID+"/run", "overlapping-run", nil, &rejected)
		if rejected.State != protocol.Rejected || rejected.Error == nil || rejected.Error.Code != "task_busy" {
			t.Fatal("任务允许重入", rejected)
		}
		waitOperation(run.ID, func(state protocol.OperationState) bool { return state == protocol.Applying })
		call("POST", "/operations/"+run.ID+"/cancel", "cancel-run", nil, nil)
		waitOperation(run.ID, func(state protocol.OperationState) bool { return state == protocol.Cancelled })
		var logs protocol.ExecutionLogs
		call("GET", "/nodes/"+id+"/executions/"+run.ID+"/logs?limit=4", "", nil, &logs)
		if len(logs.Data) != 4 || !logs.HasMore || logs.NextOffset != 4 {
			t.Fatal("日志未分页", logs)
		}
		call("GET", fmt.Sprintf("/nodes/%s/executions/%s/logs?offset=%d", id, run.ID, logs.NextOffset), "", nil, &logs)
		if !strings.Contains(string(logs.Data), "输出") {
			t.Fatal("任务输出缺失")
		}
		// 实际调度一次，再停用；计划执行与手动执行使用同一持久队列。
		task.Schedule, task.Script, task.Enabled = "* * * * *", "printf '计划任务完成\\n'", true
		call("POST", "/nodes/"+id+"/tasks", "enable-task", task, &save)
		waitOperation(save.ID, succeeded)
		deadline = time.Now().Add(75 * time.Second)
		var scheduled cluster.Operation
		for scheduled.ID == "" && time.Now().Before(deadline) {
			var runs struct{ Items []cluster.Operation }
			call("GET", "/nodes/"+id+"/tasks/"+task.ID+"/runs", "", nil, &runs)
			for _, item := range runs.Items {
				if strings.HasPrefix(item.Key, "scheduled:") {
					scheduled = item
					break
				}
			}
			if scheduled.ID == "" {
				time.Sleep(300 * time.Millisecond)
			}
		}
		if scheduled.ID == "" {
			t.Fatal("计划未触发")
		}
		waitOperation(scheduled.ID, succeeded)
		task.Revision, task.Enabled = 2, false
		call("POST", "/nodes/"+id+"/tasks", "disable-task", task, &save)
		waitOperation(save.ID, succeeded)
		var filtered struct{ Items []cluster.Operation }
		call("GET", "/operations?resource_id="+task.ID, "", nil, &filtered)
		if len(filtered.Items) < 3 {
			t.Fatal("资源筛选遗漏任务记录")
		}
		for _, item := range filtered.Items {
			if item.ResourceID != task.ID {
				t.Fatal("资源筛选混入其他操作")
			}
		}
		report["tasks"] = true
	})

	docker := func(args ...string) string {
		t := activeTest
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		data, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("Docker %v：%v %s", args, err, data)
		}
		return strings.TrimSpace(string(data))
	}
	t.Run("容器更新保留配置", func(t *testing.T) {
		activeTest = t
		docker("pull", "busybox:1.36.1")
		oldImage := docker("image", "inspect", "busybox:1.36.1", "--format", "{{.Id}}")
		// 模拟远端标签已更新，旧容器仍运行旧镜像的实际情况。
		docker("tag", oldImage, "busybox:1.37.0")
		name, network := "opsd-api-update", "opsd-api-network"
		docker("network", "create", network)
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-f", name).Run()
			_ = exec.Command("docker", "network", "rm", network).Run()
		})
		dir := t.TempDir()
		oldID := docker("run", "-d", "--name", name, "--network", network, "--network-alias", "service-api", "--restart", "unless-stopped", "--env", "OPSD_API_VALUE=配置保留", "--mount", "type=bind,src="+dir+",dst=/data", "--volume", "/cache", "--mount", "type=volume,dst=/readonly,readonly", "--mount", "type=volume,dst=/api-volume", "busybox:1.37.0", "sh", "-c", "while :; do sleep 1; done")
		var before []map[string]any
		if err := json.Unmarshal([]byte(docker("inspect", oldID)), &before); err != nil {
			t.Fatal(err)
		}
		var list httpapi.ContainerList
		deadline := time.Now().Add(15 * time.Second)
		var engine string
		for engine == "" && time.Now().Before(deadline) {
			call("GET", "/containers?node_id="+id, "", nil, &list)
			for _, item := range list.Items {
				if item.ContainerId == oldID {
					engine = item.EngineId
				}
			}
			if engine == "" {
				time.Sleep(200 * time.Millisecond)
			}
		}
		if engine == "" {
			t.Fatal("未发现验收容器")
		}
		var op cluster.Operation
		call("POST", "/nodes/"+id+"/containers/"+oldID+"/actions", "update-container", map[string]any{"engine_id": engine, "action": "update"}, &op)
		waitOperation(op.ID, succeeded)
		var after []map[string]any
		if err := json.Unmarshal([]byte(docker("inspect", name)), &after); err != nil {
			t.Fatal(err)
		}
		if after[0]["Id"] == oldID || after[0]["Image"] == oldImage {
			t.Fatal("镜像或容器未更新")
		}
		for _, field := range []string{"Env", "Cmd", "Entrypoint", "WorkingDir", "User", "Volumes"} {
			a, _ := json.Marshal(before[0]["Config"].(map[string]any)[field])
			b, _ := json.Marshal(after[0]["Config"].(map[string]any)[field])
			if !bytes.Equal(a, b) {
				t.Fatalf("容器配置 %s 改变", field)
			}
		}
		mounts := func(value any) map[string]map[string]any {
			out := map[string]map[string]any{}
			for _, item := range value.([]any) {
				m := item.(map[string]any)
				// inspect 可调整返回顺序，并将默认 rw 显式写入 Mode。
				modes := []string{}
				for _, mode := range strings.Split(fmt.Sprint(m["Mode"]), ",") {
					if mode != "" && mode != "rw" {
						modes = append(modes, mode)
					}
				}
				m["Mode"] = strings.Join(modes, ",")
				out[m["Destination"].(string)] = m
			}
			return out
		}
		a, _ := json.Marshal(mounts(before[0]["Mounts"]))
		b, _ := json.Marshal(mounts(after[0]["Mounts"]))
		if !bytes.Equal(a, b) {
			t.Fatalf("挂载改变：%s / %s", a, b)
		}
		if after[0]["HostConfig"].(map[string]any)["RestartPolicy"].(map[string]any)["Name"] != "unless-stopped" || after[0]["State"].(map[string]any)["Running"] != true {
			t.Fatal("重启策略或运行状态改变")
		}
		networks := after[0]["NetworkSettings"].(map[string]any)["Networks"].(map[string]any)
		if len(networks) != 1 || networks[network] == nil {
			t.Fatal("网络改变")
		}
		if err := exec.Command("docker", "inspect", oldID).Run(); err == nil {
			t.Fatal("成功后原容器未移除")
		}
		report["container_update"] = true
	})

	t.Run("Compose登记与操作", func(t *testing.T) {
		activeTest = t
		dir := t.TempDir()
		file := filepath.Join(dir, "compose.yaml")
		if err := os.WriteFile(file, []byte("services:\n  worker:\n    image: busybox:1.37.0\n    command: [sh, -c, 'while :; do sleep 1; done']\n"), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"compose", "--project-name", "opsd-api-compose", "--project-directory", dir, "--file", file}
		docker(append(append([]string{}, args...), "up", "-d")...)
		t.Cleanup(func() { _ = exec.Command("docker", append(append([]string{}, args...), "down", "--volumes")...).Run() })
		var project httpapi.ComposeProjectItem
		deadline := time.Now().Add(15 * time.Second)
		for project.Id == "" && time.Now().Before(deadline) {
			var list httpapi.ComposeProjectList
			call("GET", "/compose-projects?node_id="+id, "", nil, &list)
			for _, item := range list.Items {
				if item.Name == "opsd-api-compose" {
					project = item
				}
			}
			if project.Id == "" {
				time.Sleep(200 * time.Millisecond)
			}
		}
		if project.Id == "" || project.ReadOnlyReason != nil {
			t.Fatal("Compose 未发现或只读", project)
		}
		var op cluster.Operation
		path := "/nodes/" + id + "/compose-projects/" + project.Id
		call("POST", path+"/register", "compose-register", map[string]string{"engine_id": project.EngineId}, &op)
		waitOperation(op.ID, succeeded)
		before := docker(append(append([]string{}, args...), "ps", "--all", "--quiet")...)
		for _, action := range []string{"pull", "stop", "start", "recreate", "update"} {
			call("POST", path+"/actions", "compose-"+action, map[string]string{"engine_id": project.EngineId, "action": action}, &op)
			waitOperation(op.ID, succeeded)
			state := docker("inspect", docker(append(append([]string{}, args...), "ps", "--all", "--quiet")...), "--format", "{{.State.Running}}")
			if (state == "true") != (action != "stop") {
				t.Fatalf("%s 状态无效：%s", action, state)
			}
		}
		after := docker(append(append([]string{}, args...), "ps", "--all", "--quiet")...)
		if before == after {
			t.Fatal("重建未替换容器")
		}
		var logs protocol.ExecutionLogs
		call("GET", "/nodes/"+id+"/executions/"+op.ID+"/logs", "", nil, &logs)
		if len(logs.Data) == 0 {
			t.Fatal("Compose 日志缺失")
		}
		report["compose"] = true
	})

	t.Run("root终端交互与释放", func(t *testing.T) {
		activeTest = t
		var ticket httpapi.TerminalTicket
		savedCSRF := csrf
		csrf = ""
		if call("POST", "/nodes/"+id+"/terminal", "", nil, nil) != 403 {
			t.Fatal("终端创建未验证 CSRF")
		}
		csrf = savedCSRF
		open := func() (transport.TerminalSocket, string) {
			t.Helper()
			call("POST", "/nodes/"+id+"/terminal", "", nil, &ticket)
			headers := http.Header{"Origin": {base}}
			socket, _, err := websocket.Dial(t.Context(), strings.Replace(base, "http", "ws", 1)+ticket.Url, &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
			if err != nil {
				t.Fatal(err)
			}
			socket.SetReadLimit(65536)
			return transport.TerminalSocket{Socket: socket}, ticket.Url
		}
		stream, used := open()
		defer stream.Close()
		if socket, response, err := websocket.Dial(t.Context(), strings.Replace(base, "http", "ws", 1)+used, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {base}}}); err == nil {
			socket.CloseNow()
			t.Fatal("终端票据被复用")
		} else if response == nil || response.StatusCode != 403 {
			t.Fatal("票据复用错误状态", err)
		}
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "resize", Cols: 101, Rows: 37}); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "pid")
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "input", Data: []byte("printf 'UID:%s;SIZE:%s\\n' \"$(id -u)\" \"$(stty size)\"; echo $$ > " + marker + "; chmod 644 " + marker + "\n")}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		var output strings.Builder
		hasOutput := func(value string) bool {
			return strings.Contains(strings.ReplaceAll(output.String(), "\r", ""), value)
		}
		for !hasOutput("UID:0;SIZE:37 101") {
			frame, err := stream.Receive(ctx)
			if err != nil {
				t.Fatalf("终端交互未取得结果：%v %q", err, output.String())
			}
			output.Write(frame.Data)
		}
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "input", Data: []byte("sleep 60\n")}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "input", Data: []byte{3}}); err != nil {
			t.Fatal(err)
		}
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "input", Data: []byte("printf 'INTERRUPT:%s\\n' \"$?\"\n")}); err != nil {
			t.Fatal(err)
		}
		for !hasOutput("INTERRUPT:130") {
			frame, err := stream.Receive(ctx)
			if err != nil {
				t.Fatal(err)
			}
			output.Write(frame.Data)
		}
		readPID := func() string {
			t.Helper()
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			return strings.TrimSpace(string(data))
		}
		pid := readPID()
		waitGone := func(pid string) {
			t.Helper()
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat("/proc/" + pid); os.IsNotExist(err) {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatal("终端进程未释放：", pid)
		}
		stream.Close()
		waitGone(pid)
		stream, _ = open()
		defer stream.Close()
		if err := stream.Send(t.Context(), protocol.TerminalFrame{Type: "input", Data: []byte("echo $$ > " + marker + "; chmod 644 " + marker + "\n")}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for readPID() == pid && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		pid = readPID()
		call("DELETE", "/auth/session", "", nil, nil)
		waitGone(pid)
		login()
		u, _ := url.Parse(base)
		if len(client.Jar.Cookies(u)) == 0 {
			t.Fatal("重新登录无会话")
		}
		report["terminal"] = true
	})
}
