package dockerengine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/containerd/errdefs"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"io"
	"strings"
	"time"
)

const sourceImageLabel = "com.opsd.source-image"
const updateOperationLabel = "com.opsd.update-operation"

// UpdateState 只保存在目标执行器，配置中的环境变量不进入共享操作记录。
type UpdateState struct {
	OperationID string                    `json:"operation_id"`
	OldID       string                    `json:"old_id"`
	NewID       string                    `json:"new_id"`
	Name        string                    `json:"name"`
	TempName    string                    `json:"temp_name"`
	RetiredName string                    `json:"retired_name"`
	Image       string                    `json:"image"`
	ImageID     string                    `json:"image_id"`
	OldImageID  string                    `json:"old_image_id"`
	WasRunning  bool                      `json:"was_running"`
	NoChange    bool                      `json:"no_change"`
	Config      *container.Config         `json:"config"`
	HostConfig  *container.HostConfig     `json:"host_config"`
	Networks    *network.NetworkingConfig `json:"networks"`
	Mounts      []protocol.ContainerMount `json:"mounts"`
	Stage       string                    `json:"stage"`
	Intent      string                    `json:"intent"`
}

func prepareUpdate(ref protocol.ContainerRef, raw container.InspectResponse, id string) (UpdateState, error) {
	current := detail(ref, raw)
	if current.ReadOnlyReason != "" {
		return UpdateState{}, failure("conflict", current.ReadOnlyReason, "")
	}
	if current.Project != "" {
		return UpdateState{}, failure("conflict", "请通过 Compose 项目更新此容器", "")
	}
	if raw.Config == nil || raw.HostConfig == nil || raw.State == nil {
		return UpdateState{}, failure("conflict", "容器配置不完整", "")
	}
	if raw.HostConfig.AutoRemove || raw.State.Paused || raw.State.Restarting || raw.State.Dead {
		return UpdateState{}, failure("conflict", "此容器的自动删除设置或当前状态不支持镜像更新", "")
	}
	image := raw.Config.Labels[sourceImageLabel]
	if image == "" {
		image = raw.Config.Image
	}
	if strings.HasPrefix(image, "sha256:") {
		return UpdateState{}, failure("conflict", "镜像使用固定 ID，请先通过 SSH 修改镜像引用", "")
	}
	state := UpdateState{OperationID: id, OldID: raw.ID, Name: strings.TrimPrefix(raw.Name, "/"), TempName: "opsd-new-" + id, RetiredName: "opsd-old-" + id, Image: image, OldImageID: raw.Image, WasRunning: raw.State.Running, Stage: "prepared", Mounts: current.Mounts}
	clone := func(from, to any) error {
		b, e := json.Marshal(from)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, to)
	}
	if err := clone(raw.Config, &state.Config); err != nil {
		return state, err
	}
	if err := clone(raw.HostConfig, &state.HostConfig); err != nil {
		return state, err
	}
	covered := map[string]bool{}
	for _, m := range state.HostConfig.Mounts {
		covered[m.Target] = true
	}
	for _, bind := range state.HostConfig.Binds {
		parts := strings.Split(bind, ":")
		if len(parts) > 1 {
			covered[parts[1]] = true
		}
	}
	for _, m := range raw.Mounts {
		if m.Type == mount.TypeVolume && !covered[m.Destination] {
			state.HostConfig.Mounts = append(state.HostConfig.Mounts, mount.Mount{Type: mount.TypeVolume, Source: m.Name, Target: m.Destination, ReadOnly: !m.RW, VolumeOptions: &mount.VolumeOptions{NoCopy: true}})
		}
	}
	if raw.NetworkSettings != nil && !state.HostConfig.NetworkMode.IsHost() && !state.HostConfig.NetworkMode.IsNone() && !state.HostConfig.NetworkMode.IsContainer() {
		state.Networks = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
		for name, ep := range raw.NetworkSettings.Networks {
			if ep == nil {
				continue
			}
			aliases := []string{}
			for _, alias := range ep.Aliases {
				if alias != raw.ID && alias != raw.ID[:12] {
					aliases = append(aliases, alias)
				}
			}
			state.Networks.EndpointsConfig[name] = &network.EndpointSettings{IPAMConfig: ep.IPAMConfig.Copy(), Links: ep.Links, Aliases: aliases, DriverOpts: ep.DriverOpts, MacAddress: ep.MacAddress, GwPriority: ep.GwPriority}
		}
	}
	if state.Config.Labels == nil {
		state.Config.Labels = map[string]string{}
	}
	state.Config.Labels[sourceImageLabel] = image
	state.Config.Labels[updateOperationLabel] = id
	return state, nil
}

func (m *Manager) Update(ctx context.Context, ref protocol.ContainerRef, id string, state *UpdateState, persist func() error, log io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	c, raw, err := m.inspect(ctx, ref, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	if state.Stage != "" {
		return failure("conflict", "原更新步骤已经开始，请核对原操作", "")
	}
	*state, err = prepareUpdate(ref, raw, id)
	if err != nil {
		return err
	}
	if err = persist(); err != nil {
		return err
	}
	step := func(name string, run func() error) error {
		state.Intent = name
		if e := persist(); e != nil {
			return e
		}
		fmt.Fprintln(log, name)
		if e := run(); e != nil {
			return e
		}
		state.Stage = name
		state.Intent = ""
		return persist()
	}
	if err = step("pull", func() error {
		response, e := c.ImagePull(ctx, state.Image, client.ImagePullOptions{})
		if e != nil {
			return e
		}
		for message, e := range response.JSONMessages(ctx) {
			if e != nil {
				return e
			}
			if message.Error != nil {
				return fmt.Errorf("%s", message.Error.Message)
			}
			if message.Status != "" {
				fmt.Fprintln(log, message.ID, message.Status)
			}
		}
		image, e := c.ImageInspect(ctx, state.Image)
		if e != nil {
			return e
		}
		state.ImageID = image.ID
		return nil
	}); err != nil {
		return err
	}
	if state.ImageID == state.OldImageID {
		state.NoChange = true
		state.Stage = "complete"
		return persist()
	}
	state.Config.Image = state.ImageID
	if err = step("create", func() error {
		result, e := c.ContainerCreate(ctx, client.ContainerCreateOptions{Config: state.Config, HostConfig: state.HostConfig, NetworkingConfig: state.Networks, Name: state.TempName})
		if e == nil {
			state.NewID = result.ID
		}
		return e
	}); err != nil {
		return err
	}
	if err = step("stop", func() error {
		if !state.WasRunning {
			return nil
		}
		_, e := c.ContainerStop(ctx, state.OldID, client.ContainerStopOptions{})
		return e
	}); err != nil {
		return err
	}
	if err = step("rename_old", func() error {
		_, e := c.ContainerRename(ctx, state.OldID, client.ContainerRenameOptions{NewName: state.RetiredName})
		return e
	}); err != nil {
		return err
	}
	if err = step("rename_new", func() error {
		_, e := c.ContainerRename(ctx, state.NewID, client.ContainerRenameOptions{NewName: state.Name})
		return e
	}); err != nil {
		return err
	}
	if err = step("start", func() error {
		if !state.WasRunning {
			return nil
		}
		_, e := c.ContainerStart(ctx, state.NewID, client.ContainerStartOptions{})
		return e
	}); err != nil {
		return err
	}
	result, err := c.ContainerInspect(ctx, state.NewID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	if !updatedContainerMatches(result.Container, state) {
		return failure("conflict", "新容器的状态、挂载或网络未通过读回，原容器已保留", "")
	}
	if err = step("remove_old", func() error {
		_, e := c.ContainerRemove(ctx, state.OldID, client.ContainerRemoveOptions{RemoveVolumes: false})
		return e
	}); err != nil {
		return err
	}
	state.Stage = "complete"
	return persist()
}

func updatedContainerMatches(raw container.InspectResponse, state *UpdateState) bool {
	if raw.Config == nil || raw.HostConfig == nil || raw.State == nil || strings.TrimPrefix(raw.Name, "/") != state.Name || raw.Image != state.ImageID || raw.Config.Labels[updateOperationLabel] != state.OperationID || raw.State.Running != state.WasRunning {
		return false
	}
	for _, expected := range state.Mounts {
		found := false
		for _, actual := range raw.Mounts {
			if actual.Destination == expected.Destination && actual.Source == expected.Source && string(actual.Type) == expected.Type && !actual.RW == expected.ReadOnly {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if raw.HostConfig.NetworkMode != state.HostConfig.NetworkMode {
		return false
	}
	if state.Networks != nil {
		if raw.NetworkSettings == nil {
			return false
		}
		for name := range state.Networks.EndpointsConfig {
			if _, ok := raw.NetworkSettings.Networks[name]; !ok {
				return false
			}
		}
	}
	return true
}
func (m *Manager) CheckUpdate(ctx context.Context, ref protocol.ContainerRef, state *UpdateState) (bool, error) {
	c, engine, _, err := m.connect(ctx)
	if err != nil {
		return false, err
	}
	defer c.Close()
	if engine != ref.EngineID {
		return false, failure("conflict", "Docker Engine 身份已变化", "")
	}
	if state.NoChange {
		result, e := c.ContainerInspect(ctx, state.OldID, client.ContainerInspectOptions{})
		return e == nil && result.Container.Image == state.ImageID, e
	}
	id := state.NewID
	if id == "" {
		id = state.TempName
	}
	result, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return false, err
	}
	if result.Container.Config == nil || result.Container.Config.Labels[updateOperationLabel] != state.OperationID {
		return false, failure("conflict", "候选容器身份不匹配", "")
	}
	state.NewID = result.Container.ID
	if !updatedContainerMatches(result.Container, state) {
		return false, nil
	}
	_, err = c.ContainerInspect(ctx, state.OldID, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return true, nil
	}
	return false, err
}
