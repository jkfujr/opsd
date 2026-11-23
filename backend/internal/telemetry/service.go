// Package telemetry 管理监控配置、角色流程及样本链路。
package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type Stream interface {
	Send(context.Context, protocol.Message) error
	Receive(context.Context) (protocol.Message, error)
	Close()
}

type Peer struct {
	Call       func(context.Context, string, string, any, any) error
	OpenStream func(context.Context, string) (Stream, error)
	Active     func(context.Context, string) bool
	Revoked    func(context.Context, string) bool
	// Allowed 判定某个来源能否执行监控通道上的某个方法。
	Allowed func(origin, method string) bool
}

// Parent 保留队列原对象，由所属节点持久保存完整操作。
type Parent interface {
	Record() *protocol.OperationRecord
	Actor() string
	Save(context.Context, string) error
}

type Queue struct {
	Submit func(context.Context, string, string, string, json.RawMessage) (protocol.OperationRecord, error)
	Get    func(context.Context, string) (protocol.OperationRecord, error)
	Decide func(context.Context, string, string, uint64) error
	StepID func(string, string) string
}

type Storage struct {
	Target      func(context.Context, string) (objectstore.Target, error)
	Checked     func(context.Context, string, string) (objectstore.Target, protocol.S3Credentials, error)
	LocalTarget func(context.Context, string) (objectstore.Target, error)
	Credentials func(context.Context, objectstore.Target) (protocol.S3Credentials, error)
}

type Shared struct {
	Store        func(context.Context) (*sharedsql.Store, error)
	WriteAllowed func(context.Context) error
}

type Errors struct {
	New    func(string, string) error
	Code   func(error) string
	Shared func(error) error
	Wire   func(error) *protocol.Error
}

type Source struct {
	Host      func(context.Context, string) (protocol.MonitoringResult, error)
	Resources func(context.Context) (protocol.ResourceMetrics, error)
}

type Dependencies struct {
	DB        *sql.DB
	NodeID    string
	ClusterID func() string
	Now       func() time.Time
	Peer      Peer
	Queue     Queue
	Storage   Storage
	Shared    Shared
	Errors    Errors
	Source    Source
	Executor  func(context.Context, protocol.Payload) (protocol.Payload, error)
	Changed   func()
}

type Service struct {
	Dependencies
	configMu   sync.Mutex
	mu         sync.Mutex
	buffer     *metricsstore.Buffer
	directory  string
	streams    map[string]bool
	writeSlots chan struct{}
	runtime    runtime
	querySlots chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	stopParent func() bool
	wg         sync.WaitGroup
	started    bool
	closed     bool
}

func New(directory string, deps Dependencies) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Dependencies: deps, directory: directory, streams: map[string]bool{}, writeSlots: make(chan struct{}, 4), querySlots: make(chan struct{}, 2), ctx: ctx, cancel: cancel}
}

func (s *Service) LockConfig()   { s.configMu.Lock() }
func (s *Service) UnlockConfig() { s.configMu.Unlock() }

func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stopParent = context.AfterFunc(ctx, s.cancel)
	s.wg.Add(3)
	s.mu.Unlock()
	go func() { defer s.wg.Done(); s.sampleLoop() }()
	go func() { defer s.wg.Done(); s.uploadLoop() }()
	go func() { defer s.wg.Done(); s.maintenanceLoop(s.ctx) }()
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	if s.stopParent != nil {
		s.stopParent()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffer != nil {
		s.buffer.DB.Close()
		s.buffer = nil
	}
}
