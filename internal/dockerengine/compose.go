package dockerengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/client"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func composeFiles(value, directory string) []string {
	files := []string{}
	for _, file := range strings.Split(value, ",") {
		if file = strings.TrimSpace(file); file != "" {
			if !filepath.IsAbs(file) {
				file = filepath.Join(directory, file)
			}
			files = append(files, filepath.Clean(file))
		}
	}
	return files
}
func composeID(ref protocol.ComposeRef) string {
	body, _ := json.Marshal(ref)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func sameCompose(a, b protocol.ComposeRef) bool {
	a.ID = ""
	b.ID = ""
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (m *Manager) Projects(ctx context.Context) ([]protocol.ComposeProject, error) {
	c, engine, _, e := m.connect(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	list, e := c.ContainerList(ctx, client.ContainerListOptions{All: true})
	if e != nil {
		return nil, e
	}
	projects := map[string]*protocol.ComposeProject{}
	for _, item := range list.Items {
		name := item.Labels["com.docker.compose.project"]
		if name == "" {
			continue
		}
		directory := item.Labels["com.docker.compose.project.working_dir"]
		ref := protocol.ComposeRef{EngineID: engine, Name: name, Directory: directory, Files: composeFiles(item.Labels["com.docker.compose.project.config_files"], directory), EnvironmentFiles: composeFiles(item.Labels["com.docker.compose.project.environment_file"], directory)}
		ref.ID = composeID(ref)
		project := projects[name]
		if project == nil {
			project = &protocol.ComposeProject{ComposeRef: ref, Services: []protocol.ContainerSummary{}, CheckedAt: time.Now().UTC()}
			projects[name] = project
		} else if !sameCompose(project.ComposeRef, ref) {
			project.ReadOnlyReason = "项目标签中的定义路径不一致"
		}
		if err := ref.Validate(); err != nil {
			project.ReadOnlyReason = err.Error()
		}
		if reason := readOnly(item.Labels); reason != "" {
			project.ReadOnlyReason = reason
		}
		if strings.EqualFold(item.Labels["com.docker.compose.oneoff"], "true") {
			continue
		}
		service := protocol.ContainerSummary{ContainerRef: protocol.ContainerRef{EngineID: engine, ContainerID: item.ID}, Name: item.ID[:12], Image: item.Image, State: string(item.State), Project: name, Service: item.Labels["com.docker.compose.service"], Health: health(item.Status), Ports: []protocol.ContainerPort{}}
		if len(item.Names) > 0 {
			service.Name = strings.TrimPrefix(item.Names[0], "/")
		}
		project.Services = append(project.Services, service)
	}
	cliCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, cliErr := exec.CommandContext(cliCtx, "docker", "compose", "version", "--short").Output()
	out := []protocol.ComposeProject{}
	for _, project := range projects {
		if cliErr != nil {
			project.ReadOnlyReason = "Docker Compose 插件不可用"
		}
		for _, file := range append(append([]string{}, project.Files...), project.EnvironmentFiles...) {
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() {
				project.ReadOnlyReason = "Compose 定义或环境文件不存在"
			}
		}
		if info, err := os.Stat(project.Directory); err != nil || !info.IsDir() {
			project.ReadOnlyReason = "Compose 定义目录不存在"
		}
		sort.Slice(project.Services, func(i, j int) bool { return project.Services[i].Name < project.Services[j].Name })
		out = append(out, *project)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *Manager) ChangeCompose(ctx context.Context, action protocol.ComposeAction, log io.Writer) error {
	if e := action.Validate(); e != nil {
		return e
	}
	projects, e := m.Projects(ctx)
	if e != nil {
		return e
	}
	found := false
	for _, project := range projects {
		if project.ID == action.Project.ID && sameCompose(project.ComposeRef, action.Project) {
			if project.ReadOnlyReason != "" {
				return failure("conflict", project.ReadOnlyReason, "")
			}
			found = true
		}
	}
	if !found {
		return failure("conflict", "Compose 实际标签与登记资料不同，请重新读取", "")
	}
	args := []string{"--host=unix://" + m.Socket, "compose", "--ansi", "never", "--project-name", action.Project.Name, "--project-directory", action.Project.Directory}
	for _, file := range action.Project.Files {
		args = append(args, "--file", file)
	}
	for _, file := range action.Project.EnvironmentFiles {
		args = append(args, "--env-file", file)
	}
	switch action.Action {
	case "pull":
		args = append(args, "pull")
	case "start":
		args = append(args, "up", "--detach", "--no-recreate", "--no-build", "--wait", "--wait-timeout", "120")
	case "stop":
		args = append(args, "stop")
	case "recreate":
		args = append(args, "up", "--detach", "--force-recreate", "--no-build", "--wait", "--wait-timeout", "120")
	default:
		return fmt.Errorf("Compose 步骤无效")
	}
	command := exec.CommandContext(ctx, "docker", args...)
	command.Dir = action.Project.Directory
	command.Stdout = log
	command.Stderr = log
	return command.Run()
}
