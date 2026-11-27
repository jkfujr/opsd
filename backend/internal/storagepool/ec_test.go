package storagepool

import (
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestECEligibilityAndPlacement(t *testing.T) {
	now := time.Unix(10000, 0)
	size := 95 * protocol.PoolMiB
	limit := 100 * protocol.PoolMiB
	if !ECEligible(now, now.Add(-time.Hour), size, limit) ||
		ECEligible(now, now.Add(-time.Hour+time.Second), size, limit) ||
		ECEligible(now, now.Add(-time.Hour), size-1, limit) ||
		ECEligible(now, now.Add(-time.Hour), 29*protocol.PoolMiB, 29*protocol.PoolMiB) {
		t.Fatal("EC 触发年龄、装满比例或最小大小不符")
	}
	members := []ECMember{
		{NodeID: "a", FailureDomain: "A", Available: 1, Parity: true},
		{NodeID: "b", FailureDomain: "A", Available: 8, Parity: true},
		{NodeID: "c", FailureDomain: "B", Available: 4},
		{NodeID: "d", FailureDomain: "C", Available: 4},
		{NodeID: "e", FailureDomain: "D", Available: 3},
	}
	placements, err := PlaceEC(members, 1)
	if err != nil {
		t.Fatal(err)
	}
	var shards []int
	domains := map[string]int{}
	for _, placement := range placements {
		for _, node := range members {
			if node.NodeID != placement.NodeID {
				continue
			}
			domains[node.FailureDomain] += len(placement.ShardIDs)
			for _, id := range placement.ShardIDs {
				if (id >= ECDataShards) != node.Parity {
					t.Fatalf("校验片优先成员分配错误：%+v", placement)
				}
			}
		}
		shards = append(shards, placement.ShardIDs...)
	}
	slices.Sort(shards)
	if len(shards) != ECShardCount || len(domains) != 4 {
		t.Fatalf("分片或故障域不足：%v %v", shards, domains)
	}
	for _, n := range domains {
		if n > ECParityShards {
			t.Fatalf("故障域超过四片：%v", domains)
		}
	}
	for id, shard := range shards {
		if id != shard {
			t.Fatalf("分片号重复或缺失：%v", shards)
		}
	}
	members[0].Parity, members[1].Parity = false, false
	if _, err = PlaceEC(members, 1); err != nil {
		t.Fatal("无校验片优先成员时应回退至其它节点", err)
	}
	members[4].Available = 1
	if _, err = PlaceEC(members, 1); err == nil {
		t.Fatal("四个故障域合计不足十四片应拒绝")
	}
}
