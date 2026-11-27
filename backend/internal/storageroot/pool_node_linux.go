package storageroot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"golang.org/x/sys/unix"
)

type poolNodeRecord struct {
	SpecHash              string
	PoolID, OperationID   string
	Member                protocol.PoolNodePlan
	Members               []protocol.PoolNodePlan
	Control               protocol.StorageRoot
	Data                  protocol.StorageRoot
	ControlUUID, DataUUID string
	Phase                 string
	APIBoot               bool
	TLSGeneration         string
}

func (m *Manager) poolDir(id string) string { return filepath.Join(m.ExecutorDir, "pools", id) }

func (m *Manager) readPoolNode(id string) (poolNodeRecord, error) {
	var d poolNodeRecord
	dir := m.poolDir(id)
	if err := trustedDir(dir); err != nil {
		return d, err
	}
	if err := localstate.Read(filepath.Join(dir, "node.json"), &d); err != nil {
		return d, err
	}
	if d.PoolID != id || !operations.ValidID(d.OperationID) || d.ControlUUID == "" || d.DataUUID == "" {
		return d, fault("conflict", "本机存储池部署记录无效")
	}
	return d, nil
}

func validatePoolDeployment(d *protocol.PoolDeploy, id string) error {
	if d == nil || d.PoolID != id || !operations.ValidID(d.OperationID) || len(d.Members) < 3 || len(d.Members) > 64 {
		return fault("invalid_request", "本机存储池规格不完整")
	}
	memberInputs := make([]protocol.PoolMemberInput, 0, len(d.Members))
	controlDomains := map[string]bool{}
	matches := 0
	for _, n := range d.Members {
		input := n.PoolMemberInput
		memberInputs = append(memberInputs, input)
		offset := n.Ports.S3 - 8333
		if offset < 0 || offset > 16000 || n.Ports != storagepool.Ports(offset) || n.Ports != d.Member.Ports ||
			n.CapacityGiB < protocol.PoolMinCapacityGiB || n.CapacityGiB > (1<<33)-1 || n.FailureDomain == "" ||
			n.ControlDisk.UUID == "" || n.DataDisk.UUID == "" {
			return fault("invalid_request", "存储池成员端口、容量或磁盘身份无效")
		}
		expected := []string{"volume", "filer", "s3"}
		if n.Control() {
			if controlDomains[n.FailureDomain] {
				return fault("invalid_request", "控制成员位于同一故障域")
			}
			controlDomains[n.FailureDomain] = true
			expected = append([]string{"master", "etcd"}, expected...)
		}
		data, control := storagepool.Quotas(input.CapacityGiB*protocol.PoolGiB, n.Control())
		if !slices.Equal(n.Roles, expected) || data < protocol.PoolMinDataBytes || n.DataBytes != data || n.ControlBytes != control {
			return fault("invalid_request", "存储池角色和配额不匹配")
		}
		if n.NodeID == d.Member.NodeID && n.PoolMemberInput == d.Member.PoolMemberInput && n.DataBytes == d.Member.DataBytes &&
			n.ControlBytes == d.Member.ControlBytes && n.ControlDisk == d.Member.ControlDisk && n.DataDisk == d.Member.DataDisk &&
			slices.Equal(n.Roles, d.Member.Roles) {
			matches++
		}
	}
	if _, err := storagepool.Normalize(protocol.PoolInput{Name: id[:min(64, len(id))], Nodes: memberInputs}); err != nil {
		return err
	}
	if matches != 1 || len(controlDomains) != 3 {
		return fault("invalid_request", "存储池固定成员或控制面不完整")
	}
	return nil
}

func (m *Manager) PoolHost(ctx context.Context, req protocol.PoolHostRequest) (protocol.PoolHostResult, error) {
	if req.Action == "inspect" {
		if req.PortOffset < 0 || req.PortOffset > 16000 {
			return protocol.PoolHostResult{}, fault("invalid_request", "端口偏移无效")
		}
		return m.inspectPool(ctx, req.Member, req.PortOffset)
	}
	if !operations.ValidID(req.PoolID) {
		return protocol.PoolHostResult{}, fault("invalid_request", "存储池标识无效")
	}
	d, err := m.readPoolNode(req.PoolID)
	switch req.Action {
	case "observe":
		if errors.Is(err, os.ErrNotExist) {
			return protocol.PoolHostResult{Phase: "absent"}, nil
		}
		if err != nil {
			return protocol.PoolHostResult{}, err
		}
		return m.observePool(ctx, d), nil
	case "prepare":
		if err = validatePoolDeployment(req.Deploy, req.PoolID); err != nil {
			return protocol.PoolHostResult{}, err
		}
		if err = storagepool.ValidateCredentials(req.Deploy.Credentials); err != nil {
			return protocol.PoolHostResult{}, err
		}
	case "control", "serve", "rotate", "repair":
		if err != nil {
			return protocol.PoolHostResult{}, err
		}
	default:
		return protocol.PoolHostResult{}, fault("invalid_request", "未知存储池步骤")
	}
	unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return protocol.PoolHostResult{}, fault("node_busy", "%v", err)
	}
	defer unlock()
	unlock, err = operations.LockDocker(m.ExecutorDir)
	if err != nil {
		return protocol.PoolHostResult{}, fault("node_busy", "%v", err)
	}
	defer unlock()
	d, err = m.readPoolNode(req.PoolID)
	if req.Action == "prepare" {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return protocol.PoolHostResult{}, err
		}
		d, err = m.preparePoolNode(ctx, d, *req.Deploy)
	} else if err == nil && (req.Deploy == nil || req.Deploy.PoolID != d.PoolID || req.Deploy.OperationID != d.OperationID) {
		err = fault("conflict", "本机部署不属于此建池操作")
	} else if err == nil {
		switch req.Action {
		case "control":
			err = m.enablePoolControl(ctx, &d)
		case "serve":
			err = m.servePoolNode(ctx, &d)
		case "rotate", "repair":
			var release func()
			ctx, release, err = m.lockPoolMaintenance(ctx, d, d.Member.NodeID+"/"+req.Action)
			if err == nil {
				defer release()
				if req.Action == "rotate" {
					err = m.rotatePoolNode(ctx, d, req.Deploy.Material)
					if err == nil {
						d, err = m.readPoolNode(req.PoolID)
					}
				} else {
					err = m.repairPoolNode(ctx, d)
				}
			}
		}
	}
	if err != nil {
		return protocol.PoolHostResult{}, err
	}
	return m.observePool(ctx, d), nil
}

func poolReserveCredit(out *protocol.PoolDisk, dir, name string, limit int64) {
	for _, path := range []string{filepath.Join(dir, name+".img"), filepath.Join(dir, "."+name+".allocating")} {
		var st unix.Stat_t
		if unix.Lstat(path, &st) == nil && st.Uid == 0 && st.Gid == 0 && st.Nlink == 1 && st.Mode&(unix.S_IFMT|0777) == unix.S_IFREG|0600 {
			out.AvailableBytes += min(st.Blocks*512, limit)
			return
		}
	}
}

func (m *Manager) preparePoolNode(ctx context.Context, d poolNodeRecord, spec protocol.PoolDeploy) (poolNodeRecord, error) {
	n := spec.Member
	body, err := json.Marshal(spec)
	if err != nil {
		return d, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	if err := poolLocalIP(n.PrivateIP); err != nil {
		return d, err
	}
	if _, _, err := storagepool.VerifyMaterial(spec.Material, n.PrivateIP, time.Now()); err != nil {
		return d, err
	}
	if err := storagepool.VerifyEtcdPeer(spec.Material, n.NodeID, n.PrivateIP, time.Now()); err != nil {
		return d, err
	}
	if key, err := hex.DecodeString(spec.Material.FilerKey); err != nil || len(key) != 32 || n.Control() != (spec.Material.CASeal != "") {
		return d, fault("invalid_request", "存储池节点密钥材料无效")
	}
	if d.OperationID != "" {
		if d.OperationID != spec.OperationID || d.SpecHash != digest {
			return d, fault("conflict", "存储池已经由其它操作或参数登记")
		}
		if d.Phase != "allocating" {
			return d, nil
		}
	}
	r, a, b, err := m.poolRoots(ctx, n.StorageRootID)
	if err != nil {
		return d, err
	}
	if a.UUID != n.ControlDisk.UUID || b.UUID != n.DataDisk.UUID || a.MountPoint != n.ControlDisk.MountPoint || b.MountPoint != n.DataDisk.MountPoint {
		return d, fault("precondition_failed", "A 或 B 文件系统身份已变化")
	}
	unlock, err := lockPoolDisks(a.UUID, b.UUID)
	if err != nil {
		return d, fault("node_busy", "%v", err)
	}
	defer unlock()
	if err = m.checkRoot(ctx, a); err != nil {
		return d, err
	}
	if err = m.checkRoot(ctx, b); err != nil {
		return d, err
	}
	controlDir, dataDir := m.poolDir(spec.PoolID), filepath.Join(b.Base, "pools", spec.PoolID)
	ad, bd, err := m.poolDisks(ctx, r, a, b)
	if err != nil {
		return d, err
	}
	if d.OperationID != "" {
		initialA, initialB := ad.AvailableBytes, bd.AvailableBytes
		poolReserveCredit(&ad, controlDir, "control", n.ControlBytes)
		poolReserveCredit(&bd, dataDir, "data", n.DataBytes)
		if ad.UUID == bd.UUID {
			total := min(initialA, initialB) + (ad.AvailableBytes - initialA) + (bd.AvailableBytes - initialB)
			ad.AvailableBytes, bd.AvailableBytes = total, total
		}
	}
	if !storagepool.Enough(ad, bd, n.ControlBytes, n.DataBytes) {
		return d, storagepool.Fault("insufficient_storage", storagepool.CapacityMessage(n.NodeID, ad, bd, n.ControlBytes, n.DataBytes, n.Control()))
	}
	for _, b := range r.Bindings {
		if b.ComponentID != spec.PoolID && overlaps(b.Directory, dataDir) {
			return d, fault("conflict", "数据镜像目录已被受管组件占用")
		}
	}
	for _, path := range []string{controlDir, dataDir} {
		if err := trustedDir(path); err != nil {
			return d, err
		}
		if d.OperationID == "" {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return d, fault("conflict", "存储池目录已被占用：%s", path)
			}
		}
	}
	if d.OperationID == "" {
		reserved, err := m.reservedPoolPorts(n.PrivateIP)
		if err != nil {
			return d, err
		}
		dataPorts, controlPorts := storagepool.PortGroups(n.Ports)
		if !portGroupFree(ctx, n.PrivateIP, dataPorts, reserved) || n.Control() && !portGroupFree(ctx, n.PrivateIP, controlPorts, reserved) {
			return d, fault("conflict", "建池时私网端口已占用")
		}
		if _, err = m.Docker.AuditStorage(ctx, []string{controlDir, dataDir}, nil, true); err != nil {
			return d, fault("conflict", "%v", err)
		}
		d = poolNodeRecord{PoolID: spec.PoolID, OperationID: spec.OperationID, SpecHash: digest, Member: n, Members: spec.Members, Control: a, Data: b,
			ControlUUID: poolImageUUID(spec.PoolID, n.NodeID, "control"), DataUUID: poolImageUUID(spec.PoolID, n.NodeID, "data"), Phase: "allocating"}
		if err = m.savePoolNode(d); err != nil {
			return d, err
		}
	}
	if err = m.ensurePoolImages(ctx, n.Control()); err != nil {
		return d, err
	}
	for _, image := range []struct {
		dir, name, uuid string
		size            int64
	}{
		{controlDir, "control", d.ControlUUID, n.ControlBytes}, {dataDir, "data", d.DataUUID, n.DataBytes},
	} {
		if err = createPoolImage(ctx, image.dir, image.name, image.uuid, image.size); err != nil {
			return d, err
		}
		if err = os.MkdirAll(filepath.Join(image.dir, image.name), 0700); err != nil {
			return d, err
		}
	}
	config := filepath.Join(controlDir, "runtime")
	if err = writePoolConfig(config, d, spec.Material, spec.Credentials); err != nil {
		return d, err
	}
	p := poolDeployment{ID: spec.PoolID, UUID: d.DataUUID, Directory: dataDir,
		Templates: []dockerengine.StoragePoolTemplate{poolVolumeTemplate(d, config)}}
	if err = localstate.Write(filepath.Join(controlDir, "storage-data.json"), p, 0600); err != nil {
		return d, err
	}
	r.setBinding(binding{StorageBinding: protocol.StorageBinding{Component: protocol.StoragePoolData, ComponentID: d.PoolID, RootID: b.ID, Directory: dataDir},
		Identity: b, Guarded: true, Instance: m.Config})
	if err = m.save(&r); err != nil {
		return d, err
	}
	d.Phase = "prepared"
	d.TLSGeneration = storagepool.TLSGeneration(spec.Material)
	return d, m.savePoolNode(d)
}

func (m *Manager) savePoolNode(d poolNodeRecord) error {
	return localstate.Write(filepath.Join(m.poolDir(d.PoolID), "node.json"), d, 0600)
}

func (m *Manager) poolControlBinding(d poolNodeRecord) binding {
	return binding{StorageBinding: protocol.StorageBinding{Component: poolControlComponent, ComponentID: d.PoolID, Directory: m.poolDir(d.PoolID)},
		Identity: d.Control, Guarded: true, Instance: m.Config}
}

func (m *Manager) checkPoolControl(ctx context.Context, d poolNodeRecord) error {
	if d.Phase == "allocating" {
		return fault("conflict", "存储池镜像尚未准备")
	}
	if err := m.checkRoot(ctx, d.Control); err != nil {
		return err
	}
	dir := m.poolDir(d.PoolID)
	return verifyPoolImage(ctx, filepath.Join(dir, "control.img"), d.ControlUUID, d.Member.ControlBytes)
}

func (m *Manager) poolControlComponent(ctx context.Context, cmd, id string) error {
	d, err := m.readPoolNode(id)
	if err != nil {
		return err
	}
	switch cmd {
	case "component-check":
		return m.checkPoolControl(ctx, d)
	case "component-start":
		return m.startPoolControl(ctx, d)
	case "component-stop":
		dir := m.poolDir(id)
		states, err := m.auditPoolControl(ctx, d, true, true)
		if err != nil {
			return err
		}
		if err = m.Docker.StopStorage(ctx, states); err != nil {
			return err
		}
		return unmountPoolImage(ctx, dir, "control", d.ControlUUID)
	}
	return fault("invalid_request", "未知存储池控制卷命令")
}

func (m *Manager) auditPoolControl(ctx context.Context, d poolNodeRecord, includeAPI, allowMissing bool) ([]dockerengine.StorageContainerState, error) {
	dir := m.poolDir(d.PoolID)
	templates := poolControlTemplates(d, filepath.Join(dir, "runtime"), includeAPI)
	if len(templates) == 0 {
		return nil, nil
	}
	specs := make([]dockerengine.StorageContainer, 0, len(templates))
	for _, t := range templates {
		spec, err := t.StorageControlSpec(d.PoolID, dir)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	states, err := m.Docker.AuditStorage(ctx, nil, specs, allowMissing)
	for _, state := range states {
		if state.Restart != "no" {
			return nil, fault("conflict", "池容器 %s 的重启策略已变化", state.Name)
		}
	}
	return states, err
}

func (m *Manager) startPoolControl(ctx context.Context, d poolNodeRecord) error {
	if err := m.checkPoolControl(ctx, d); err != nil {
		return err
	}
	dir := m.poolDir(d.PoolID)
	if err := mountPoolImage(ctx, dir, "control", d.ControlUUID); err != nil {
		return err
	}
	config := filepath.Join(dir, "runtime")
	templates := poolControlTemplates(d, config, d.APIBoot)
	for _, t := range templates {
		name := t.Host.Mounts[0].Source
		if err := makePoolRoleDir(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	if len(templates) == 0 {
		return nil
	}
	if err := m.Docker.StageStoragePoolControl(ctx, d.PoolID, dir, templates); err != nil {
		return err
	}
	states, err := m.auditPoolControl(ctx, d, d.APIBoot, false)
	if err != nil {
		return err
	}
	var api, controllers []dockerengine.StorageContainerState
	filerStarting := false
	for _, state := range states {
		if strings.HasSuffix(state.Name, "-filer") || strings.HasSuffix(state.Name, "-s3") {
			api = append(api, state)
			filerStarting = filerStarting || strings.HasSuffix(state.Name, "-filer") && !state.Running
		} else {
			controllers = append(controllers, state)
		}
	}
	if err = m.Docker.StartStorage(ctx, controllers, nil); err != nil {
		return err
	}
	if filerStarting {
		if err = preparePoolFiler(ctx, d, config, ""); err != nil {
			return err
		}
	}
	return m.Docker.StartStorage(ctx, api, nil)
}

func (m *Manager) enablePoolControl(ctx context.Context, d *poolNodeRecord) error {
	if d.Phase == "control" || d.Phase == "serving" {
		return nil
	}
	if d.Phase != "prepared" {
		return fault("conflict", "控制组件需要先完成镜像预分配")
	}
	bind := m.poolControlBinding(*d)
	if err := drain(ctx, drainBudget); err != nil {
		return err
	}
	if err := m.installUnit(ctx, bind); err != nil {
		return err
	}
	if err := changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
		return err
	}
	if err := changeSystemd(ctx, startBudget, "start", unitName(bind)); err != nil {
		return err
	}
	d.Phase = "control"
	return m.savePoolNode(*d)
}

func (m *Manager) servePoolNode(ctx context.Context, d *poolNodeRecord) error {
	if d.Phase == "serving" {
		return nil
	}
	if d.Phase != "control" {
		return fault("conflict", "服务组件需要控制组件先启动")
	}
	r, err := m.paths(ctx)
	if err != nil {
		return err
	}
	b, found := r.lookup(protocol.StoragePoolData, d.PoolID)
	if !found {
		return fault("conflict", "存储池缺少数据绑定")
	}
	if err = m.validateBinding(ctx, b, false); err != nil {
		return err
	}
	if pending, err := m.pending(r); err != nil {
		return err
	} else if pending != nil && pending.From.ComponentID == d.PoolID {
		return fault("node_busy", "池数据存在待处理的迁移")
	}
	p, err := m.pool(d.PoolID)
	if err != nil {
		return err
	}
	if err = p.Pool.mount(ctx, b.Directory); err != nil {
		return err
	}
	if err = makePoolRoleDir(filepath.Join(b.Directory, "data", "volume")); err != nil {
		return err
	}
	if err = m.stage(ctx, p, b); err != nil {
		return err
	}
	if err = drain(ctx, drainBudget); err != nil {
		return err
	}
	if err = m.installUnit(ctx, b); err != nil {
		return err
	}
	if err = changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
		return err
	}
	if err = changeSystemd(ctx, startBudget, "start", unitName(b)); err != nil {
		return err
	}
	d.APIBoot = true
	if err = m.savePoolNode(*d); err != nil {
		return err
	}
	if err = m.startPoolControl(ctx, *d); err != nil {
		return err
	}
	d.Phase = "serving"
	return m.savePoolNode(*d)
}

func poolRotationValid(d poolNodeRecord, material protocol.PoolMaterial, config string) (string, error) {
	if d.Phase != "serving" {
		return "", fault("conflict", "存储池节点尚未完成首次部署")
	}
	if _, _, err := storagepool.VerifyMaterial(material, d.Member.PrivateIP, time.Now()); err != nil {
		return "", err
	}
	if err := storagepool.VerifyEtcdPeer(material, d.Member.NodeID, d.Member.PrivateIP, time.Now()); err != nil {
		return "", err
	}
	old, err := os.Readlink(filepath.Join(config, "active"))
	if err != nil || !strings.HasPrefix(old, "tls-") || filepath.Base(old) != old {
		return "", fault("conflict", "当前证书代次无效")
	}
	return old, nil
}

func poolError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
