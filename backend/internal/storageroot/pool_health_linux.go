package storageroot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"golang.org/x/sys/unix"
)

func poolMountUsage(ctx context.Context, dir, role, uuid string) (int64, int64, error) {
	dev, err := activePoolLoop(ctx, dir, role, uuid)
	if err != nil || dev == "" {
		return 0, 0, fault("dependency_unavailable", "池 %s 镜像未挂载：%s %v", role, dir, err)
	}
	var stat unix.Statfs_t
	if err = unix.Statfs(filepath.Join(dir, role), &stat); err != nil {
		return 0, 0, err
	}
	block := int64(stat.Frsize)
	return int64(stat.Blocks-stat.Bfree) * block, int64(stat.Bavail) * block, nil
}

func poolReadJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(request)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("组件返回 HTTP %d", res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

func poolMemberHTTP(d poolNodeRecord, config string, timeout time.Duration) (*http.Client, time.Time, error) {
	material, err := readPoolMaterial(config)
	if err != nil {
		return nil, time.Time{}, err
	}
	tlsConfig, expiry, err := storagepool.VerifyMaterial(material, d.Member.PrivateIP, time.Now())
	if err != nil {
		return nil, time.Time{}, err
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, expiry, nil
}

func poolEtcdHealthy(ctx context.Context, httpClient *http.Client, base string) bool {
	var result struct{ Health string }
	return poolReadJSON(ctx, httpClient, base+"/health?serializable=false", &result) == nil && result.Health == "true"
}

func preparePoolFiler(ctx context.Context, d poolNodeRecord, config, restarting string) error {
	httpClient, _, err := poolMemberHTTP(d, config, time.Second)
	if err != nil {
		return err
	}
	defer httpClient.CloseIdleConnections()
	_, clients, _ := poolMasters(d.Members)
	addresses := strings.Split(clients, ",")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	// SeaweedFS 4.55 启动只核对首个 etcd 端点，随后仍使用全部端点。
	for {
		for i, addr := range addresses {
			if addr == restarting || !poolEtcdHealthy(ctx, httpClient, addr) {
				continue
			}
			ordered := append([]string{addr}, addresses[:i]...)
			ordered = append(ordered, addresses[i+1:]...)
			return writePoolFilerConfig(config, d.PoolID, strings.Join(ordered, ","))
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fault("dependency_unavailable", "Filer 启动前未找到可读的 etcd 成员")
		}
	}
}

func poolFilerReady(ctx context.Context, client *http.Client, member protocol.PoolNodePlan) error {
	return poolReadJSON(ctx, client, "https://"+storagepool.Address(member.PrivateIP, member.Ports.Filer)+"/readyz", nil)
}

func (m *Manager) observePool(ctx context.Context, d poolNodeRecord) protocol.PoolHostResult {
	n := d.Member
	status := protocol.PoolMemberStatus{PoolNodePlan: n, Endpoint: "https://" + storagepool.Address(n.PrivateIP, n.Ports.S3),
		DataUUID: d.DataUUID, ControlUUID: d.ControlUUID, CheckedAt: time.Now().UTC(), State: "deploying"}
	out := protocol.PoolHostResult{Phase: d.Phase, TLSGeneration: d.TLSGeneration, Status: status}
	if d.Phase == "allocating" || d.Phase == "prepared" {
		return out
	}
	s := &out.Status
	if err := m.checkRoot(ctx, d.Control); err != nil {
		s.State, s.Error = "unavailable", err.Error()
		return out
	}
	dir := m.poolDir(d.PoolID)
	config := filepath.Join(dir, "runtime")
	httpClient, expiry, err := poolMemberHTTP(d, config, 2*time.Second)
	if err != nil {
		s.State, s.Error = "unavailable", "池 TLS 证书校验失败"
		return out
	}
	s.CertExpiry = expiry
	defer httpClient.CloseIdleConnections()
	if n.Control() {
		var master struct{ Leader string }
		if poolReadJSON(ctx, httpClient, "https://"+storagepool.Address(n.PrivateIP, n.Ports.Master)+"/cluster/status", &master) == nil {
			s.MasterLeader = master.Leader
		}
		s.EtcdRead = poolEtcdHealthy(ctx, httpClient, "https://"+storagepool.Address(n.PrivateIP, n.Ports.EtcdClient))
	}
	s.ControlUsedBytes, _, err = poolMountUsage(ctx, dir, "control", d.ControlUUID)
	if err != nil {
		s.State, s.Error = "unavailable", err.Error()
		return out
	}
	if d.Phase != "serving" {
		return out
	}
	r, err := m.paths(ctx)
	if err != nil {
		s.State, s.Error = "unavailable", "池数据绑定不可读取"
		return out
	}
	b, ok := r.lookup(protocol.StoragePoolData, d.PoolID)
	if !ok {
		s.State, s.Error = "unavailable", "池数据绑定缺失"
		return out
	}
	s.StorageRootID, s.DataDisk.UUID, s.DataDisk.MountPoint = b.RootID, b.Identity.UUID, b.Identity.MountPoint
	if err = m.validateBinding(ctx, b, false); err != nil {
		s.State, s.Error = "unavailable", err.Error()
		return out
	}
	var free int64
	s.DataUsedBytes, free, err = poolMountUsage(ctx, b.Directory, "data", d.DataUUID)
	if err != nil {
		s.State, s.Error = "unavailable", err.Error()
		return out
	}
	if err = m.checkServingPool(ctx, d, config, b, httpClient); err != nil {
		s.State, s.Error = "degraded", err.Error()
		return out
	}
	s.S3Ready = true
	s.Writable = free >= max(128*protocol.PoolMiB, n.DataBytes/10)
	s.State = "ready"
	if !s.Writable {
		s.State, s.Error = "degraded", "数据镜像已达到写入预留"
	}
	return out
}

func (m *Manager) checkServingPool(ctx context.Context, d poolNodeRecord, config string, b binding, httpClient *http.Client) error {
	p, err := m.pool(d.PoolID)
	if err != nil {
		return err
	}
	specs, err := p.Pool.specs(b.Directory)
	if err != nil {
		return err
	}
	data, err := m.Docker.AuditStorage(ctx, nil, specs, false)
	if err != nil {
		return err
	}
	control, err := m.auditPoolControl(ctx, d, true, false)
	if err != nil {
		return err
	}
	for _, state := range append(data, control...) {
		if !state.Running {
			return fault("dependency_unavailable", "池容器 %s 已停止", state.Name)
		}
	}
	if err = poolFilerReady(ctx, httpClient, d.Member); err != nil {
		return fault("dependency_unavailable", "本机 Filer 未就绪：%v", err)
	}
	return checkPoolS3(ctx, d, config)
}

func checkPoolS3(ctx context.Context, d poolNodeRecord, config string) error {
	var auth struct {
		Identities []struct {
			Credentials []struct{ AccessKey, SecretKey string }
		}
	}
	body, err := os.ReadFile(poolS3ConfigPath(config))
	if err != nil || json.Unmarshal(body, &auth) != nil || len(auth.Identities) != 1 || len(auth.Identities[0].Credentials) != 1 {
		return fault("conflict", "S3 探测凭据不可读取")
	}
	ca, err := os.ReadFile(filepath.Join(config, "active", "ca.crt"))
	if err != nil {
		return fault("conflict", "S3 CA 不可读取")
	}
	key := auth.Identities[0].Credentials[0]
	client, err := storagepool.S3("https://"+storagepool.Address(d.Member.PrivateIP, d.Member.Ports.S3), string(ca),
		protocol.PoolCredentials{AccessKey: key.AccessKey, SecretKey: key.SecretKey})
	if err != nil {
		return err
	}
	read, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err = client.ListBuckets(read); err != nil {
		return fault("dependency_unavailable", "S3 TLS 或签名读取失败")
	}
	return nil
}

func (m *Manager) rotatePoolNode(ctx context.Context, d poolNodeRecord, material protocol.PoolMaterial) error {
	dir := m.poolDir(d.PoolID)
	config := filepath.Join(dir, "runtime")
	active, err := poolRotationValid(d, material, config)
	if err != nil {
		return err
	}
	if d.Member.Control() != (material.CASeal != "") {
		return fault("invalid_request", "存储池 CA 密封材料与控制角色不同")
	}
	wanted := storagepool.TLSGeneration(material)
	if active == wanted && d.TLSGeneration == wanted {
		return nil
	}
	// 崩溃可能发生在证书切换和容器读回之间，以原已验证代次作为恢复点。
	old := d.TLSGeneration
	if !strings.HasPrefix(old, "tls-") || filepath.Base(old) != old {
		return fault("conflict", "缺少可恢复的已验证 TLS 代次")
	}
	path, err := m.Directory(ctx, protocol.StoragePoolData, d.PoolID, true)
	if err != nil {
		return err
	}
	p, err := m.pool(d.PoolID)
	if err != nil {
		return err
	}
	specs, err := p.Pool.specs(path)
	if err != nil {
		return err
	}
	data, err := m.Docker.AuditStorage(ctx, nil, specs, false)
	if err != nil {
		return err
	}
	control, err := m.auditPoolControl(ctx, d, true, false)
	if err != nil {
		return err
	}
	restarting := ""
	if d.Member.Control() {
		restarting = "https://" + storagepool.Address(d.Member.PrivateIP, d.Member.Ports.EtcdClient)
	}
	if err = preparePoolFiler(ctx, d, config, restarting); err != nil {
		return err
	}
	if _, err = switchPoolMaterial(config, material); err != nil {
		return err
	}
	states := append(data, control...)
	err = m.restartPoolContainers(ctx, states, d, config)
	if err != nil {
		recover, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if rollback := activatePoolMaterial(config, old); rollback != nil {
			return errors.Join(err, rollback)
		}
		return errors.Join(err, m.restartPoolContainers(recover, states, d, config))
	}
	d.TLSGeneration = wanted
	return m.savePoolNode(d)
}

func (m *Manager) restartPoolContainers(ctx context.Context, states []dockerengine.StorageContainerState, d poolNodeRecord, config string) error {
	if err := m.Docker.RestartStoragePool(ctx, states); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err := checkPoolS3(ready, d, config); err == nil {
			return nil
		}
		select {
		case <-tick.C:
		case <-ready.Done():
			return fault("dependency_unavailable", "轮换后 S3 入口未就绪")
		}
	}
}
