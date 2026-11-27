package erasure_coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/seaweedfs/seaweedfs/weed/operation"
	"github.com/seaweedfs/seaweedfs/weed/pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/volume_server_pb"
	"github.com/seaweedfs/seaweedfs/weed/storage/erasure_coding"
	"github.com/seaweedfs/seaweedfs/weed/storage/needle"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func repairOPSDShards(ctx context.Context, m opsdManifest, dir string, dial grpc.DialOption) error {
	r := m.Request
	var present [opsdECTotal]string
	var missing []OPSDMember
	for _, member := range r.Members {
		if len(member.Shards) == 0 {
			continue
		}
		var ids []int
		err := operation.WithVolumeServerClient(false, pb.ServerAddress(member.Address), dial, func(c volume_server_pb.VolumeServerClient) error {
			var err error
			ids, err = mountedOPSDShards(ctx, m, member, c)
			return err
		})
		if err != nil {
			return fmt.Errorf("修复成员 %s 尚未恢复原代际：%w", member.Address, err)
		}
		for _, id := range ids {
			present[id] = member.Address
		}
		if needed := slices.DeleteFunc(slices.Clone(member.Shards), func(id int) bool { return present[id] != "" }); len(needed) > 0 {
			missing = append(missing, OPSDMember{Address: member.Address, Shards: needed})
		}
	}
	if len(missing) == 0 {
		return nil
	}
	available := 0
	for _, address := range present {
		if address != "" {
			available++
		}
	}
	if available < opsdECData {
		return fmt.Errorf("卷 %d 原代际只剩 %d 片，无法修复", r.Volume, available)
	}
	scratch := filepath.Join(dir, "repair")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		return err
	}
	base := filepath.Join(scratch, "shards")
	var exts []string
	for id := range opsdECTotal {
		exts = append(exts, erasure_coding.ToExt(id))
	}
	if err := removeOPSDStage(base, exts); err != nil {
		return err
	}
	defer removeOPSDStage(base, exts)
	for id, address := range present {
		if address != "" {
			if err := fetchOPSDShard(ctx, m, address, id, base+exts[id], dial); err != nil {
				return err
			}
		}
	}
	if _, err := erasure_coding.RebuildEcFiles(base, erasure_coding.NewDefaultECContext(r.Collection, needle.VolumeId(r.Volume)), false); err != nil {
		return err
	}
	for _, target := range missing {
		if err := publishOPSDRepair(ctx, m, target, base, dial); err != nil {
			return err
		}
	}
	return inspectOPSDShards(ctx, m, true, dial)
}

func publishOPSDRepair(ctx context.Context, m opsdManifest, target OPSDMember, base string, dial grpc.DialOption) error {
	r := m.Request
	var ids []uint32
	for _, id := range target.Shards {
		ext := erasure_coding.ToExt(id)
		rebuilt, err := opsdHash(base + ext)
		if err != nil || rebuilt != m.Shards[id] {
			return fmt.Errorf("修复片 %d 与原代际摘要不同：%v", id, err)
		}
		stamp, err := json.Marshal(struct {
			Generation, BlockSize int64
			Bytes                 int64
			SHA256, Index         string
		}{r.Generation, m.BlockSize, rebuilt.Bytes, rebuilt.SHA256, m.Index.SHA256})
		if err != nil {
			return err
		}
		exact := metadata.AppendToOutgoingContext(ctx, "opsd-ec-repair", string(stamp))
		info := &volume_server_pb.ReceiveFileInfo{VolumeId: r.Volume, Collection: r.Collection, Ext: ext,
			IsEcVolume: true, DiskId: 1, ShardId: uint32(id)}
		if err = sendOPSDFile(exact, target.Address, base+ext, info, dial); err != nil {
			return err
		}
		ids = append(ids, uint32(id))
	}
	return operation.WithVolumeServerClient(false, pb.ServerAddress(target.Address), dial, func(c volume_server_pb.VolumeServerClient) error {
		_, err := c.VolumeEcShardsMount(ctx, &volume_server_pb.VolumeEcShardsMountRequest{
			VolumeId: r.Volume, Collection: r.Collection, ShardIds: ids, SourceDiskType: "ssd",
		})
		return err
	})
}
