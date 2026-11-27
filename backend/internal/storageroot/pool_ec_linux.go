package storageroot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
)

type poolECReceipt struct {
	Digest string
	At     time.Time
	Result protocol.PoolECJobResult
}

func (m *Manager) poolECJob(ctx context.Context, d poolNodeRecord, job protocol.PoolECJob) (protocol.PoolHostResult, error) {
	var out protocol.PoolHostResult
	if d.Member.ECImage == "" || d.Phase != "serving" {
		return out, fault("invalid_request", "池成员未运行固定 EC 镜像")
	}
	in := job.Input
	in.Masters = nil
	if in.Action == "scan" {
		for _, node := range d.Members {
			if node.Control() {
				in.Masters = append(in.Masters, poolECAddress(node, node.Ports.Master, node.Ports.MasterGRPC))
			}
		}
		res, err := m.runPoolEC(ctx, d, in)
		out.ECJob = &res
		return out, err
	}
	if !operations.ValidID(job.ID) || job.Attempt < 1 || in.Volume == 0 || in.Generation <= 0 ||
		!slices.Contains([]string{"seal", "prepare", "verify", "repair", "clean-replicas", "restore-replicas", "clean-shards"}, in.Action) ||
		len(in.Members) != len(d.Members) {
		return out, fault("invalid_request", "EC 步骤或原任务身份无效")
	}
	members := make(map[string]string)
	for _, node := range d.Members {
		members[poolECAddress(node, node.Ports.Volume, node.Ports.VolumeGRPC)] = node.FailureDomain
	}
	for _, node := range in.Members {
		if members[node.Address] != node.Domain || node.Domain == "" {
			return out, fault("invalid_request", "EC 计划成员与本地池定义不同")
		}
		delete(members, node.Address)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	dir := filepath.Join(m.poolDir(d.PoolID), "ec-jobs")
	if err := trustedDir(dir); err != nil {
		return out, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", job.ID, in.Action, job.Attempt))
	unlock, err := localstate.Lock(filepath.Join(dir, "lock"))
	if err != nil {
		return out, err
	}
	defer unlock()
	var receipt poolECReceipt
	err = localstate.Read(path, &receipt)
	if err == nil {
		if receipt.Digest != digest {
			return out, fault("conflict", "EC 原任务参数不同")
		}
		if receipt.Result.State == "running" && time.Since(receipt.At) > 13*time.Minute {
			receipt.Result = protocol.PoolECJobResult{State: "unknown", Message: "原步骤未收到执行结果"}
		}
		out.ECJob = &receipt.Result
		return out, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	if !job.Start {
		out.ECJob = &protocol.PoolECJobResult{State: "absent"}
		return out, nil
	}
	receipt = poolECReceipt{Digest: digest, At: time.Now(), Result: protocol.PoolECJobResult{State: "running"}}
	if err = localstate.Write(path, receipt, 0600); err != nil {
		return out, err
	}
	initial := receipt.Result
	out.ECJob = &initial
	go func() {
		work, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		result, err := m.applyPoolEC(work, d, in)
		if err != nil {
			result = protocol.PoolECJobResult{State: "failed", Code: firewall.Code(err), Message: err.Error()}
		}
		receipt.Result = result
		_ = localstate.Write(path, receipt, 0600)
	}()
	return out, nil
}

func poolECAddress(node protocol.PoolNodePlan, http, grpc int) string {
	return fmt.Sprintf("%s.%d", storagepool.Address(node.PrivateIP, http), grpc)
}

func (m *Manager) applyPoolEC(ctx context.Context, d poolNodeRecord, in protocol.PoolECWorkerInput) (protocol.PoolECJobResult, error) {
	var out protocol.PoolECJobResult
	unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return out, err
	}
	defer unlock()
	unlock, err = operations.LockDocker(m.ExecutorDir)
	if err != nil {
		return out, err
	}
	defer unlock()
	ctx, release, err := m.lockPoolMaintenance(ctx, d, fmt.Sprintf("ec/%d/%s", in.Generation, in.Action))
	if err != nil {
		return out, err
	}
	defer release()
	return m.runPoolEC(ctx, d, in)
}

func (m *Manager) runPoolEC(ctx context.Context, d poolNodeRecord, in protocol.PoolECWorkerInput) (protocol.PoolECJobResult, error) {
	var out protocol.PoolECJobResult
	r, err := m.paths(ctx)
	if err != nil {
		return out, err
	}
	b, ok := r.lookup(protocol.StoragePoolData, d.PoolID)
	if !ok {
		return out, fault("dependency_unavailable", "EC 数据卷绑定不存在")
	}
	if err = m.validateBinding(ctx, b, false); err != nil {
		return out, err
	}
	if _, _, err = poolMountUsage(ctx, b.Directory, "data", d.DataUUID); err != nil {
		return out, err
	}
	record, err := m.pool(d.PoolID)
	if err != nil {
		return out, err
	}
	specs, err := record.Pool.specs(b.Directory)
	if err != nil {
		return out, err
	}
	running, err := m.Docker.AuditStorage(ctx, nil, specs, false)
	if err != nil || len(running) != 1 || !running[0].Running {
		return out, fault("dependency_unavailable", "原 EC Volume 容器未就绪：%v", err)
	}
	id := running[0].ID
	image, err := output(ctx, "docker", "inspect", "--format", "{{.Image}}", id)
	if err != nil || image != d.Member.ECImage {
		return out, fault("conflict", "Volume 容器镜像不同于原计划")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	command := exec.CommandContext(ctx, "docker", "exec", "-i", id, "timeout", "-s", "KILL", "700s", "weed", "opsd-ec")
	command.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	result, err := command.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		msg = msg[max(0, len(msg)-4096):]
		if in.Action == "restore-replicas" && (strings.Contains(msg, "no hdd disk location with a free slot") ||
			strings.Contains(msg, "no space left on device")) {
			return out, fault("insufficient_storage", "卷 %d 恢复容量不足：%s", in.Volume, msg)
		}
		return out, fmt.Errorf("EC %s 在 %s 失败：%v %s", in.Action, d.Member.NodeID, err, msg)
	}
	if len(result) > 1<<20 || !json.Valid(result) {
		return out, fmt.Errorf("EC %s 读回应答无效", in.Action)
	}
	return protocol.PoolECJobResult{State: "done", Data: json.RawMessage(result)}, nil
}
