package storagepool

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

const (
	ECDataShards   = 10
	ECParityShards = 4
	ECShardCount   = ECDataShards + ECParityShards
)

type ECMember struct {
	NodeID        string
	FailureDomain string
	Available     int64
	Parity        bool
}

type ECPlacement struct {
	NodeID   string `json:"node_id"`
	ShardIDs []int  `json:"shard_ids"`
}

func ECEligible(now, lastWrite time.Time, size, limit int64) bool {
	return !lastWrite.IsZero() && now.Sub(lastWrite) >= time.Hour &&
		limit > 0 && size >= 30*protocol.PoolMiB && size >= limit-limit/20
}

func PlaceEC(members []ECMember, shardBytes int64) ([]ECPlacement, error) {
	if shardBytes <= 0 {
		return nil, Fault("invalid_request", "EC 单片大小无效")
	}
	type candidate struct {
		ECMember
		slots int
		ids   []int
	}
	available := make(map[string]int)
	seen := make(map[string]bool)
	var targets []candidate
	for _, member := range members {
		if member.NodeID == "" || member.FailureDomain == "" || seen[member.NodeID] {
			return nil, Fault("invalid_request", "EC 成员或故障域重复或缺失")
		}
		seen[member.NodeID] = true
		slots := int(min(ECParityShards, member.Available/shardBytes))
		if slots > 0 {
			available[member.FailureDomain] += slots
			targets = append(targets, candidate{ECMember: member, slots: slots})
		}
	}
	if len(available) < 4 {
		return nil, Fault("invalid_request", "EC 需要至少四个在线且有可用空间的故障域")
	}
	total := 0
	for _, slots := range available {
		total += min(ECParityShards, slots)
	}
	if total < ECShardCount {
		return nil, Fault("insufficient_storage", fmt.Sprintf("EC 需要 %d 个分片位置，按故障域上限可用 %d 个", ECShardCount, total))
	}
	assigned := make(map[string]int)
	for i := range ECShardCount {
		shard := (i + ECDataShards) % ECShardCount
		parity := shard >= ECDataShards
		best := -1
		for j, target := range targets {
			if target.slots == 0 || assigned[target.FailureDomain] >= ECParityShards {
				continue
			}
			if best < 0 {
				best = j
				continue
			}
			previous := targets[best]
			if parity && target.Parity != previous.Parity {
				if target.Parity {
					best = j
				}
				continue
			}
			if order := cmp.Or(cmp.Compare(len(target.ids), len(previous.ids)),
				cmp.Compare(previous.slots, target.slots), cmp.Compare(target.NodeID, previous.NodeID)); order < 0 {
				best = j
			}
		}
		target := &targets[best]
		target.ids = append(target.ids, shard)
		target.slots--
		assigned[target.FailureDomain]++
	}
	var placements []ECPlacement
	for _, target := range targets {
		if len(target.ids) > 0 {
			slices.Sort(target.ids)
			placements = append(placements, ECPlacement{NodeID: target.NodeID, ShardIDs: target.ids})
		}
	}
	slices.SortFunc(placements, func(a, b ECPlacement) int { return cmp.Compare(a.NodeID, b.NodeID) })
	return placements, nil
}
