package databaseengine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

// DeploymentProfile 仅返回给所属节点，由认证通道传递扩容所需资料。
func (m Manager) DeploymentProfile(ctx context.Context, request protocol.DatabaseDeploymentProfileRead) (protocol.DatabaseDeploymentProfile, error) {
	out := protocol.DatabaseDeploymentProfile{Problems: []string{}}
	state, err := m.Read(ctx, request.Connection, "health")
	if err != nil {
		return out, err
	}
	out.State = state
	d := protocol.DatabaseDeployment{Kind: "galera", Connection: request.Connection, ClusterName: state.ClusterName, ClusterID: state.ClusterID, Address: state.MemberAddress, SQLPort: 3306, ReplicationPort: 4567, ISTPort: 4568, SSTPort: 4444}
	if request.ContainerID != "" && m.Docker != nil {
		container, e := m.Docker.DeploymentContainer(ctx, request.ContainerID)
		if e != nil {
			return out, e
		}
		if container.Config != nil {
			d.Image = container.Config.Image
		}
		for _, mount := range container.Mounts {
			if mount.Destination == "/etc/mysql/conf.d/opsd.cnf" {
				path := filepath.Join(filepath.Dir(mount.Source), "profile.json")
				if info, e := os.Stat(path); e == nil && info.Mode().IsRegular() && info.Size() <= 256*1024 {
					data, e := os.ReadFile(path)
					if e == nil {
						_ = json.Unmarshal(data, &d)
					}
				}
			}
		}
	}
	s, err := connect(ctx, request.Connection)
	if err != nil {
		return out, err
	}
	defer s.Close()
	values, err := rows(ctx, s, "SHOW GLOBAL VARIABLES WHERE Variable_name IN ('port','wsrep_node_address','wsrep_sst_receive_address','wsrep_sst_auth','wsrep_sst_method','wsrep_provider_options')")
	if err != nil {
		return out, err
	}
	v := map[string]string{}
	for _, item := range values {
		v[strings.ToLower(item["variable_name"])] = item["value"]
	}
	d.ClusterID, d.ClusterName, d.Connection = state.ClusterID, state.ClusterName, request.Connection
	d.SQLPort, _ = strconv.Atoi(v["port"])
	d.Address = v["wsrep_node_address"]
	if host, _, e := net.SplitHostPort(d.Address); e == nil {
		d.Address = host
	}
	d.SSTMethod = v["wsrep_sst_method"]
	d.SSTUser, d.SSTPassword, _ = strings.Cut(v["wsrep_sst_auth"], ":")
	if _, port, e := net.SplitHostPort(v["wsrep_sst_receive_address"]); e == nil {
		d.SSTPort, _ = strconv.Atoi(port)
	}
	for _, option := range strings.Split(v["wsrep_provider_options"], ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(option), "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "gmcast.listen_addr" || key == "ist.recv_addr" {
			if _, port, e := net.SplitHostPort(strings.TrimPrefix(value, "tcp://")); e == nil {
				p, _ := strconv.Atoi(port)
				if key == "gmcast.listen_addr" {
					d.ReplicationPort = p
				} else {
					d.ISTPort = p
				}
			}
		}
		if key == "socket.ssl" && strings.EqualFold(value, "YES") && len(d.TLS) == 0 {
			out.Problems = append(out.Problems, "原集群启用了复制 TLS，请在高级设置补充同步证书")
		}
	}
	if d.Image == "" {
		d.Image = "mariadb:" + strings.Split(state.Version, "-")[0]
	}
	if d.SSTMethod != "mariabackup" {
		out.Problems = append(out.Problems, "原集群同步方式需要 mariabackup")
	}
	if d.SSTPassword == "" {
		out.Problems = append(out.Problems, "请补充原集群数据同步账号和密码")
	}
	if !state.Primary || !state.Ready || !state.Synced {
		out.Problems = append(out.Problems, "原集群尚未达到 Primary / Ready / Synced")
	}
	sizes, err := rows(ctx, s, "SELECT COALESCE(SUM(DATA_LENGTH+INDEX_LENGTH),0) AS bytes FROM information_schema.TABLES")
	if err != nil {
		return out, fmt.Errorf("读取数据大小失败：%w", err)
	}
	if len(sizes) > 0 {
		out.DataBytes = uint64(number(sizes[0]["bytes"]))
	}
	out.Deployment = d
	return out, nil
}
