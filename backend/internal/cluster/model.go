// Package cluster 管理对等节点、本机受理和地域存储，不执行宿主机命令。
package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Member struct {
	ID                    string `json:"id"`
	Cluster               string `json:"cluster"`
	Name                  string `json:"name"`
	Address               string `json:"address"`
	Region                string `json:"region"`
	Public                string `json:"public"`
	Signing               string `json:"signing"`
	Recipient             string `json:"recipient"`
	Version               uint64 `json:"version"`
	ConfigurationRevision uint64 `json:"configuration_revision,omitempty"`
	Issuer                string `json:"issuer"`
	Signature             []byte `json:"signature"`
	Storage               string `json:"storage,omitempty"`
}
type Revocation struct {
	ID        string `json:"id"`
	Issuer    string `json:"issuer"`
	Cluster   string `json:"cluster"`
	Signature []byte `json:"signature"`
}
type Directory struct {
	Databases   []ManagedDatabaseCluster `json:"databases,omitempty"`
	Admissions  []Member                 `json:"admissions"`
	Members     []Member                 `json:"members"`
	Revocations []Revocation             `json:"revocations"`
	Addresses   []AddressRecord          `json:"addresses,omitempty"`
	Trust       *TrustPolicy             `json:"trust,omitempty"`
}
type Config struct {
	Cached           bool          `json:"cached,omitempty"`
	LastSyncedAt     *time.Time    `json:"last_synced_at,omitempty"`
	Revision         uint64        `json:"revision"`
	Name             string        `json:"name"`
	Address          string        `json:"address"`
	AddressMode      string        `json:"address_mode,omitempty"`
	Region           string        `json:"region"`
	Listen           string        `json:"listen"`
	Console          bool          `json:"console"`
	API              bool          `json:"api"`
	ConsoleListen    string        `json:"console_listen"`
	PreferredStorage string        `json:"preferred_storage"`
	IntervalMinutes  int           `json:"interval_minutes"`
	RetentionDays    int           `json:"retention_days"`
	Storage          StorageConfig `json:"storage"`
}
type StorageConfig struct {
	NativeCluster bool   `json:"native_cluster"`
	Enabled       bool   `json:"enabled"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
}
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type Account struct {
	Cached       bool       `json:"cached,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	Revision     uint64     `json:"revision"`
	Username     string     `json:"username"`
	Entrance     string     `json:"entrance"`
	Secret       Secret     `json:"secret"`
}
type AccountChange struct {
	Revision int    `json:"revision,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Entrance string `json:"entrance,omitempty"`
}
type Secret struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Keys       []byte `json:"keys"`
}
type Record struct {
	ID            string    `json:"_id"`
	Cluster       string    `json:"cluster"`
	Owner         string    `json:"owner"`
	Sequence      uint64    `json:"sequence"`
	Kind          string    `json:"kind"`
	Object        string    `json:"object"`
	Created       time.Time `json:"created"`
	Data          []byte    `json:"data"`
	QueueSequence uint64    `json:"queue_sequence,omitempty"`
	Signature     []byte    `json:"signature"`
}
type Request struct {
	ExpectedRevision *uint64         `json:"expected_revision,omitempty"`
	InputDigest      string          `json:"input_digest,omitempty"`
	ResourceID       string          `json:"resource_id,omitempty"`
	Actor            string          `json:"actor,omitempty"`
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	Payload          json.RawMessage `json:"payload"`
	Key              string          `json:"key,omitempty"`
}
type Operation struct {
	ResourceID string `json:"resource_id,omitempty"`
	Key        string `json:"idempotency_key,omitempty"`
	protocol.OperationRecord
	NodeID            string    `json:"node_id"`
	Sequence          uint64    `json:"queue_sequence"`
	Subject           string    `json:"subject"`
	Actor             string    `json:"actor"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	PlanID            string    `json:"plan_id,omitempty"`
	Executor          string    `json:"executor"`
	ResultPendingSync bool      `json:"result_pending_sync"`
}
type Executor interface {
	Call(context.Context, protocol.Payload) (protocol.Payload, error)
	Storage(context.Context, StorageConfig) (Credentials, error)
}
type Plan struct {
	protocol.ChangePlan
	OperationID string `json:"operation_id,omitempty"`
}
type Fault struct{ Code, Message string }

func (e *Fault) Error() string         { return e.Message }
func fault(code, message string) error { return &Fault{code, message} }
func nodeID(public []byte) string      { s := sha256.Sum256(public); return hex.EncodeToString(s[:16]) }
func operationOwner(id string) string {
	if len(id) > 33 && id[32] == '_' {
		return id[:32]
	}
	return ""
}
func OperationOwner(id string) string    { return operationOwner(id) }
func NewOperationID(owner string) string { return owner + "_" + identity.Random() }
func validAddress(address string) bool {
	u, e := url.Parse(address)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func memberBytes(m Member) []byte         { m.Signature = nil; b, _ := json.Marshal(m); return b }
func revocationBytes(r Revocation) []byte { r.Signature = nil; b, _ := json.Marshal(r); return b }
func recordBytes(r Record) []byte         { r.Signature = nil; b, _ := json.Marshal(r); return b }
func validateMember(m Member) error {
	p, e := identity.Decode(m.Public)
	signing, e2 := identity.Decode(m.Signing)
	if e != nil || e2 != nil || nodeID(p) != m.ID || len(signing) != ed25519.PublicKeySize || m.Cluster == "" || m.Version == 0 || m.Name == "" || len(m.Name) > 255 || !validAddress(m.Address) {
		return fmt.Errorf("节点目录无效")
	}
	if m.Storage != "" && !validAddress(m.Storage) {
		return fmt.Errorf("数据库地址无效")
	}
	return nil
}
