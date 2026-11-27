package erasure_coding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/seaweedfs/seaweedfs/weed/operation"
	"github.com/seaweedfs/seaweedfs/weed/pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/master_pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/volume_server_pb"
	"github.com/seaweedfs/seaweedfs/weed/security"
	"github.com/seaweedfs/seaweedfs/weed/storage/erasure_coding"
	"github.com/seaweedfs/seaweedfs/weed/storage/idx"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/types"
	"github.com/seaweedfs/seaweedfs/weed/storage/volume_info"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type opsdVolume struct {
	Volume     uint32 `json:"volume"`
	Collection string `json:"collection"`
	Address    string `json:"address"`
	Bytes      int64  `json:"bytes"`
	ShardBytes int64  `json:"shard_bytes"`
	LastWrite  int64  `json:"last_write"`
	ReadOnly   bool   `json:"read_only"`
	Generation int64  `json:"generation"`
	ShardBits  uint32 `json:"shard_bits"`
}

type opsdScan struct {
	Limit        int64            `json:"limit"`
	Volumes      []opsdVolume     `json:"volumes"`
	ReplicaSlots map[string]int64 `json:"replica_slots"`
}

func scanOPSDVolumes(ctx context.Context, masters []string, dial grpc.DialOption) (opsdScan, error) {
	var out opsdScan
	if len(masters) != 3 {
		return out, fmt.Errorf("需要三个固定 Master 地址")
	}
	var topology *master_pb.VolumeListResponse
	var err error
	for _, address := range masters {
		try, cancel := context.WithTimeout(ctx, 4*time.Second)
		err = pb.WithMasterClient(try, false, pb.ServerAddress(address), dial, false, func(c master_pb.SeaweedClient) error {
			topology, err = c.VolumeList(try, &master_pb.VolumeListRequest{})
			return err
		})
		cancel()
		if err == nil {
			break
		}
	}
	if err != nil {
		return out, err
	}
	out.Limit = int64(topology.GetVolumeSizeLimitMb()) << 20
	out.ReplicaSlots = make(map[string]int64)
	for _, dc := range topology.GetTopologyInfo().GetDataCenterInfos() {
		for _, rack := range dc.GetRackInfos() {
			for _, node := range rack.GetDataNodeInfos() {
				address := string(pb.NewServerAddressFromDataNode(node))
				for _, disk := range node.GetDiskInfos() {
					if types.ToDiskType(disk.GetType()) == types.HardDriveType {
						out.ReplicaSlots[address] = max(0, disk.GetFreeVolumeCount())
					}
					for _, v := range disk.GetVolumeInfos() {
						if v.GetReplicaPlacement() != 10 || v.GetTtl() != 0 || v.GetRemoteStorageName() != "" {
							continue
						}
						size := int64(v.GetSize())
						out.Volumes = append(out.Volumes, opsdVolume{Volume: v.GetId(), Collection: v.GetCollection(), Address: address,
							Bytes: size, ShardBytes: erasure_coding.UniformBlockSize(size, opsdECData),
							LastWrite: v.GetModifiedAtSecond(), ReadOnly: v.GetReadOnly() && !v.GetReadOnlyCanDelete()})
					}
					for _, v := range disk.GetEcShardInfos() {
						out.Volumes = append(out.Volumes, opsdVolume{Volume: v.GetId(), Collection: v.GetCollection(), Address: address,
							Generation: v.GetEncodeTsNs(), ShardBits: v.GetEcIndexBits()})
					}
				}
			}
		}
	}
	return out, nil
}

type opsdSample struct {
	FID    string    `json:"fid"`
	Digest opsdShard `json:"digest"`
}

type opsdReplicaDigest struct {
	Collection string `json:"collection"`
	Data       string `json:"data"`
	Index      string `json:"index"`
}

type opsdHTTP interface {
	Do(*http.Request) (*http.Response, error)
}

func opsdHTTPClient() (opsdHTTP, error) {
	c, err := security.LoadHTTPClientFromFile("/etc/seaweedfs/security.toml")
	if err == nil && c == nil {
		return nil, fmt.Errorf("池 Volume 验证需要 HTTPS")
	}
	return c, err
}

func sampleOPSDReplicas(ctx context.Context, vid uint32, server, datFile, indexFile string) ([]opsdSample, error) {
	c, err := opsdHTTPClient()
	if err != nil {
		return nil, err
	}
	dat, err := os.Open(datFile)
	if err != nil {
		return nil, err
	}
	defer dat.Close()
	index, err := os.Open(indexFile)
	if err != nil {
		return nil, err
	}
	defer index.Close()
	var samples []opsdSample
	var header [types.NeedleHeaderSize]byte
	err = idx.WalkIndexFile(index, 0, func(key types.NeedleId, offset types.Offset, size types.Size) error {
		if size.IsDeleted() || offset.ToActualOffset() == 0 {
			return nil
		}
		if _, err := dat.ReadAt(header[:], offset.ToActualOffset()); err != nil {
			return err
		}
		var n needle.Needle
		n.ParseNeedleHeader(header[:])
		if n.Id != key {
			return fmt.Errorf("卷 %d 索引指向错误数据", vid)
		}
		fid := needle.NewFileId(needle.VolumeId(vid), uint64(key), uint32(n.Cookie)).String()
		got, err := opsdReadNeedle(ctx, c, server, fid)
		if err != nil {
			return err
		}
		samples = append(samples, opsdSample{FID: fid, Digest: got})
		if len(samples) == 100 {
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err == nil && len(samples) == 0 {
		err = fmt.Errorf("卷 %d 没有可抽读的数据", vid)
	}
	return samples, err
}

func opsdReadNeedle(ctx context.Context, c opsdHTTP, server string, fid string) (opsdShard, error) {
	address := pb.ServerAddress(server).ToHttpAddress()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+address+"/"+fid, nil)
	if err != nil {
		return opsdShard{}, err
	}
	res, err := c.Do(req)
	if err != nil {
		return opsdShard{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return opsdShard{}, fmt.Errorf("卷数据 %s 从 %s 读取返回 HTTP %d", fid, address, res.StatusCode)
	}
	hash := sha256.New()
	bytes, err := io.Copy(hash, res.Body)
	return opsdShard{Bytes: bytes, SHA256: hex.EncodeToString(hash.Sum(nil))}, err
}

func verifyOPSDConversion(ctx context.Context, m opsdManifest, dial grpc.DialOption) error {
	if err := inspectOPSDShards(ctx, m, true, dial); err != nil {
		return err
	}
	var readers []string
	for _, node := range m.Request.Members {
		if len(node.Shards) == 0 {
			continue
		}
		index, err := copyOPSDVolumeFile(ctx, node.Address, m.Request, ".ecx", true, false, io.Discard, dial)
		if err != nil {
			return err
		}
		if index != m.Index {
			return fmt.Errorf("成员 %s 的 EC 索引与编码内容不同", node.Address)
		}
		if !slices.Contains(m.Request.Sources, node.Address) {
			readers = append(readers, node.Address)
		}
	}
	if len(readers) == 0 || len(m.Samples) == 0 {
		return fmt.Errorf("EC 实际读取缺少独立分片节点或源摘要")
	}
	c, err := opsdHTTPClient()
	if err != nil {
		return err
	}
	for _, sample := range m.Samples {
		if err := verifyOPSDNeedle(ctx, c, readers, sample); err != nil {
			return err
		}
	}
	return nil
}

func verifyOPSDNeedle(ctx context.Context, c opsdHTTP, readers []string, sample opsdSample) error {
	read, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for {
		var last error
		for _, reader := range readers {
			try, stop := context.WithTimeout(read, 4*time.Second)
			got, err := opsdReadNeedle(try, c, reader, sample.FID)
			stop()
			if err == nil {
				if got != sample.Digest {
					return fmt.Errorf("EC 数据 %s 与源摘要不同", sample.FID)
				}
				return nil
			}
			last = err
		}
		select {
		case <-read.Done():
			return last
		case <-time.After(time.Second):
		}
	}
}

func finishOPSDTransition(ctx context.Context, action string, m opsdManifest, restore []string, dir string, dial grpc.DialOption) (any, error) {
	if action == "clean-replicas" {
		if err := verifyOPSDConversion(ctx, m, dial); err != nil {
			return nil, err
		}
		err := cleanOPSDReplicas(ctx, m, dir, dial)
		return m.Request.Sources, err
	}
	if len(restore) != 2 {
		return nil, fmt.Errorf("缺少恢复双副本的两个固定目标")
	}
	if action == "restore-replicas" {
		return restoreOPSDReplicas(ctx, m, restore, dir, dial)
	}
	return restore, cleanOPSDShards(ctx, m, restore, dir, dial)
}

func regularOPSDVolume(ctx context.Context, c volume_server_pb.VolumeServerClient, r OPSDRequest) (*volume_server_pb.ReadVolumeFileStatusResponse, error) {
	v, err := c.VolumeStatus(ctx, &volume_server_pb.VolumeStatusRequest{VolumeId: r.Volume})
	if err != nil {
		if strings.Contains(err.Error(), fmt.Sprintf("not found volume id %d", r.Volume)) {
			return nil, nil
		}
		return nil, err
	}
	if !v.GetIsReadOnly() || v.GetVolumeSize() == 0 {
		return nil, fmt.Errorf("普通卷 %d 未完整封存", r.Volume)
	}
	f, err := c.ReadVolumeFileStatus(ctx, &volume_server_pb.ReadVolumeFileStatusRequest{VolumeId: r.Volume})
	if err != nil {
		return nil, err
	}
	if f.GetCollection() != r.Collection || f.GetExpireAtSec() != 0 {
		return nil, fmt.Errorf("普通卷 %d 与固定集合或寿命不符", r.Volume)
	}
	return f, nil
}

func cleanOPSDReplicas(ctx context.Context, m opsdManifest, dir string, dial grpc.DialOption) error {
	r := m.Request
	for _, address := range r.Sources {
		if len(m.Original[address].Data) != 64 || len(m.Original[address].Index) != 64 {
			return fmt.Errorf("源 %s 缺少封存时的摘要", address)
		}
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(address), dial, func(c volume_server_pb.VolumeServerClient) error {
			_, err := regularOPSDVolume(ctx, c, r)
			return err
		})
		if err != nil {
			return fmt.Errorf("核对待清理源副本 %s：%w", address, err)
		}
	}
	for _, address := range r.Sources {
		stamp, err := json.Marshal(m.Original[address])
		if err != nil {
			return err
		}
		err = operation.WithVolumeServerClient(false, pb.ServerAddress(address), dial, func(c volume_server_pb.VolumeServerClient) error {
			exact := metadata.AppendToOutgoingContext(ctx, "opsd-ec-clean-replica", string(stamp))
			_, err := c.VolumeDelete(exact, &volume_server_pb.VolumeDeleteRequest{VolumeId: r.Volume, KeepRemoteData: true})
			if err != nil {
				return err
			}
			f, err := regularOPSDVolume(ctx, c, r)
			if err == nil && f != nil {
				err = fmt.Errorf("仍可读到普通源卷")
			}
			return err
		})
		if err != nil {
			return fmt.Errorf("清理源 %s：%w", address, err)
		}
	}
	exts := []string{".dat", ".idx", ".ecx", ".ecj", ".ecsum", ".vif"}
	for id := range opsdECTotal {
		exts = append(exts, fmt.Sprintf(".ec%02d", id))
	}
	return removeOPSDStage(filepath.Join(dir, fmt.Sprint(r.Volume)), exts)
}

func removeOPSDStage(base string, exts []string) error {
	for _, ext := range exts {
		if err := os.Remove(base + ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncOPSDFile(filepath.Dir(base))
}

func copyOPSDVolumeFile(ctx context.Context, server string, r OPSDRequest, ext string, ec, optional bool, dst io.Writer, dial grpc.DialOption) (opsdShard, error) {
	hash := sha256.New()
	target := io.MultiWriter(dst, hash)
	var size int64
	err := operation.WithVolumeServerClient(false, pb.ServerAddress(server), dial, func(c volume_server_pb.VolumeServerClient) error {
		st, err := c.CopyFile(ctx, &volume_server_pb.CopyFileRequest{VolumeId: r.Volume, Ext: ext, Collection: r.Collection,
			CompactionRevision: math.MaxUint32, StopOffset: math.MaxInt64, IsEcVolume: ec, IgnoreSourceFileNotFound: optional})
		if err != nil {
			return err
		}
		for {
			f, err := st.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			n, err := target.Write(f.GetFileContent())
			size += int64(n)
			if err != nil {
				return err
			}
		}
	})
	return opsdShard{Bytes: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, err
}

type opsdRestored struct {
	Targets []string             `json:"targets"`
	Data    opsdShard            `json:"data"`
	Index   opsdShard            `json:"index"`
	Journal map[string]opsdShard `json:"journal"`
}

func restoreOPSDReplicas(ctx context.Context, m opsdManifest, targets []string, dir string, dial grpc.DialOption) (opsdRestored, error) {
	var out opsdRestored
	record, err := os.ReadFile(filepath.Join(dir, "restore.json"))
	switch {
	case err == nil:
		if err = json.Unmarshal(record, &out); err != nil || !slices.Equal(out.Targets, targets) {
			return out, fmt.Errorf("原恢复计划的目标不一致：%v", err)
		}
	case errors.Is(err, os.ErrNotExist):
		if err = inspectOPSDShards(ctx, m, true, dial); err != nil {
			return out, err
		}
		out, err = stageOPSDRestore(ctx, m, targets, dir, dial)
		if err != nil {
			return out, err
		}
	default:
		return out, err
	}
	base := filepath.Join(dir, "restored")
	for _, target := range targets {
		ready, err := verifyOPSDReplica(ctx, m.Request, out, target, dial)
		if err != nil {
			return out, err
		}
		if ready {
			continue
		}
		for _, ext := range []string{".dat", ".idx", ".vif"} {
			info := &volume_server_pb.ReceiveFileInfo{VolumeId: m.Request.Volume, Collection: m.Request.Collection, Ext: ext, DiskType: "hdd"}
			if err = sendOPSDFile(ctx, target, base+ext, info, dial); err != nil {
				return out, err
			}
		}
	}
	for _, target := range targets {
		err = operation.WithVolumeServerClient(false, pb.ServerAddress(target), dial, func(c volume_server_pb.VolumeServerClient) error {
			f, err := regularOPSDVolume(ctx, c, m.Request)
			if err != nil || f != nil {
				return err
			}
			_, err = c.VolumeEcShardsToVolume(ctx, &volume_server_pb.VolumeEcShardsToVolumeRequest{VolumeId: m.Request.Volume,
				Collection: m.Request.Collection, FromStaged: true, DiskType: "hdd"})
			return err
		})
		if err != nil {
			return out, err
		}
		ready, err := verifyOPSDReplica(ctx, m.Request, out, target, dial)
		if err != nil || !ready {
			return out, fmt.Errorf("恢复副本 %s 读回失败：%v", target, err)
		}
	}
	return out, nil
}

func stageOPSDRestore(ctx context.Context, m opsdManifest, targets []string, dir string, dial grpc.DialOption) (opsdRestored, error) {
	out := opsdRestored{Targets: targets, Journal: map[string]opsdShard{}}
	base := filepath.Join(dir, "restored")
	var files [opsdECData]string
	indexWritten := false
	journal, err := os.Create(base + ".ecj")
	if err != nil {
		return out, err
	}
	defer journal.Close()
	for _, member := range m.Request.Members {
		if len(member.Shards) == 0 {
			continue
		}
		out.Journal[member.Address], err = copyOPSDVolumeFile(ctx, member.Address, m.Request, ".ecj", true, true, journal, dial)
		if err != nil {
			return out, err
		}
		if !indexWritten {
			index, err := os.Create(base + ".ecx")
			if err != nil {
				return out, err
			}
			got, err := copyOPSDVolumeFile(ctx, member.Address, m.Request, ".ecx", true, false, index, dial)
			if err = errors.Join(err, index.Sync(), index.Close()); err != nil || got != m.Index {
				return out, fmt.Errorf("恢复源 EC 索引不匹配：%v", err)
			}
			indexWritten = true
		}
		for _, id := range member.Shards {
			if id >= len(files) {
				continue
			}
			files[id] = fmt.Sprintf("%s.ec%02d", base, id)
			if err := fetchOPSDShard(ctx, m, member.Address, id, files[id], dial); err != nil {
				return out, err
			}
		}
	}
	if err = journal.Sync(); err != nil {
		return out, err
	}
	if err = erasure_coding.WriteDatFile(base, m.Bytes, m.Bytes, files[:], m.BlockSize, m.BlockSize); err != nil {
		return out, err
	}
	if out.Data, err = opsdHash(base + ".dat"); err != nil || out.Data != m.Data {
		return out, fmt.Errorf("EC 重建的普通卷与源摘要不同：%v", err)
	}
	if err = erasure_coding.WriteIdxFileFromEcIndex(base); err != nil {
		return out, err
	}
	if out.Index, err = opsdHash(base + ".idx"); err != nil {
		return out, err
	}
	if err = volume_info.SaveVolumeInfo(base+".vif", &volume_server_pb.VolumeInfo{
		Replication: "010", Version: uint32(needle.GetCurrentVersion()), ReadOnly: true, DatFileSize: m.Bytes,
	}); err != nil {
		return out, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	tmp := filepath.Join(dir, "restore.pending")
	if err = os.WriteFile(tmp, raw, 0600); err == nil {
		err = syncOPSDFile(tmp)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, "restore.json"))
	}
	if err != nil {
		return out, err
	}
	return out, syncOPSDFile(dir)
}

func fetchOPSDShard(ctx context.Context, m opsdManifest, server string, id int, dst string, dial grpc.DialOption) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	err = operation.WithVolumeServerClient(false, pb.ServerAddress(server), dial, func(c volume_server_pb.VolumeServerClient) error {
		return readOPSDShard(ctx, m, id, f, c)
	})
	return errors.Join(err, f.Sync(), f.Close())
}

func verifyOPSDReplica(ctx context.Context, r OPSDRequest, restore opsdRestored, target string, dial grpc.DialOption) (bool, error) {
	var found bool
	err := operation.WithVolumeServerClient(false, pb.ServerAddress(target), dial, func(c volume_server_pb.VolumeServerClient) error {
		f, err := regularOPSDVolume(ctx, c, r)
		found = f != nil
		return err
	})
	if err != nil || !found {
		return false, err
	}
	for _, want := range []struct {
		ext    string
		digest opsdShard
	}{{".dat", restore.Data}, {".idx", restore.Index}} {
		got, err := copyOPSDVolumeFile(ctx, target, r, want.ext, false, false, io.Discard, dial)
		if err != nil || got != want.digest {
			return false, fmt.Errorf("恢复副本 %s %s 摘要不同：%v", target, want.ext, err)
		}
	}
	return true, nil
}

func sendOPSDFile(ctx context.Context, target, path string, request *volume_server_pb.ReceiveFileInfo, dial grpc.DialOption) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	request.FileSize = uint64(info.Size())
	return operation.WithVolumeServerClient(false, pb.ServerAddress(target), dial, func(c volume_server_pb.VolumeServerClient) error {
		st, err := c.ReceiveFile(ctx)
		if err != nil {
			return err
		}
		err = st.Send(&volume_server_pb.ReceiveFileRequest{Data: &volume_server_pb.ReceiveFileRequest_Info{Info: request}})
		buf := make([]byte, 256<<10)
		for err == nil {
			n, readErr := f.Read(buf)
			if n > 0 {
				err = st.Send(&volume_server_pb.ReceiveFileRequest{Data: &volume_server_pb.ReceiveFileRequest_FileContent{FileContent: buf[:n]}})
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("目标 %s 暂存 %s 失败：%w", target, request.Ext, err)
		}
		resp, recvErr := st.CloseAndRecv()
		if recvErr != nil || resp.GetError() != "" || resp.GetBytesWritten() != request.FileSize {
			return fmt.Errorf("目标 %s 暂存 %s 失败：%s %v", target, request.Ext, resp.GetError(), recvErr)
		}
		return nil
	})
}

func cleanOPSDShards(ctx context.Context, m opsdManifest, targets []string, dir string, dial grpc.DialOption) error {
	raw, err := os.ReadFile(filepath.Join(dir, "restore.json"))
	if err != nil {
		return err
	}
	var restore opsdRestored
	if err = json.Unmarshal(raw, &restore); err != nil || !slices.Equal(restore.Targets, targets) {
		return fmt.Errorf("恢复目标与验证记录不一致：%v", err)
	}
	for _, target := range targets {
		ok, err := verifyOPSDReplica(ctx, m.Request, restore, target, dial)
		if err != nil || !ok {
			return fmt.Errorf("恢复副本 %s 未验证：%v", target, err)
		}
	}
	for _, member := range m.Request.Members {
		if len(member.Shards) == 0 {
			continue
		}
		address := member.Address
		journal, err := copyOPSDVolumeFile(ctx, address, m.Request, ".ecj", true, true, io.Discard, dial)
		// 已清理的成员没有日志，精确代际清理 RPC 会再核对其落盘身份。
		if err != nil || journal.Bytes > 0 && journal != restore.Journal[address] {
			return fmt.Errorf("EC 删除日志自双副本恢复后变化：%s %v", address, err)
		}
	}
	for _, member := range m.Request.Members {
		if len(member.Shards) == 0 {
			continue
		}
		err = operation.WithVolumeServerClient(false, pb.ServerAddress(member.Address), dial, func(c volume_server_pb.VolumeServerClient) error {
			exact := metadata.AppendToOutgoingContext(ctx, "opsd-ec-clean-generation", fmt.Sprint(m.Request.Generation))
			_, err := c.VolumeEcShardsDelete(exact, &volume_server_pb.VolumeEcShardsDeleteRequest{
				VolumeId: m.Request.Volume, Collection: m.Request.Collection, FullTeardown: true,
			})
			return err
		})
		if err != nil {
			return fmt.Errorf("清理成员 %s 的原代际分片：%w", member.Address, err)
		}
	}
	exts := []string{".dat", ".idx", ".vif", ".ecx", ".ecj"}
	for id := range opsdECData {
		exts = append(exts, fmt.Sprintf(".ec%02d", id))
	}
	return removeOPSDStage(filepath.Join(dir, "restored"), exts)
}
