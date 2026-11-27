package protocol

import "time"

const (
	PoolGiB                   = int64(1 << 30)
	PoolMiB                   = int64(1 << 20)
	PoolMinDataBytes          = 6 * PoolGiB
	PoolMinCapacityGiB        = 7
	PoolMinControlCapacityGiB = 12
	PoolReplication           = "010"
	PoolSeaweedImage          = "chrislusf/seaweedfs:4.55@sha256:7ca46aeae2f223b37e5d91593c17ab37738eec6df6ab69b48a0487f7643342b9"
	PoolEtcdImage             = "gcr.io/etcd-development/etcd:v3.6.16@sha256:b29d816b546a1966fe7034a33b34e231fa98c47ce4d2883f70b0555020b22132"
)

type PoolMemberInput struct {
	NodeID        string `json:"node_id"`
	PrivateIP     string `json:"private_ip"`
	CapacityGiB   int64  `json:"capacity_gib"`
	StorageRootID string `json:"storage_root_id,omitempty"`
	FailureDomain string `json:"failure_domain,omitempty"`
}

type PoolInput struct {
	Name       string            `json:"name"`
	Nodes      []PoolMemberInput `json:"nodes"`
	PortOffset int               `json:"port_offset"`
	ECEnabled  bool              `json:"ec_enabled"`
}

type PoolDisk struct {
	UUID           string `json:"uuid"`
	MountPoint     string `json:"mount_point"`
	CapacityBytes  int64  `json:"capacity_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	ReserveBytes   int64  `json:"reserve_bytes"`
	ArchiveBytes   int64  `json:"archive_bytes"`
}

type PoolPorts struct {
	S3         int `json:"s3"`
	S3GRPC     int `json:"s3_grpc"`
	Volume     int `json:"volume"`
	VolumeGRPC int `json:"volume_grpc"`
	Filer      int `json:"filer"`
	FilerGRPC  int `json:"filer_grpc"`
	Master     int `json:"master"`
	MasterGRPC int `json:"master_grpc"`
	EtcdClient int `json:"etcd_client"`
	EtcdPeer   int `json:"etcd_peer"`
}

type PoolNodePlan struct {
	PoolMemberInput
	Roles        []string  `json:"roles"`
	Ports        PoolPorts `json:"ports"`
	ControlDisk  PoolDisk  `json:"control_disk"`
	DataDisk     PoolDisk  `json:"data_disk"`
	DataBytes    int64     `json:"data_bytes"`
	ControlBytes int64     `json:"control_bytes"`
}

func (n PoolNodePlan) Control() bool {
	return len(n.Roles) == 5 && n.Roles[0] == "master" && n.Roles[1] == "etcd"
}

type PoolPlan struct {
	PlanID      string         `json:"plan_id"`
	PoolID      string         `json:"pool_id"`
	OwnerNodeID string         `json:"owner_node_id"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Normalized  PoolInput      `json:"normalized"`
	Nodes       []PoolNodePlan `json:"nodes"`
}

type PoolCredentials struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

type PoolCreate struct {
	PlanID      string          `json:"plan_id"`
	Credentials PoolCredentials `json:"credentials"`
}

type PoolMemberStatus struct {
	PoolNodePlan
	Endpoint         string    `json:"endpoint"`
	DataUsedBytes    int64     `json:"data_used_bytes"`
	ControlUsedBytes int64     `json:"control_used_bytes"`
	DataUUID         string    `json:"data_uuid,omitempty"`
	ControlUUID      string    `json:"control_uuid,omitempty"`
	State            string    `json:"state"`
	Error            string    `json:"error,omitempty"`
	CheckedAt        time.Time `json:"checked_at,omitzero"`
	Writable         bool      `json:"writable"`
	S3Ready          bool      `json:"s3_ready"`
	MasterLeader     string    `json:"master_leader,omitempty"`
	EtcdRead         bool      `json:"etcd_read"`
	CertExpiry       time.Time `json:"cert_expiry,omitzero"`
}

type StoragePool struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	OwnerNodeID string             `json:"owner_node_id"`
	Revision    uint64             `json:"revision"`
	State       string             `json:"state"`
	ECEnabled   bool               `json:"ec_enabled"`
	Replication string             `json:"replication"`
	OperationID string             `json:"operation_id"`
	CAPEM       string             `json:"ca_pem"`
	Nodes       []PoolMemberStatus `json:"nodes"`
	CheckedAt   time.Time          `json:"checked_at"`
}

type PoolList struct {
	Items       []StoragePool `json:"items"`
	NextCursor  string        `json:"next_cursor,omitempty"`
	Unavailable []string      `json:"unavailable"`
}

type PoolMaterial struct {
	CA       string `json:"ca"`
	Cert     string `json:"cert"`
	Key      string `json:"key"`
	PeerCert string `json:"peer_cert,omitempty"`
	PeerKey  string `json:"peer_key,omitempty"`
	CASeal   string `json:"ca_seal,omitempty"`
	FilerKey string `json:"filer_key"`
}

type PoolDeploy struct {
	PoolID      string          `json:"pool_id"`
	OperationID string          `json:"operation_id"`
	Member      PoolNodePlan    `json:"member"`
	Members     []PoolNodePlan  `json:"members"`
	Material    PoolMaterial    `json:"material"`
	Credentials PoolCredentials `json:"credentials"`
}

type PoolHostRequest struct {
	Action     string          `json:"action"`
	PoolID     string          `json:"pool_id,omitempty"`
	Member     PoolMemberInput `json:"member"`
	PortOffset int             `json:"port_offset,omitempty"`
	Deploy     *PoolDeploy     `json:"deploy,omitempty"`
}

type PoolHostResult struct {
	ControlDisk      PoolDisk         `json:"control_disk"`
	DataDisk         PoolDisk         `json:"data_disk"`
	HostID           string           `json:"host_id,omitempty"`
	PortsFree        bool             `json:"ports_free"`
	ControlPortsFree bool             `json:"control_ports_free"`
	Status           PoolMemberStatus `json:"status"`
	Phase            string           `json:"phase,omitempty"`
	TLSGeneration    string           `json:"tls_generation,omitempty"`
}

func (*PoolHostRequest) Kind() Kind { return "storage.pool.host" }
func (*PoolHostRequest) isPayload() {}
func (*PoolHostResult) Kind() Kind  { return "storage.pool.host.result" }
func (*PoolHostResult) isPayload()  {}
