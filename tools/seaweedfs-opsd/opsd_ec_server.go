package weed_server

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
	"strconv"

	"github.com/seaweedfs/seaweedfs/weed/pb/volume_server_pb"
	"github.com/seaweedfs/seaweedfs/weed/storage"
	"github.com/seaweedfs/seaweedfs/weed/storage/erasure_coding"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"github.com/seaweedfs/seaweedfs/weed/storage/volume_info"
	"github.com/seaweedfs/seaweedfs/weed/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (vs *VolumeServer) receiveOPSDRepairShard(stream volume_server_pb.VolumeServer_ReceiveFileServer, token []string) error {
	ctx := stream.Context()
	if err := vs.checkGrpcAdminAuth(ctx); err != nil {
		return err
	}
	var stamp struct {
		Generation, BlockSize int64
		Bytes                 int64
		SHA256, Index         string
	}
	if len(token) != 1 || json.Unmarshal([]byte(token[0]), &stamp) != nil ||
		stamp.Generation <= 0 || stamp.BlockSize <= 0 || stamp.Bytes <= 0 || stamp.Bytes > 1<<30 ||
		len(stamp.SHA256) != 64 || len(stamp.Index) != 64 {
		return status.Error(codes.InvalidArgument, "EC 修复摘要或代际无效")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	info := first.GetInfo()
	if info == nil || info.VolumeId == 0 || info.ShardId >= 14 || info.Ext != erasure_coding.ToExt(int(info.ShardId)) ||
		!info.IsEcVolume || info.DiskId != 1 || uint64(stamp.Bytes) != info.FileSize || checkVolumeCollection(info.Collection) != nil {
		return status.Error(codes.InvalidArgument, "EC 修复文件身份无效")
	}
	if len(vs.store.Locations) != 2 || filepath.Base(vs.store.Locations[1].Directory) != "ec" {
		return status.Error(codes.FailedPrecondition, "执行位置不属于 OPSD EC 数据目录")
	}
	vid := needle.VolumeId(info.VolumeId)
	lifecycle := vs.store.EcLifecycleLock(vid)
	lifecycle.Lock()
	defer lifecycle.Unlock()
	loc := vs.store.Locations[1]
	base := storage.VolumeFileName(loc.Directory, info.Collection, int(vid))
	stored, _, found, err := volume_info.MaybeLoadVolumeInfo(base + ".vif")
	if err != nil || !found {
		return status.Error(codes.FailedPrecondition, "EC 修复缺少原代际卷信息")
	}
	if cfg := stored.GetEcShardConfig(); cfg.GetEncodeTsNs() != stamp.Generation || cfg.GetDataShards() != 10 ||
		cfg.GetParityShards() != 4 || cfg.GetBlockSize() != stamp.BlockSize {
		return status.Error(codes.FailedPrecondition, "EC 修复卷的代际或编码布局不同")
	}
	if live, ok := loc.FindEcVolume(vid); ok && live.EncodeTsNs != stamp.Generation {
		return status.Error(codes.FailedPrecondition, "当前挂载卷的代际不同")
	}
	if _, hash, err := hashOPSDFile(base + ".ecx"); err != nil || hash != stamp.Index {
		return status.Error(codes.FailedPrecondition, "EC 修复缺少原代际索引")
	}
	dst := base + info.Ext
	if size, hash, err := hashOPSDFile(dst); err == nil {
		if size != stamp.Bytes || hash != stamp.SHA256 {
			return status.Error(codes.FailedPrecondition, "已有分片的摘要不同，保留原文件")
		}
		return stream.SendAndClose(&volume_server_pb.ReceiveFileResponse{BytesWritten: info.FileSize})
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged, err := os.CreateTemp(loc.Directory, ".opsd-ec-repair-")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	digest := sha256.New()
	writer := io.MultiWriter(staged, digest)
	var size int64
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		data := msg.GetFileContent()
		if len(data) == 0 || size+int64(len(data)) > stamp.Bytes {
			return status.Error(codes.InvalidArgument, "EC 修复文件超出固定大小")
		}
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		size += int64(n)
	}
	if size != stamp.Bytes || hex.EncodeToString(digest.Sum(nil)) != stamp.SHA256 {
		return status.Error(codes.DataLoss, "EC 修复摘要与原代际不同")
	}
	if err := errors.Join(staged.Sync(), staged.Close(), ctx.Err()); err != nil {
		return err
	}
	if err := os.Link(staged.Name(), dst); err != nil {
		return err
	}
	if err := util.FsyncDir(loc.Directory); err != nil {
		return err
	}
	return stream.SendAndClose(&volume_server_pb.ReceiveFileResponse{BytesWritten: uint64(size)})
}

func hashOPSDFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	size, err := io.Copy(h, f)
	return size, hex.EncodeToString(h.Sum(nil)), errors.Join(err, f.Close())
}

func (vs *VolumeServer) opsdDeleteECGeneration(ctx context.Context, req *volume_server_pb.VolumeEcShardsDeleteRequest) (*volume_server_pb.VolumeEcShardsDeleteResponse, error) {
	token := metadata.ValueFromIncomingContext(ctx, "opsd-ec-clean-generation")
	if !req.FullTeardown || len(token) != 1 {
		return nil, status.Error(codes.FailedPrecondition, "OPSD EC 清理需要原编码代际")
	}
	generation, err := strconv.ParseInt(token[0], 10, 64)
	if err != nil || generation <= 0 || checkVolumeCollection(req.Collection) != nil {
		return nil, status.Error(codes.InvalidArgument, "EC 清理参数无效")
	}
	vid := needle.VolumeId(req.VolumeId)
	lifecycle := vs.store.EcLifecycleLock(vid)
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if len(vs.store.Locations) != 2 || filepath.Base(vs.store.Locations[1].Directory) != "ec" {
		return nil, status.Error(codes.FailedPrecondition, "执行位置不属于 OPSD EC 数据目录")
	}
	loc := vs.store.Locations[1]
	base := storage.VolumeFileName(loc.Directory, req.Collection, int(vid))
	info, _, found, err := volume_info.MaybeLoadVolumeInfo(base + ".vif")
	if err != nil {
		return nil, err
	}
	exts := []string{".ecx", ".ecj", ".ecsum"}
	for id := range 14 {
		exts = append(exts, erasure_coding.ToExt(id))
	}
	if !found {
		for _, ext := range exts {
			if _, err := os.Lstat(base + ext); !errors.Is(err, os.ErrNotExist) {
				return nil, status.Error(codes.FailedPrecondition, "EC 文件存在但编码代际缺失")
			}
		}
		return &volume_server_pb.VolumeEcShardsDeleteResponse{FullTeardownDone: true}, nil
	}
	if cfg := info.GetEcShardConfig(); cfg.GetEncodeTsNs() != generation || cfg.GetDataShards() != 10 || cfg.GetParityShards() != 4 {
		return nil, status.Error(codes.FailedPrecondition, "EC 文件代际或编码比例与清理记录不同")
	}
	if live, ok := loc.FindEcVolume(vid); ok && live.EncodeTsNs != generation {
		return nil, status.Error(codes.FailedPrecondition, "已挂载的 EC 代际与清理记录不同")
	}
	for id := range 14 {
		vs.store.UnmountEcShardOnDisk(1, vid, erasure_coding.ShardId(id))
	}
	for _, ext := range append(exts, ".vif") {
		if err := os.Remove(base + ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := util.FsyncDir(loc.Directory); err != nil {
		return nil, err
	}
	return &volume_server_pb.VolumeEcShardsDeleteResponse{FullTeardownDone: true}, nil
}

func (vs *VolumeServer) checkOPSDReplicaRemoval(ctx context.Context, req *volume_server_pb.VolumeDeleteRequest) (bool, error) {
	token := metadata.ValueFromIncomingContext(ctx, "opsd-ec-clean-replica")
	if len(token) == 0 {
		return true, nil
	}
	var stamp struct {
		Collection string `json:"collection"`
		Data       string `json:"data"`
		Index      string `json:"index"`
	}
	if len(token) != 1 || json.Unmarshal([]byte(token[0]), &stamp) != nil ||
		len(stamp.Data) != 64 || len(stamp.Index) != 64 || checkVolumeCollection(stamp.Collection) != nil ||
		req.OnlyEmpty || req.OnlyGarbage || !req.KeepRemoteData {
		return false, status.Error(codes.InvalidArgument, "源卷清理身份无效")
	}
	if len(vs.store.Locations) != 2 || filepath.Base(vs.store.Locations[0].Directory) != "replica" {
		return false, status.Error(codes.FailedPrecondition, "执行位置不属于 OPSD 普通卷目录")
	}
	vid := needle.VolumeId(req.VolumeId)
	base := storage.VolumeFileName(vs.store.Locations[0].Directory, stamp.Collection, int(vid))
	_, datErr := os.Lstat(base + ".dat")
	_, idxErr := os.Lstat(base + ".idx")
	if errors.Is(datErr, os.ErrNotExist) && errors.Is(idxErr, os.ErrNotExist) && vs.store.GetVolume(vid) == nil {
		return false, nil
	}
	if err := errors.Join(datErr, idxErr); err != nil {
		return false, err
	}
	info, _, found, err := volume_info.MaybeLoadVolumeInfo(base + ".vif")
	if err != nil || !found || !info.GetReadOnly() || info.GetReadOnlyCanDelete() {
		return false, status.Error(codes.FailedPrecondition, "源卷只读标记缺失")
	}
	for _, item := range []struct{ ext, expected string }{{".dat", stamp.Data}, {".idx", stamp.Index}} {
		_, hash, err := hashOPSDFile(base + item.ext)
		if err != nil {
			return false, err
		}
		if hash != item.expected {
			return false, fmt.Errorf("封存源卷 %d 的 %s 已变化", vid, item.ext)
		}
	}
	if err := ctx.Err(); err != nil {
		return false, status.FromContextError(err).Err()
	}
	if vs.store.GetVolume(vid) == nil {
		if err := vs.store.MountVolumeClaimed(vid, &stamp.Collection); err != nil {
			return false, err
		}
	}
	if v := vs.store.GetVolume(vid); v == nil || !v.IsReadOnly() || v.Collection != stamp.Collection {
		return false, status.Error(codes.FailedPrecondition, "已挂载源卷与删除记录不同")
	}
	return true, nil
}
