package erasure_coding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/seaweedfs/seaweedfs/weed/operation"
	"github.com/seaweedfs/seaweedfs/weed/pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/volume_server_pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/worker_pb"
	"google.golang.org/grpc"
)

const (
	opsdECData   = 10
	opsdECParity = 4
	opsdECTotal  = opsdECData + opsdECParity
)

type OPSDMember struct {
	Address string `json:"address"`
	Domain  string `json:"domain"`
	Shards  []int  `json:"shards,omitempty"`
}

type OPSDRequest struct {
	Action      string       `json:"action"`
	Volume      uint32       `json:"volume"`
	Collection  string       `json:"collection"`
	Generation  int64        `json:"generation"`
	SourceBytes int64        `json:"source_bytes,omitempty"`
	QuietBefore int64        `json:"quiet_before,omitempty"`
	Sources     []string     `json:"sources"`
	Members     []OPSDMember `json:"members"`
	Restore     []string     `json:"restore,omitempty"`
	Masters     []string     `json:"masters,omitempty"`
}

type opsdShard struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type opsdManifest struct {
	Request   OPSDRequest                  `json:"request"`
	BlockSize int64                        `json:"block_size"`
	Bytes     int64                        `json:"bytes"`
	Data      opsdShard                    `json:"data"`
	Index     opsdShard                    `json:"index"`
	Shards    [opsdECTotal]opsdShard       `json:"shards"`
	Samples   []opsdSample                 `json:"samples"`
	Original  map[string]opsdReplicaDigest `json:"original"`
}

var opsdCollection = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,126}$`)

func RunOPSD(ctx context.Context, r OPSDRequest, dial grpc.DialOption) (any, error) {
	if r.Action == "scan" {
		return scanOPSDVolumes(ctx, r.Masters, dial)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	action, restore := r.Action, r.Restore
	r.Action, r.Restore, r.Masters = "", nil, nil
	for i := range r.Members {
		if len(r.Members[i].Shards) == 0 {
			r.Members[i].Shards = nil
		}
	}
	dir := filepath.Join("/data/.opsd-ec", fmt.Sprintf("%d-%d", r.Volume, r.Generation))
	initial, err := readOPSDManifest(dir)
	if err == nil && !reflect.DeepEqual(initial.Request, r) {
		return nil, fmt.Errorf("EC 代际 %d 已登记其他源或目标", r.Generation)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	switch action {
	case "seal":
		return nil, sealOPSDReplicas(ctx, r, dial)
	case "prepare":
		if errors.Is(err, os.ErrNotExist) {
			initial, err = stageOPSD(ctx, r, dir, dial)
			if err != nil {
				return nil, err
			}
		}
		if err = publishOPSD(ctx, initial, dir, dial); err != nil {
			return nil, err
		}
		return initial, nil
	case "verify":
		if err != nil {
			return nil, err
		}
		return initial, verifyOPSDConversion(ctx, initial, dial)
	case "repair":
		if err != nil {
			return nil, err
		}
		return nil, repairOPSDShards(ctx, initial, dir, dial)
	case "clean-replicas", "restore-replicas", "clean-shards":
		if err != nil {
			return nil, err
		}
		return finishOPSDTransition(ctx, action, initial, restore, dir, dial)
	default:
		return nil, fmt.Errorf("未知 EC 步骤 %q", action)
	}
}

func (r OPSDRequest) validate() error {
	if r.Volume == 0 || r.Generation <= 0 || r.Collection != "" && !opsdCollection.MatchString(r.Collection) ||
		len(r.Sources) != 2 || len(r.Members) < 4 || len(r.Members) > 64 {
		return fmt.Errorf("EC 卷、代际、源或成员无效")
	}
	members := map[string]string{}
	domains := map[string]int{}
	var seen [opsdECTotal]bool
	for _, m := range r.Members {
		if m.Address == "" || m.Domain == "" || members[m.Address] != "" {
			return fmt.Errorf("EC 成员地址或故障域无效")
		}
		members[m.Address] = m.Domain
		for _, id := range m.Shards {
			if id < 0 || id >= len(seen) || seen[id] {
				return fmt.Errorf("EC 片号重复或越界：%d", id)
			}
			seen[id] = true
			domains[m.Domain]++
			if domains[m.Domain] > opsdECParity {
				return fmt.Errorf("EC 故障域 %s 超过四片", m.Domain)
			}
		}
	}
	if a, b := members[r.Sources[0]], members[r.Sources[1]]; a == "" || b == "" || a == b {
		return fmt.Errorf("EC 两个源副本须位于独立故障域")
	}
	if len(r.Restore) != 0 && (len(r.Restore) != 2 || members[r.Restore[0]] == "" ||
		members[r.Restore[1]] == "" || members[r.Restore[0]] == members[r.Restore[1]]) {
		return fmt.Errorf("EC 恢复需要两个跨故障域目标")
	}
	if slices.Contains(seen[:], false) {
		return fmt.Errorf("EC 布局缺少分片")
	}
	return nil
}

func sealOPSDReplicas(ctx context.Context, r OPSDRequest, dial grpc.DialOption) error {
	for _, src := range r.Sources {
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(src), dial, func(client volume_server_pb.VolumeServerClient) error {
			f, err := client.ReadVolumeFileStatus(ctx, &volume_server_pb.ReadVolumeFileStatusRequest{VolumeId: r.Volume})
			if err != nil {
				return err
			}
			return checkOPSDSource(r, f)
		})
		if err != nil {
			return fmt.Errorf("源 %s 封存前核对失败：%w", src, err)
		}
	}
	for _, src := range r.Sources {
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(src), dial, func(client volume_server_pb.VolumeServerClient) error {
			_, err := client.VolumeMarkReadonly(ctx, &volume_server_pb.VolumeMarkReadonlyRequest{VolumeId: r.Volume, Persist: true})
			return err
		})
		if err != nil {
			return fmt.Errorf("源 %s 封存失败：%w", src, err)
		}
	}
	return nil
}

func checkOPSDSource(r OPSDRequest, f *volume_server_pb.ReadVolumeFileStatusResponse) error {
	if f.GetCollection() != r.Collection || f.GetExpireAtSec() != 0 || f.GetDatFileSize() == 0 {
		return fmt.Errorf("源卷 %d 的集合、寿命或数据文件不符合计划", r.Volume)
	}
	if r.SourceBytes > 0 && f.GetDatFileSize() != uint64(r.SourceBytes) ||
		r.QuietBefore > 0 && f.GetDatFileTimestampSeconds() > uint64(r.QuietBefore) {
		return fmt.Errorf("源卷 %d 自计划以来有新写入或容量变化", r.Volume)
	}
	return nil
}

func stageOPSD(ctx context.Context, r OPSDRequest, dir string, dial grpc.DialOption) (opsdManifest, error) {
	out := opsdManifest{Request: r}
	for _, member := range r.Members {
		if len(member.Shards) == 0 {
			continue
		}
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(member.Address), dial, func(c volume_server_pb.VolumeServerClient) error {
			_, err := c.VolumeEcShardsInfo(ctx, &volume_server_pb.VolumeEcShardsInfoRequest{VolumeId: r.Volume})
			if err == nil {
				return fmt.Errorf("卷 %d 已挂载分片但原编码回执缺失，保留原布局", r.Volume)
			}
			if noOPSDVolume(err, r.Volume) {
				return nil
			}
			return err
		})
		if err != nil {
			return out, err
		}
	}
	task := NewErasureCodingTask(fmt.Sprint(r.Generation), r.Sources[0], r.Volume, r.Collection, dial)
	task.encodeTsNs = r.Generation
	task.workDir = dir
	for _, src := range r.Sources {
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(src), dial, func(client volume_server_pb.VolumeServerClient) error {
			f, err := regularOPSDVolume(ctx, client, r)
			if err != nil {
				return err
			}
			if f == nil || !f.GetVolumeInfo().GetReadOnly() || f.GetVolumeInfo().GetReadOnlyCanDelete() {
				return fmt.Errorf("源卷未封存或没有数据")
			}
			return checkOPSDSource(r, f)
		})
		if err != nil {
			return out, fmt.Errorf("核对源 %s：%w", src, err)
		}
		task.sources = append(task.sources, &worker_pb.TaskSource{Node: src, VolumeId: r.Volume})
	}
	if err := task.syncAndSelectSourceReplica(); err != nil {
		return out, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	if err := errors.Join(syncOPSDFile(filepath.Dir(dir)), syncOPSDFile(filepath.Dir(filepath.Dir(dir)))); err != nil {
		return out, err
	}
	files, err := task.copyVolumeFilesToWorker(ctx, dir)
	if err != nil {
		return out, err
	}
	info, err := os.Stat(files["dat"])
	if err != nil {
		return out, err
	}
	out.Bytes = info.Size()
	shards, err := task.generateEcShardsLocally(files, dir)
	if err != nil {
		return out, err
	}
	out.BlockSize = task.encodedBlockSize
	out.Data, err = opsdHash(files["dat"])
	if err != nil {
		return out, err
	}
	out.Index, err = opsdHash(shards["ecx"])
	if err != nil {
		return out, err
	}
	out.Samples, err = sampleOPSDReplicas(ctx, r.Volume, task.server, files["dat"], shards["ecx"])
	if err != nil {
		return out, err
	}
	client, err := opsdHTTPClient()
	if err != nil {
		return out, err
	}
	out.Original = make(map[string]opsdReplicaDigest, len(r.Sources))
	for _, source := range r.Sources {
		if source != task.server {
			for _, sample := range out.Samples {
				got, err := opsdReadNeedle(ctx, client, source, sample.FID)
				if err != nil || got != sample.Digest {
					return out, fmt.Errorf("两份普通源的对象 %s 不一致：%v", sample.FID, err)
				}
			}
		}
		data, err := copyOPSDVolumeFile(ctx, source, r, ".dat", false, false, io.Discard, dial)
		if err != nil {
			return out, err
		}
		index, err := copyOPSDVolumeFile(ctx, source, r, ".idx", false, false, io.Discard, dial)
		if err != nil {
			return out, err
		}
		if source == task.server && data != out.Data {
			return out, fmt.Errorf("封存源卷自编码以来已变化")
		}
		out.Original[source] = opsdReplicaDigest{Collection: r.Collection, Data: data.SHA256, Index: index.SHA256}
	}
	for i := range out.Shards {
		path := shards[fmt.Sprintf("ec%02d", i)]
		out.Shards[i], err = opsdHash(path)
		if err != nil || out.Shards[i].Bytes == 0 {
			return out, fmt.Errorf("暂存片 %d 读回失败：%v", i, err)
		}
	}
	for _, file := range shards {
		if err := syncOPSDFile(file); err != nil {
			return out, err
		}
	}
	return out, writeOPSDManifest(dir, out)
}

func syncOPSDFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func opsdHash(path string) (opsdShard, error) {
	f, err := os.Open(path)
	if err != nil {
		return opsdShard{}, err
	}
	defer f.Close()
	digest := sha256.New()
	n, err := io.Copy(digest, f)
	return opsdShard{Bytes: n, SHA256: hex.EncodeToString(digest.Sum(nil))}, err
}

func readOPSDManifest(dir string) (opsdManifest, error) {
	var result opsdManifest
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return result, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&result)
	return result, err
}

func writeOPSDManifest(dir string, result opsdManifest) error {
	tmp, err := os.CreateTemp(dir, ".manifest-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = json.NewEncoder(tmp).Encode(result)
	if err = errors.Join(err, tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(dir, "manifest.json")); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func publishOPSD(ctx context.Context, manifest opsdManifest, dir string, dial grpc.DialOption) error {
	if err := inspectOPSDShards(ctx, manifest, true, dial); err == nil {
		return nil
	}
	if err := inspectOPSDShards(ctx, manifest, false, dial); err != nil {
		return err
	}
	r := manifest.Request
	base := filepath.Join(dir, fmt.Sprint(r.Volume))
	shards := make(map[string]string)
	for i, want := range manifest.Shards {
		name := fmt.Sprintf("ec%02d", i)
		file := base + "." + name
		if got, err := opsdHash(file); err != nil || got != want {
			return fmt.Errorf("暂存片 %d 的内容与代际记录不一致：%v", i, err)
		}
		shards[name] = file
	}
	for _, name := range []string{"ecx", "ecj", "vif", "ecsum"} {
		file := base + "." + name
		if _, err := os.Stat(file); err == nil {
			shards[name] = file
		} else if !errors.Is(err, os.ErrNotExist) || name == "ecx" || name == "vif" {
			return err
		}
	}
	task := NewErasureCodingTask(fmt.Sprint(r.Generation), r.Sources[0], r.Volume, r.Collection, dial)
	task.sourceDiskType = "ssd"
	for _, m := range r.Members {
		if len(m.Shards) == 0 {
			continue
		}
		var existing []int
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(m.Address), dial, func(c volume_server_pb.VolumeServerClient) error {
			var err error
			existing, err = mountedOPSDShards(ctx, manifest, m, c)
			return err
		})
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			missing := slices.DeleteFunc(slices.Clone(m.Shards), func(id int) bool { return slices.Contains(existing, id) })
			if len(missing) > 0 {
				if err := publishOPSDRepair(ctx, manifest, OPSDMember{Address: m.Address, Shards: missing}, base, dial); err != nil {
					return err
				}
			}
			continue
		}
		target := &worker_pb.TaskTarget{Node: m.Address, DiskId: 1}
		for _, id := range m.Shards {
			target.ShardIds = append(target.ShardIds, uint32(id))
		}
		task.targets = append(task.targets, target)
	}
	if len(task.targets) == 0 {
		return nil
	}
	if err := task.distributeEcShards(shards); err != nil {
		return err
	}
	return task.mountEcShards()
}

func inspectOPSDShards(ctx context.Context, manifest opsdManifest, complete bool, dial grpc.DialOption) error {
	r := manifest.Request
	for _, m := range r.Members {
		if len(m.Shards) == 0 {
			continue
		}
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(m.Address), dial, func(client volume_server_pb.VolumeServerClient) error {
			ids, err := mountedOPSDShards(ctx, manifest, m, client)
			if err != nil {
				return err
			}
			if complete {
				if len(ids) != len(m.Shards) {
					return fmt.Errorf("卷 %d 缺少分片，实际 %d，期望 %d", r.Volume, len(ids), len(m.Shards))
				}
				for _, id := range ids {
					if err = verifyOPSDShard(ctx, manifest, id, client); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("核对成员 %s：%w", m.Address, err)
		}
	}
	return nil
}

func mountedOPSDShards(ctx context.Context, manifest opsdManifest, m OPSDMember, client volume_server_pb.VolumeServerClient) ([]int, error) {
	r := manifest.Request
	inventory, err := client.VolumeEcShardsInfo(ctx, &volume_server_pb.VolumeEcShardsInfoRequest{VolumeId: r.Volume})
	if err != nil {
		if noOPSDVolume(err, r.Volume) {
			return nil, nil
		}
		return nil, err
	}
	cfg := inventory.GetEcShardConfig()
	if cfg.GetEncodeTsNs() != r.Generation || cfg.GetDataShards() != opsdECData ||
		cfg.GetParityShards() != opsdECParity || cfg.GetBlockSize() != manifest.BlockSize {
		return nil, fmt.Errorf("卷 %d 的分片代际、比例或布局不一致", r.Volume)
	}
	var ids []int
	for _, shard := range inventory.GetEcShardInfos() {
		id := int(shard.GetShardId())
		if !slices.Contains(m.Shards, id) || slices.Contains(ids, id) || shard.GetCollection() != r.Collection ||
			shard.GetSize() != manifest.Shards[id].Bytes {
			return nil, fmt.Errorf("卷 %d 的分片 %d 位置、身份或大小错误", r.Volume, id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func noOPSDVolume(err error, volume uint32) bool {
	return strings.Contains(err.Error(), fmt.Sprintf("VolumeEcShardsInfo: EC volume %d not found", volume))
}

func verifyOPSDShard(ctx context.Context, m opsdManifest, id int, client volume_server_pb.VolumeServerClient) error {
	return readOPSDShard(ctx, m, id, io.Discard, client)
}

func readOPSDShard(ctx context.Context, m opsdManifest, id int, dst io.Writer, client volume_server_pb.VolumeServerClient) error {
	shard := m.Shards[id]
	stream, err := client.VolumeEcShardRead(ctx, &volume_server_pb.VolumeEcShardReadRequest{
		VolumeId: m.Request.Volume, ShardId: uint32(id), Size: shard.Bytes, EncodeTsNs: m.Request.Generation,
	})
	if err != nil {
		return err
	}
	digest := sha256.New()
	writer := io.MultiWriter(dst, digest)
	var size int64
	for {
		part, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if part.GetEncodeTsNs() != m.Request.Generation {
			return fmt.Errorf("分片 %d 流代际不一致", id)
		}
		p := part.GetData()
		n, err := writer.Write(p)
		size += int64(n)
		if err != nil {
			return err
		}
	}
	if size != shard.Bytes || hex.EncodeToString(digest.Sum(nil)) != shard.SHA256 {
		return fmt.Errorf("分片 %d 内容不同", id)
	}
	return nil
}
