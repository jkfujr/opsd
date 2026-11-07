// Package dockerengine 通过本机 Unix Socket 管理 Docker，SDK 依赖只留在本包。
package dockerengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const MaxLogBytes = 256 * 1024

type Fault struct{ Code, Message, State string }

func (f *Fault) Error() string                  { return f.Message }
func failure(code, message, state string) error { return &Fault{code, message, state} }

type Manager struct{ Socket string }

func New() *Manager { return &Manager{Socket: "/var/run/docker.sock"} }

func (m *Manager) connect(ctx context.Context) (*client.Client, string, string, error) {
	if runtime.GOOS != "linux" {
		return nil, "", "", failure("dependency_unavailable", "容器管理仅支持 Linux Docker", "unsupported")
	}
	if _, err := os.Stat(m.Socket); err != nil {
		if os.IsNotExist(err) {
			if _, err := exec.LookPath("docker"); err == nil {
				return nil, "", "", failure("dependency_unavailable", "Docker 服务不可用", "unavailable")
			}
			return nil, "", "", failure("dependency_unavailable", "Docker 未安装", "not_installed")
		}
		return nil, "", "", failure("dependency_unavailable", "Docker Socket 不可用", "unavailable")
	}
	c, err := client.New(client.WithHost("unix://"+m.Socket), client.WithAPIVersionNegotiation(), client.WithTimeout(15*time.Second))
	if err != nil {
		return nil, "", "", failure("dependency_unavailable", "Docker 客户端初始化失败", "unavailable")
	}
	info, err := c.Info(ctx, client.InfoOptions{})
	if err != nil {
		c.Close()
		return nil, "", "", failure("dependency_unavailable", "Docker 服务不可用", "unavailable")
	}
	for _, option := range info.Info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			c.Close()
			return nil, "", "", failure("dependency_unavailable", "当前版本仅支持 rootful Docker", "unsupported")
		}
	}
	if info.Info.ID == "" || info.Info.OSType != "linux" {
		c.Close()
		return nil, "", "", failure("dependency_unavailable", "Docker Engine 身份或系统无效", "unsupported")
	}
	return c, info.Info.ID, info.Info.ServerVersion, nil
}

func (m *Manager) List(ctx context.Context) protocol.DockerState {
	state := protocol.DockerState{State: "ready", Containers: []protocol.ContainerSummary{}, CheckedAt: time.Now().UTC()}
	c, id, version, err := m.connect(ctx)
	if err == nil {
		defer c.Close()
		state.EngineID, state.Version = id, version
		var result client.ContainerListResult
		result, err = c.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err == nil {
			for _, item := range result.Items {
				summary := protocol.ContainerSummary{ContainerRef: protocol.ContainerRef{EngineID: id, ContainerID: item.ID}, Image: item.Image, State: string(item.State), Health: health(item.Status), Ports: []protocol.ContainerPort{}, Project: item.Labels["com.docker.compose.project"], Service: item.Labels["com.docker.compose.service"], ReadOnlyReason: readOnly(item.Labels)}
				if len(item.Names) > 0 {
					summary.Name = strings.TrimPrefix(item.Names[0], "/")
				}
				if item.Health != nil {
					summary.Health = string(item.Health.Status)
				}
				for _, port := range item.Ports {
					summary.Ports = append(summary.Ports, protocol.ContainerPort{HostIP: ipText(port.IP), HostPort: port.PublicPort, ContainerPort: port.PrivatePort, Protocol: port.Type})
				}
				state.Containers = append(state.Containers, summary)
			}
			sort.Slice(state.Containers, func(i, j int) bool { return state.Containers[i].Name < state.Containers[j].Name })
			return state
		}
	}
	state.State = "unavailable"
	state.Error = &protocol.Error{Code: "dependency_unavailable", Message: "Docker 容器读取失败"}
	var f *Fault
	if errors.As(err, &f) {
		state.State = f.State
		state.Error.Code = f.Code
		state.Error.Message = f.Message
	}
	return state
}

func health(status string) string {
	if strings.Contains(status, "(unhealthy)") {
		return "unhealthy"
	}
	if strings.Contains(status, "(healthy)") {
		return "healthy"
	}
	if strings.Contains(status, "(health: starting)") {
		return "starting"
	}
	return "none"
}
func readOnly(labels map[string]string) string {
	if labels["opsd.storage"] != "" {
		return "通过 OPSD 数据设置管理"
	}
	if labels["com.docker.swarm.task.id"] != "" {
		return "由 Swarm 管理"
	}
	return ""
}
func (m *Manager) inspect(ctx context.Context, ref protocol.ContainerRef) (*client.Client, container.InspectResponse, error) {
	if err := ref.Validate(); err != nil {
		return nil, container.InspectResponse{}, failure("invalid_request", err.Error(), "")
	}
	c, id, _, err := m.connect(ctx)
	if err != nil {
		return nil, container.InspectResponse{}, err
	}
	if id != ref.EngineID {
		c.Close()
		return nil, container.InspectResponse{}, failure("conflict", "Docker Engine 身份已变化，请刷新列表", "")
	}
	result, err := c.ContainerInspect(ctx, ref.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		c.Close()
		if errdefs.IsNotFound(err) {
			return nil, container.InspectResponse{}, failure("not_found", "容器已移除，请刷新列表", "")
		}
		return nil, container.InspectResponse{}, failure("dependency_unavailable", "容器详情读取失败", "")
	}
	if result.Container.ID != ref.ContainerID {
		c.Close()
		return nil, container.InspectResponse{}, failure("conflict", "容器身份已变化", "")
	}
	return c, result.Container, nil
}
func timeValue(value string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.IsZero() {
		return nil
	}
	return &t
}
func ipText(value netip.Addr) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}
func (m *Manager) Inspect(ctx context.Context, ref protocol.ContainerRef) (protocol.ContainerDetail, error) {
	c, raw, err := m.inspect(ctx, ref)
	if err != nil {
		return protocol.ContainerDetail{}, err
	}
	defer c.Close()
	return detail(ref, raw), nil
}
func detail(ref protocol.ContainerRef, raw container.InspectResponse) protocol.ContainerDetail {
	v := protocol.ContainerDetail{ContainerSummary: protocol.ContainerSummary{ContainerRef: ref, Name: strings.TrimPrefix(raw.Name, "/"), Ports: []protocol.ContainerPort{}, Health: "none"}, Mounts: []protocol.ContainerMount{}, Networks: []protocol.ContainerNetwork{}, CheckedAt: time.Now().UTC(), RestartCount: raw.RestartCount}
	if t := timeValue(raw.Created); t != nil {
		v.CreatedAt = *t
	}
	if raw.Config != nil {
		v.Image = raw.Config.Image
		v.TTY = raw.Config.Tty
		v.Project = raw.Config.Labels["com.docker.compose.project"]
		v.Service = raw.Config.Labels["com.docker.compose.service"]
		v.ReadOnlyReason = readOnly(raw.Config.Labels)
	}
	if raw.State != nil {
		v.State = string(raw.State.Status)
		v.StartedAt = timeValue(raw.State.StartedAt)
		v.FinishedAt = timeValue(raw.State.FinishedAt)
		if raw.State.Health != nil {
			v.Health = string(raw.State.Health.Status)
		}
	}
	if raw.HostConfig != nil {
		v.RestartPolicy = string(raw.HostConfig.RestartPolicy.Name)
	}
	for _, mount := range raw.Mounts {
		v.Mounts = append(v.Mounts, protocol.ContainerMount{Type: string(mount.Type), Source: mount.Source, Destination: mount.Destination, ReadOnly: !mount.RW})
	}
	if raw.NetworkSettings != nil {
		for name, network := range raw.NetworkSettings.Networks {
			if network != nil {
				v.Networks = append(v.Networks, protocol.ContainerNetwork{Name: name, IPv4: ipText(network.IPAddress), IPv6: ipText(network.GlobalIPv6Address)})
			}
		}
		for port, bindings := range raw.NetworkSettings.Ports {
			number, transport := port.Num(), string(port.Proto())
			if len(bindings) == 0 {
				v.Ports = append(v.Ports, protocol.ContainerPort{ContainerPort: uint16(number), Protocol: transport})
			}
			for _, binding := range bindings {
				hostPort, _ := strconv.ParseUint(binding.HostPort, 10, 16)
				v.Ports = append(v.Ports, protocol.ContainerPort{HostIP: ipText(binding.HostIP), HostPort: uint16(hostPort), ContainerPort: number, Protocol: transport})
			}
		}
	}
	sort.Slice(v.Networks, func(i, j int) bool { return v.Networks[i].Name < v.Networks[j].Name })
	sort.Slice(v.Ports, func(i, j int) bool {
		a, b := v.Ports[i], v.Ports[j]
		return fmt.Sprint(a.ContainerPort, a.Protocol, a.HostIP, a.HostPort) < fmt.Sprint(b.ContainerPort, b.Protocol, b.HostIP, b.HostPort)
	})
	return v
}
func (m *Manager) Change(ctx context.Context, action protocol.ContainerAction) error {
	if err := action.Validate(); err != nil {
		return failure("invalid_request", err.Error(), "")
	}
	c, raw, err := m.inspect(ctx, action.ContainerRef)
	if err != nil {
		return err
	}
	defer c.Close()
	current := detail(action.ContainerRef, raw)
	if current.ReadOnlyReason != "" {
		return failure("conflict", current.ReadOnlyReason, "")
	}
	if current.State == "paused" || current.State == "restarting" || current.State == "removing" {
		return failure("conflict", "容器当前状态不支持此操作", "")
	}
	if action.Action == "restart" && current.State != "running" {
		return failure("conflict", "请先启动容器", "")
	}
	switch action.Action {
	case "start":
		_, err = c.ContainerStart(ctx, action.ContainerID, client.ContainerStartOptions{})
	case "stop":
		_, err = c.ContainerStop(ctx, action.ContainerID, client.ContainerStopOptions{})
	case "restart":
		_, err = c.ContainerRestart(ctx, action.ContainerID, client.ContainerRestartOptions{})
	}
	if err != nil {
		return failure("dependency_unavailable", "Docker 操作应答未知，请核对原操作", "")
	}
	return nil
}

type boundedLog struct {
	bytes.Buffer
	truncated bool
}

var errLogLimit = errors.New("日志达到读取上限")

func (b *boundedLog) Write(p []byte) (int, error) {
	remaining := MaxLogBytes - b.Len()
	if len(p) > remaining {
		b.Buffer.Write(p[:remaining])
		b.truncated = true
		return remaining, errLogLimit
	}
	return b.Buffer.Write(p)
}
func (m *Manager) Logs(ctx context.Context, request protocol.ContainerLogsRead) (protocol.ContainerLogs, error) {
	if request.Tail < 1 || request.Tail > 1000 {
		return protocol.ContainerLogs{}, failure("invalid_request", "日志行数须为 1 至 1000", "")
	}
	c, raw, err := m.inspect(ctx, request.ContainerRef)
	if err != nil {
		return protocol.ContainerLogs{}, err
	}
	defer c.Close()
	out, err := c.ContainerLogs(ctx, request.ContainerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Tail: strconv.Itoa(request.Tail)})
	if err != nil {
		return protocol.ContainerLogs{}, failure("dependency_unavailable", "日志读取失败，请检查容器日志驱动", "")
	}
	defer out.Close()
	buffer := &boundedLog{}
	if raw.Config != nil && raw.Config.Tty {
		_, err = io.Copy(buffer, out)
	} else {
		_, err = stdcopy.StdCopy(buffer, buffer, out)
	}
	if err != nil && !errors.Is(err, errLogLimit) {
		return protocol.ContainerLogs{}, failure("dependency_unavailable", "日志读取中断", "")
	}
	return protocol.ContainerLogs{ContainerRef: request.ContainerRef, Text: strings.ToValidUTF8(buffer.String(), "�"), Truncated: buffer.truncated, CheckedAt: time.Now().UTC()}, nil
}
