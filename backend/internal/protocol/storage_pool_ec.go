package protocol

import "encoding/json"

type PoolECSettings struct {
	ID              string   `json:"id"`
	Revision        uint64   `json:"revision"`
	Enabled         bool     `json:"enabled"`
	RestoreReplicas bool     `json:"restore_replicas,omitempty"`
	ParityPreferred []string `json:"parity_preferred_node_ids,omitempty"`
}

type PoolECMember struct {
	Address string `json:"address"`
	Domain  string `json:"domain"`
	Shards  []int  `json:"shards,omitempty"`
}

type PoolECWorkerInput struct {
	Action      string         `json:"action"`
	Volume      uint32         `json:"volume"`
	Collection  string         `json:"collection"`
	Generation  int64          `json:"generation"`
	SourceBytes int64          `json:"source_bytes,omitempty"`
	QuietBefore int64          `json:"quiet_before,omitempty"`
	Sources     []string       `json:"sources"`
	Members     []PoolECMember `json:"members"`
	Restore     []string       `json:"restore,omitempty"`
	Masters     []string       `json:"masters,omitempty"`
}

type PoolECJob struct {
	ID      string            `json:"id"`
	Attempt int               `json:"attempt"`
	Start   bool              `json:"start"`
	Input   PoolECWorkerInput `json:"input"`
}

type PoolECJobResult struct {
	State   string          `json:"state"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type PoolECVolume struct {
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

type PoolECScan struct {
	Limit        int64            `json:"limit"`
	Volumes      []PoolECVolume   `json:"volumes"`
	ReplicaSlots map[string]int64 `json:"replica_slots"`
}

type PoolECStatus struct {
	Volume         uint32 `json:"volume_id"`
	Collection     string `json:"collection"`
	Layout         string `json:"layout"`
	State          string `json:"state"`
	Direction      string `json:"direction"`
	Conversion     string `json:"conversion_state"`
	Generation     string `json:"generation"`
	CleanupPending bool   `json:"cleanup_pending"`
	Operation      string `json:"operation_id"`
	Error          string `json:"error,omitempty"`
}
