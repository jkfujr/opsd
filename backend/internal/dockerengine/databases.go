package dockerengine

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/client"
)

func (m *Manager) StartDatabaseProxy(ctx context.Context, ref protocol.ContainerRef) error {
	c, raw, err := m.inspect(ctx, ref)
	if err != nil {
		return err
	}
	defer c.Close()
	image := ""
	if raw.Config != nil {
		image = raw.Config.Image
		if raw.Config.Labels[sourceImageLabel] != "" {
			image = raw.Config.Labels[sourceImageLabel]
		}
	}
	if !strings.Contains(strings.ToLower(image), "proxysql") || raw.State == nil || raw.State.Paused || raw.State.Status == "removing" {
		return fmt.Errorf("代理容器身份或状态已变化")
	}
	if m.StorageDatabaseStart != nil && raw.Config != nil {
		if id := raw.Config.Labels[deploymentLabel]; id != "" {
			if handled, err := m.StorageDatabaseStart(ctx, id); handled || err != nil {
				return err
			}
		}
	}
	_, err = c.ContainerStart(ctx, ref.ContainerID, client.ContainerStartOptions{})
	return err
}

func (m *Manager) DatabaseInstances(ctx context.Context) ([]protocol.DatabaseCandidate, error) {
	c, _, _, e := m.connect(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	list, e := c.ContainerList(ctx, client.ContainerListOptions{All: true})
	if e != nil {
		return nil, e
	}
	out := []protocol.DatabaseCandidate{}
	for _, item := range list.Items {
		image := strings.ToLower(item.Image)
		kind := ""
		port := 3306
		if strings.Contains(image, "mariadb") {
			kind = "galera"
		} else if strings.Contains(image, "proxysql") {
			kind = "proxysql"
			port = 6032
		} else {
			continue
		}
		detail, e := c.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if e != nil {
			return nil, e
		}
		config := detail.Container.Config
		if config == nil {
			continue
		}
		connection := protocol.DatabaseConnection{Kind: kind, Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Username: "root", ProtectedUsers: []string{}}
		if kind == "proxysql" {
			connection.Username = "admin"
		}
		for _, env := range config.Env {
			key, value, ok := strings.Cut(env, "=")
			if !ok {
				continue
			}
			if key == "MARIADB_ROOT_PASSWORD" || key == "MYSQL_ROOT_PASSWORD" || key == "PROXYSQL_ADMIN_PASSWORD" {
				connection.Password = value
			}
		}
		for _, p := range item.Ports {
			if int(p.PrivatePort) == port && p.PublicPort != 0 {
				connection.Address = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p.PublicPort)))
				break
			}
		}
		name := strings.TrimPrefix(detail.Container.Name, "/")
		out = append(out, protocol.DatabaseCandidate{ID: item.ID, Name: name, Connection: connection, ContainerID: item.ID, Running: string(item.State) == "running"})
	}
	return out, nil
}
