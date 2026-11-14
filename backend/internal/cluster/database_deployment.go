package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

type DatabaseDeploymentTargetInput struct {
	NodeID    string `json:"node_id"`
	Address   string `json:"address,omitempty"`
	Directory string `json:"directory,omitempty"`
}
type DatabaseDeploymentOptions struct {
	Image           string            `json:"image,omitempty"`
	SQLPort         int               `json:"sql_port,omitempty"`
	ReplicationPort int               `json:"replication_port,omitempty"`
	ISTPort         int               `json:"ist_port,omitempty"`
	SSTPort         int               `json:"sst_port,omitempty"`
	AdminPort       int               `json:"admin_port,omitempty"`
	SSTUser         string            `json:"sst_user,omitempty"`
	SSTPassword     string            `json:"sst_password,omitempty"`
	TLS             map[string]string `json:"tls,omitempty"`
}
type DatabaseDeploymentInput struct {
	Mode             string                          `json:"mode"`
	Name             string                          `json:"name"`
	ClusterID        string                          `json:"cluster_id,omitempty"`
	ExpectedRevision uint64                          `json:"expected_revision"`
	Targets          []DatabaseDeploymentTargetInput `json:"targets"`
	Options          DatabaseDeploymentOptions       `json:"options"`
	Imports          []DatabaseInstanceInput         `json:"imports"`
	GaleraID         string                          `json:"galera_id,omitempty"`
}
type databaseDeploymentTarget struct {
	Ref         DatabaseRef                 `json:"ref"`
	Deployment  protocol.DatabaseDeployment `json:"deployment"`
	Fingerprint string                      `json:"fingerprint"`
}
type DatabaseDeploymentNetwork struct {
	NodeID   string        `json:"node_id"`
	Source   string        `json:"source"`
	Protocol string        `json:"protocol"`
	Ports    string        `json:"ports"`
	Write    FirewallWrite `json:"write"`
}
type DatabaseDeploymentPlan struct {
	ID              string                      `json:"id"`
	Mode            string                      `json:"mode"`
	Actor           string                      `json:"actor"`
	Cluster         ManagedDatabaseCluster      `json:"cluster"`
	Targets         []databaseDeploymentTarget  `json:"targets"`
	Network         []DatabaseDeploymentNetwork `json:"network"`
	Imports         []DatabaseInstanceInput     `json:"imports"`
	ExpiresAt       time.Time                   `json:"expires_at"`
	MonitorUser     string                      `json:"monitor_user,omitempty"`
	MonitorPassword string                      `json:"monitor_password,omitempty"`
}
type DatabaseDeploymentTargetPreview struct {
	NodeID      string   `json:"node_id"`
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	Directory   string   `json:"directory"`
	Image       string   `json:"image"`
	DockerState string   `json:"docker_state"`
	FreeBytes   uint64   `json:"free_bytes"`
	Ports       []int    `json:"ports"`
	Problems    []string `json:"problems"`
}
type DatabaseDeploymentPreview struct {
	ID          string                            `json:"id"`
	Coordinator string                            `json:"coordinator"`
	ClusterID   string                            `json:"cluster_id"`
	Revision    string                            `json:"revision"`
	Token       string                            `json:"token"`
	ExpiresAt   time.Time                         `json:"expires_at"`
	Targets     []DatabaseDeploymentTargetPreview `json:"targets"`
	Network     []DatabaseDeploymentNetwork       `json:"network"`
	Problems    []string                          `json:"problems"`
}
type DatabaseDeploymentCommit struct {
	Coordinator     string `json:"coordinator"`
	Token           string `json:"token"`
	Revision        string `json:"revision"`
	Actor           string `json:"actor"`
	Key             string `json:"key"`
	Digest          string `json:"digest"`
	ExpectedCluster string `json:"expected_cluster"`
	ExpectedMode    string `json:"expected_mode"`
}

func (n *Node) PreviewDatabaseDeployment(ctx context.Context, input DatabaseDeploymentInput, actor string) (DatabaseDeploymentPreview, error) {
	coordinator := ""
	if input.Mode == "import" && input.ClusterID == "" {
		all, err := n.DatabaseClusters(ctx)
		if err != nil {
			return DatabaseDeploymentPreview{}, err
		}
		for _, c := range all {
			if !c.Deleted && c.ClusterID == input.GaleraID {
				input.ClusterID = c.ID
				input.ExpectedRevision = c.Revision
				break
			}
		}
	}
	if input.ClusterID != "" {
		c, err := n.DatabaseCluster(ctx, input.ClusterID)
		if err != nil {
			return DatabaseDeploymentPreview{}, err
		}
		coordinator = c.Coordinator
	} else if input.Mode == "import" && len(input.Imports) > 0 {
		for _, item := range input.Imports {
			if item.Kind == "galera" {
				coordinator = item.NodeID
				break
			}
		}
	} else if len(input.Targets) > 0 {
		coordinator = input.Targets[0].NodeID
	}
	if coordinator == "" || !n.Active(ctx, coordinator) {
		return DatabaseDeploymentPreview{}, fault("invalid_request", "请选择在线数据库节点")
	}
	var out DatabaseDeploymentPreview
	err := n.Call(ctx, coordinator, "database.deployment.preview", databaseDeploymentRequest{Input: input, Actor: actor}, &out)
	return out, err
}

type databaseDeploymentRequest struct {
	Input  DatabaseDeploymentInput  `json:"input"`
	Actor  string                   `json:"actor"`
	Commit DatabaseDeploymentCommit `json:"commit"`
}

func (n *Node) previewDatabaseDeployment(ctx context.Context, input DatabaseDeploymentInput, actor string) (DatabaseDeploymentPreview, error) {
	out := DatabaseDeploymentPreview{Coordinator: n.ID, Targets: []DatabaseDeploymentTargetPreview{}, Network: []DatabaseDeploymentNetwork{}, Problems: []string{}}
	if !slices.Contains([]string{"create", "members", "proxy", "import"}, input.Mode) || len(input.Targets) > 100 || len(input.Imports) > 200 {
		return out, fault("invalid_request", "数据库部署参数无效")
	}
	plan := DatabaseDeploymentPlan{ID: identity.Random(), Mode: input.Mode, Actor: actor, Targets: []databaseDeploymentTarget{}, Network: []DatabaseDeploymentNetwork{}, Imports: input.Imports, ExpiresAt: n.Now().UTC().Add(30 * time.Minute)}
	c := ManagedDatabaseCluster{ID: NewOperationID(n.ID), Name: strings.TrimSpace(input.Name), Coordinator: n.ID, Members: []DatabaseRef{}, Proxies: []DatabaseRef{}}
	if input.ClusterID != "" {
		var err error
		c, err = n.DatabaseCluster(ctx, input.ClusterID)
		if err != nil {
			return out, err
		}
		if c.Coordinator != n.ID || c.Revision != input.ExpectedRevision {
			return out, fault("conflict", "数据库集群配置已经变化")
		}
	} else if input.Mode != "create" && input.Mode != "import" {
		return out, fault("invalid_request", "请选择数据库集群")
	}
	if c.Name == "" || len(c.Name) > 128 {
		return out, fault("invalid_request", "请输入集群名称")
	}
	if input.Mode == "import" {
		if !slices.ContainsFunc(input.Imports, func(i DatabaseInstanceInput) bool { return i.Kind == "galera" && i.NodeID == n.ID }) || input.GaleraID == "" {
			return out, fault("invalid_request", "请选择发现的 Galera 集群")
		}
		c.ClusterID = input.GaleraID
		all, err := n.DatabaseClusters(ctx)
		if err != nil {
			return out, err
		}
		for _, old := range all {
			if !old.Deleted && old.ClusterID == c.ClusterID {
				c = old
				break
			}
		}
		if c.Coordinator != n.ID {
			return out, fault("conflict", "此集群已接入，请从集群详情添加成员")
		}
		foundItems := []DatabaseScanItem{}
		for _, item := range input.Imports {
			var found DatabaseScanItem
			err := n.Call(ctx, item.NodeID, "database.discovery.inspect", DatabaseRPC{Instance: item}, &found)
			if err != nil {
				out.Problems = append(out.Problems, item.Name+"："+err.Error())
				continue
			}
			if found.Instance.Kind == "galera" && (found.Status.ClusterID != c.ClusterID || !found.Status.Primary || !found.Status.Ready || !found.Status.Synced) {
				out.Problems = append(out.Problems, item.Name+"：集群身份或同步状态已经变化")
			}
			if found.Status.Error != nil {
				out.Problems = append(out.Problems, item.Name+"："+found.Status.Error.Message)
			}
			foundItems = append(foundItems, found)
			out.Targets = append(out.Targets, DatabaseDeploymentTargetPreview{NodeID: item.NodeID, Name: found.Instance.Name, Address: found.Instance.Address, Image: found.Status.Version, Ports: []int{}, Problems: []string{}})
		}
		for _, item := range foundItems {
			if item.Instance.Kind == "proxysql" && !proxyMatchesScan(item.Status, c.ClusterID, foundItems) {
				out.Problems = append(out.Problems, item.Instance.Name+"：请选择实际关联此集群的代理")
			}
		}
		plan.Cluster = c
		return n.sealDeploymentPreview(plan, out)
	}
	if len(input.Targets) == 0 {
		return out, fault("invalid_request", "请选择数据库节点")
	}
	d := protocol.DatabaseDeployment{Kind: "galera", Image: databaseengine.GaleraImage, SQLPort: 3306, ReplicationPort: 4567, ISTPort: 4568, SSTPort: 4444, ClusterName: "opsd_" + plan.ID, SSTMethod: "mariabackup", SSTUser: "opsd_sst", SSTPassword: identity.Random(), InstallDocker: true, Connection: protocol.DatabaseConnection{Kind: "galera", Username: "root", Password: identity.Random(), ProtectedUsers: []string{"opsd_sst"}}, MonitorUser: "opsd_monitor", MonitorPassword: identity.Random()}
	seeds := []string{}
	dataBytes := uint64(0)
	existing := []databaseDeploymentTarget{}
	if input.Mode == "create" {
		var err error
		d.TLS, err = databaseengine.NewDeploymentTLS()
		if err != nil {
			return out, err
		}
	} else {
		for _, ref := range c.Members {
			var profile protocol.DatabaseDeploymentProfile
			if err := n.Call(ctx, ref.NodeID, "database.deployment.profile", DatabaseRPC{InstanceID: ref.InstanceID}, &profile); err != nil {
				out.Problems = append(out.Problems, ref.Name+"："+err.Error())
				continue
			}
			if profile.State.ClusterID != c.ClusterID {
				out.Problems = append(out.Problems, ref.Name+"：集群身份已经变化")
				continue
			}
			if !profile.State.Primary || !profile.State.Ready || !profile.State.Synced {
				out.Problems = append(out.Problems, ref.Name+"：请先恢复已有数据库成员同步")
			}
			if ref.NodeID == c.Coordinator {
				d = profile.Deployment
				dataBytes = profile.DataBytes
				if input.Mode == "members" {
					out.Problems = append(out.Problems, profile.Problems...)
				}
			}
			existing = append(existing, databaseDeploymentTarget{Ref: ref, Deployment: profile.Deployment})
			seeds = append(seeds, net.JoinHostPort(profile.Deployment.Address, strconv.Itoa(profile.Deployment.ReplicationPort)))
		}
		if len(existing) != len(c.Members) {
			out.Problems = append(out.Problems, "请恢复已有数据库成员后重新预览")
		}
	}
	if input.Mode == "proxy" {
		plan.MonitorUser, plan.MonitorPassword = "opsd_monitor_"+plan.ID[:8], identity.Random()
		d = protocol.DatabaseDeployment{Kind: "proxysql", Image: databaseengine.ProxyImage, SQLPort: 6033, AdminPort: 6032, InstallDocker: true, Connection: protocol.DatabaseConnection{Kind: "proxysql", Username: "opsdadmin", Password: identity.Random(), ProtectedUsers: []string{"opsdadmin"}}, MonitorUser: plan.MonitorUser, MonitorPassword: plan.MonitorPassword}
		for _, target := range existing {
			d.Backends = append(d.Backends, protocol.ProxyEndpoint{Host: target.Deployment.Address, Port: target.Deployment.SQLPort})
		}
	}
	options := input.Options
	if options.Image != "" {
		if input.Mode == "members" && options.Image != d.Image {
			out.Problems = append(out.Problems, "扩容镜像须与现有成员版本一致")
		} else {
			d.Image = options.Image
		}
	}
	if input.Mode != "members" {
		if options.SQLPort > 0 {
			d.SQLPort = options.SQLPort
		}
		if options.ReplicationPort > 0 {
			d.ReplicationPort = options.ReplicationPort
		}
		if options.ISTPort > 0 {
			d.ISTPort = options.ISTPort
		}
		if options.SSTPort > 0 {
			d.SSTPort = options.SSTPort
		}
		if options.AdminPort > 0 {
			d.AdminPort = options.AdminPort
		}
	}
	if options.SSTPassword != "" {
		d.SSTPassword, d.SSTUser = options.SSTPassword, options.SSTUser
		out.Problems = slices.DeleteFunc(out.Problems, func(s string) bool { return s == "请补充原集群数据同步账号和密码" })
	}
	if len(options.TLS) > 0 {
		d.TLS = options.TLS
		out.Problems = slices.DeleteFunc(out.Problems, func(s string) bool { return s == "原集群启用了复制 TLS，请在高级设置补充同步证书" })
	}
	d.InstallDocker = true
	dir, err := n.Directory(ctx)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for index, target := range input.Targets {
		if seen[target.NodeID] || !n.Active(ctx, target.NodeID) {
			return out, fault("invalid_request", "节点重复或已撤销")
		}
		seen[target.NodeID] = true
		if input.Mode == "members" && slices.ContainsFunc(c.Members, func(r DatabaseRef) bool { return r.NodeID == target.NodeID }) {
			return out, fault("conflict", "此节点已经包含集群成员")
		}
		mi := slices.IndexFunc(dir.Members, func(m Member) bool { return m.ID == target.NodeID })
		if mi < 0 {
			return out, fault("invalid_request", "节点不存在")
		}
		member := dir.Members[mi]
		value := d
		value.ID = plan.ID + "_" + strconv.Itoa(index)
		value.Connection.PoolKey = ""
		value.Connection.Socket = ""
		value.Address = target.Address
		if value.Address == "" {
			u, _ := url.Parse(member.Address)
			if u != nil {
				value.Address = u.Hostname()
			}
		}
		value.Directory = target.Directory
		if value.Directory == "" {
			value.Directory = "/var/lib/opsd/databases/" + value.ID
		}
		value.Connection.Address = net.JoinHostPort("127.0.0.1", strconv.Itoa(value.SQLPort))
		if value.Kind == "proxysql" {
			value.Connection.Address = net.JoinHostPort("127.0.0.1", strconv.Itoa(value.AdminPort))
		}
		value.Bootstrap = input.Mode == "create" && index == 0
		value.ClusterID = c.ClusterID
		if value.Kind == "galera" && !value.Bootstrap && value.ClusterID == "" {
			value.ClusterID = "pending"
		}
		ref := DatabaseRef{NodeID: target.NodeID, InstanceID: "deployment_" + value.ID, Name: member.Name, Address: net.JoinHostPort(value.Address, strconv.Itoa(value.SQLPort))}
		plan.Targets = append(plan.Targets, databaseDeploymentTarget{Ref: ref, Deployment: value})
		if value.Kind == "galera" {
			seeds = append(seeds, net.JoinHostPort(value.Address, strconv.Itoa(value.ReplicationPort)))
		}
	}
	for index := range plan.Targets {
		target := &plan.Targets[index]
		target.Deployment.Seeds = seeds
		if target.Deployment.Kind == "galera" {
			target.Deployment.ReachableSeeds = append([]string{}, seeds[:len(existing)+index]...)
		} else {
			for _, backend := range target.Deployment.Backends {
				target.Deployment.ReachableSeeds = append(target.Deployment.ReachableSeeds, net.JoinHostPort(backend.Host, strconv.Itoa(backend.Port)))
			}
		}
		var env protocol.DatabaseDeploymentEnvironment
		err := n.Call(ctx, target.Ref.NodeID, "database.deployment.check", DatabaseRPC{Action: protocol.DatabaseAction{Deployment: &target.Deployment}}, &env)
		if err != nil {
			env.Problems = append(env.Problems, err.Error())
		}
		if dataBytes > 0 && env.FreeBytes < dataBytes*2+512*1024*1024 {
			env.Problems = append(env.Problems, "可用空间不足以保存已有数据与同步临时文件")
		}
		target.Fingerprint = env.Fingerprint
		ports := []int{target.Deployment.SQLPort}
		if target.Deployment.Kind == "galera" {
			ports = append(ports, target.Deployment.ReplicationPort, target.Deployment.ISTPort, target.Deployment.SSTPort)
		} else {
			ports = append(ports, target.Deployment.AdminPort)
		}
		out.Targets = append(out.Targets, DatabaseDeploymentTargetPreview{NodeID: target.Ref.NodeID, Name: target.Ref.Name, Address: target.Deployment.Address, Directory: target.Deployment.Directory, Image: target.Deployment.Image, DockerState: env.DockerState, FreeBytes: env.FreeBytes, Ports: ports, Problems: env.Problems})
		for _, problem := range env.Problems {
			out.Problems = append(out.Problems, target.Ref.Name+"："+problem)
		}
	}
	plan.Cluster = c
	plan.Network, err = n.previewDeploymentNetwork(ctx, append(existing, plan.Targets...))
	if err != nil {
		out.Problems = append(out.Problems, err.Error())
	}
	out.Network = plan.Network
	return n.sealDeploymentPreview(plan, out)
}

func (n *Node) sealDeploymentPreview(plan DatabaseDeploymentPlan, out DatabaseDeploymentPreview) (DatabaseDeploymentPreview, error) {
	out.ID, out.ClusterID, out.ExpiresAt = plan.ID, plan.Cluster.ID, plan.ExpiresAt
	plain, _ := json.Marshal(plan)
	sum := sha256.Sum256(plain)
	out.Revision = hex.EncodeToString(sum[:])
	if len(out.Problems) > 0 {
		return out, nil
	}
	secret, err := seal(plain, []string{n.age.Recipient().String()})
	if err != nil {
		return out, err
	}
	b, _ := json.Marshal(secret)
	out.Token = base64.RawURLEncoding.EncodeToString(b)
	return out, nil
}
func (n *Node) SubmitDatabaseDeployment(ctx context.Context, input DatabaseDeploymentCommit) (Operation, error) {
	var out Operation
	err := n.Call(ctx, input.Coordinator, "database.deployment.submit", databaseDeploymentRequest{Commit: input}, &out)
	return out, err
}
func (n *Node) submitDatabaseDeployment(ctx context.Context, input DatabaseDeploymentCommit) (Operation, error) {
	if input.Key == "" || len(input.Key) > 128 || len(input.Token) > 4*1024*1024 {
		return Operation{}, fault("invalid_request", "部署请求无效")
	}
	prior, err := n.RequestReceipt(ctx, n.ID, requeststate.Write{Actor: input.Actor, Key: input.Key, Digest: input.Digest})
	if err == nil && prior.Operation != nil {
		return *prior.Operation, nil
	}
	if err != nil && !missingRequest(err) {
		return Operation{}, err
	}
	b, err := base64.RawURLEncoding.DecodeString(input.Token)
	if err != nil {
		return Operation{}, fault("invalid_request", "部署预览无效")
	}
	var secret Secret
	if json.Unmarshal(b, &secret) != nil {
		return Operation{}, fault("invalid_request", "部署预览无效")
	}
	plain, err := unseal(secret, n.age)
	if err != nil {
		return Operation{}, fault("invalid_request", "部署预览已失效")
	}
	var plan DatabaseDeploymentPlan
	if json.Unmarshal(plain, &plan) != nil {
		return Operation{}, fault("invalid_request", "部署预览无效")
	}
	if input.ExpectedCluster != "" && input.ExpectedCluster != plan.Cluster.ID || input.ExpectedMode == "create_or_import" && plan.Mode != "create" && plan.Mode != "import" || input.ExpectedMode != "create_or_import" && input.ExpectedMode != plan.Mode {
		return Operation{}, fault("invalid_request", "部署预览与操作入口不同")
	}
	sum := sha256.Sum256(plain)
	if hex.EncodeToString(sum[:]) != input.Revision || plan.Actor != input.Actor || n.Now().After(plan.ExpiresAt) {
		return Operation{}, fault("conflict", "部署预览已变化或过期，请重新预览")
	}
	kind := map[string]string{"create": "create_cluster", "members": "add_members", "proxy": "add_proxy", "import": "import_cluster"}[plan.Mode]
	submission := DatabaseSubmission{Cluster: plan.Cluster, Change: DatabaseChange{Kind: kind, Cluster: plan.Cluster.ID, ExpectedRevision: plan.Cluster.Revision, Deployment: &plan}}
	if err = validateDatabaseSubmission(submission); err != nil {
		return Operation{}, err
	}
	body, _ := json.Marshal(submission)
	return n.Submit(ctx, n.ID, Request{Actor: input.Actor, Kind: "database", Key: input.Key, InputDigest: input.Digest, Payload: body})
}
func validateDeploymentSubmission(input DatabaseSubmission) error {
	p := input.Change.Deployment
	if p == nil || p.Cluster.ID != input.Cluster.ID || p.Cluster.Coordinator != input.Cluster.Coordinator || input.Change.ExpectedRevision != input.Cluster.Revision || len(p.Targets)+len(p.Imports) == 0 {
		return fault("invalid_request", "数据库部署计划无效")
	}
	for _, target := range p.Targets {
		if err := target.Deployment.Validate(); err != nil {
			return fault("invalid_request", err.Error())
		}
	}
	return nil
}

func (n *Node) deploymentDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	var request databaseDeploymentRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	if method == "database.deployment.preview" {
		return n.previewDatabaseDeployment(ctx, request.Input, request.Actor)
	}
	if method == "database.deployment.submit" {
		return n.submitDatabaseDeployment(ctx, request.Commit)
	}
	return nil, fault("invalid_request", "未知部署请求")
}

func deploymentStepID(owner, id string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", id, index)))
	return owner + "_" + hex.EncodeToString(sum[:])
}

func (n *Node) buildDeploymentWorkflow(ctx context.Context, id string, input DatabaseSubmission) (databaseWorkflow, error) {
	p := input.Change.Deployment
	flow := databaseWorkflow{Steps: []databaseWorkflowStep{}, Cluster: &p.Cluster}
	for _, target := range p.Targets {
		var env protocol.DatabaseDeploymentEnvironment
		if err := n.Call(ctx, target.Ref.NodeID, "database.deployment.check", DatabaseRPC{Action: protocol.DatabaseAction{Deployment: &target.Deployment}}, &env); err != nil {
			return flow, err
		}
		if len(env.Problems) > 0 || env.Fingerprint != target.Fingerprint {
			return flow, fault("conflict", target.Ref.Name+"：环境已变化，请核对端口、目录及 Docker")
		}
	}
	for _, network := range p.Network {
		if len(network.Write.Changes) == 0 {
			continue
		}
		write := network.Write
		write.Actor = p.Actor
		write.Key = deploymentStepID(network.NodeID, id, len(flow.Steps))
		sum := sha256.Sum256([]byte(p.Actor + "\x00" + write.Key))
		flow.Steps = append(flow.Steps, databaseWorkflowStep{Ref: DatabaseRef{NodeID: network.NodeID, Name: network.NodeID}, ID: network.NodeID + "_" + hex.EncodeToString(sum[:]), State: "pending", Firewall: &write})
	}
	if p.Mode == "proxy" {
		var source DatabaseRef
		for _, ref := range p.Cluster.Members {
			if ref.NodeID == n.ID {
				source = ref
				break
			}
		}
		flow.Steps = append(flow.Steps, databaseWorkflowStep{Ref: source, ID: deploymentStepID(source.NodeID, id, len(flow.Steps)), State: "pending", Action: protocol.DatabaseAction{Kind: "create_user", ClusterID: p.Cluster.ClusterID, User: p.MonitorUser, Host: "%", PasswordHash: databaseengine.NativePassword(p.MonitorPassword)}})
	}
	for _, target := range p.Targets {
		d := target.Deployment
		flow.Steps = append(flow.Steps, databaseWorkflowStep{Ref: target.Ref, ID: deploymentStepID(target.Ref.NodeID, id, len(flow.Steps)), State: "pending", Action: protocol.DatabaseAction{Kind: "deploy_database", Connection: d.Connection, Deployment: &d}})
	}
	for _, item := range p.Imports {
		copy := item
		flow.Steps = append(flow.Steps, databaseWorkflowStep{Ref: DatabaseRef{NodeID: item.NodeID, InstanceID: item.ID, Name: item.Name, Address: item.Address}, ID: deploymentStepID(item.NodeID, id, len(flow.Steps)), State: "pending", Import: &copy})
	}
	flow.Built = true
	return flow, nil
}

func (n *Node) finishDeploymentStep(ctx context.Context, step *databaseWorkflowStep, flow *databaseWorkflow) error {
	var result DatabaseScanItem
	if err := n.Call(ctx, step.Ref.NodeID, "database.deployment.register", DatabaseRPC{InstanceID: step.Ref.InstanceID, OperationID: step.ID, Action: step.Action, Instance: DatabaseInstanceInput{DatabaseInstance: DatabaseInstance{Name: step.Ref.Name}}}, &result); err != nil {
		return err
	}
	step.Ref.InstanceRevision = result.Instance.Revision
	c := flow.Cluster
	if result.Instance.Kind == "galera" {
		for _, previous := range c.Members {
			if previous.NodeID == step.Ref.NodeID {
				continue
			}
			var profile protocol.DatabaseDeploymentProfile
			if err := n.Call(ctx, previous.NodeID, "database.deployment.profile", DatabaseRPC{InstanceID: previous.InstanceID}, &profile); err != nil {
				return err
			}
			d := step.Action.Deployment
			check := protocol.DatabaseNetworkCheck{LocalAddress: profile.Deployment.Address, Endpoints: []protocol.ProxyEndpoint{{Host: d.Address, Port: d.ReplicationPort}}}
			if err := n.Call(ctx, previous.NodeID, "database.deployment.connectivity", DatabaseRPC{NetworkCheck: &check}, nil); err != nil {
				return err
			}
		}
		if c.ClusterID == "" {
			c.ClusterID = result.Status.ClusterID
		}
		if result.Status.ClusterID != c.ClusterID {
			return fault("conflict", "新成员实际集群身份不同")
		}
		if !slices.ContainsFunc(c.Members, func(r DatabaseRef) bool { return r.NodeID == step.Ref.NodeID && r.InstanceID == step.Ref.InstanceID }) {
			c.Members = append(c.Members, step.Ref)
		}
		for i := range flow.Steps {
			d := flow.Steps[i].Action.Deployment
			if d != nil && d.Kind == "galera" && d.ClusterID == "pending" {
				d.ClusterID = c.ClusterID
			}
		}
	} else if !slices.ContainsFunc(c.Proxies, func(r DatabaseRef) bool { return r.NodeID == step.Ref.NodeID && r.InstanceID == step.Ref.InstanceID }) {
		c.Proxies = append(c.Proxies, step.Ref)
	}
	// 已经就绪的成员立即登记，后续节点失败时仍可管理原集群。
	saved, err := n.saveDeploymentCluster(ctx, *c)
	if err != nil {
		return err
	}
	*c = saved
	return nil
}

func (n *Node) finishDeploymentWorkflow(ctx context.Context, op *Operation, flow *databaseWorkflow) error {
	c := flow.Cluster
	if c.ProxyRouting != nil && c.ProxyRouting.Enabled && len(c.Proxies) > 0 && !flow.RoutingBuilt {
		preview, err := n.PreviewProxyRouting(ctx, *c, ProxyRoutingPreviewInput{ExpectedRevision: c.Revision, Enabled: true, Automatic: c.ProxyRouting.Automatic})
		if err != nil {
			return err
		}
		for _, target := range preview.Targets {
			ref := DatabaseRef{NodeID: target.NodeID, InstanceID: target.InstanceID, InstanceRevision: target.InstanceRevision, Name: target.NodeID}
			a := target.Action
			flow.Steps = append(flow.Steps, databaseWorkflowStep{Ref: ref, ID: deploymentStepID(ref.NodeID, op.ID, len(flow.Steps)), State: "pending", Action: protocol.DatabaseAction{Kind: "proxy_route", Routing: &a}})
		}
		flow.RoutingBuilt = true
		flow.Routing = &preview.Policy
		return nil
	}
	if flow.Index < len(flow.Steps) {
		return nil
	}
	if flow.Routing != nil {
		if err := n.finishDeploymentRouting(ctx, *c, *flow.Routing); err != nil {
			return err
		}
	}
	return nil
}

// 同一预览确认后生成的子操作使用原始请求键；未知应答先查询。
func (n *Node) advanceDeploymentFirewall(ctx context.Context, op *Operation, flow *databaseWorkflow) {
	step := &flow.Steps[flow.Index]
	attention := func(err error) {
		op.State = protocol.AttentionRequired
		op.Error = &protocol.Error{Code: "dependency_unavailable", Message: err.Error()}
		flow.Request = ""
		_ = n.persistDatabaseWorkflow(ctx, op, *flow)
	}
	if step.State == "pending" {
		step.State = "intent"
		if n.persistDatabaseWorkflow(ctx, op, *flow) != nil {
			return
		}
		var saved FirewallSaved
		if err := n.Call(ctx, step.Ref.NodeID, "firewall.config.write", *step.Firewall, &saved); err != nil {
			attention(err)
			return
		}
		step.ID = saved.OperationID
		step.State = "applying"
		_ = n.persistDatabaseWorkflow(ctx, op, *flow)
		return
	}
	// 协调节点上的子操作由当前父操作推进，保持目标队列中其余修改的顺序。
	if step.Ref.NodeID == n.ID {
		n.advanceFiltered("id=?", step.ID)
	}
	var child Operation
	if err := n.Call(ctx, step.Ref.NodeID, "operation.get", step.ID, &child); err != nil {
		if flow.Request == "recover" {
			step.State = "pending"
			flow.Request = ""
			op.State = protocol.Applying
			op.Error = nil
			_ = n.persistDatabaseWorkflow(ctx, op, *flow)
			return
		}
		attention(err)
		return
	}
	step.Children = child.Steps
	if child.State == protocol.AwaitingConfirmation {
		var updated Operation
		if err := n.Call(ctx, step.Ref.NodeID, "operation.confirm", OperationDecision{ID: child.ID, ExpectedRevision: child.Revision}, &updated); err != nil {
			attention(err)
			return
		}
	} else if child.State == protocol.Succeeded {
		step.State = "completed"
		flow.Index++
		flow.Request = ""
		op.State = protocol.Applying
		op.Error = nil
	} else if child.State == protocol.AttentionRequired || child.State == protocol.Rejected || child.State == protocol.Reverted || child.State == protocol.Failed {
		if flow.Request == "recover" || flow.Request == "resolve" {
			var updated Operation
			method := "operation." + flow.Request
			if err := n.Call(ctx, step.Ref.NodeID, method, OperationDecision{ID: child.ID, ExpectedRevision: child.Revision, AcceptCurrent: true}, &updated); err != nil {
				attention(err)
				return
			}
			flow.Request = ""
			op.State = protocol.Applying
		} else {
			message := "防火墙变更需要处理"
			if child.Error != nil {
				message = child.Error.Message
			}
			attention(fmt.Errorf("%s", message))
			return
		}
	} else if child.State == protocol.ResolvedManually {
		op.State = protocol.ResolvedManually
		op.Error = nil
	}
	_ = n.persistDatabaseWorkflow(ctx, op, *flow)
}
