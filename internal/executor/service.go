package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Services struct {
	Metrics       monitoring.Collector
	Engine        *operations.Engine
	Start         func(context.Context, string) error
	Restart       func(context.Context, string) error
	OperationLock func() (func(), error)
	localMu       sync.Mutex
	trustMu       sync.Mutex
}

func NewServices(db *sql.DB) *Services {
	m := firewall.New()
	engine := &operations.Engine{Store: operations.Store{DB: db}, Manager: m}
	engine.Install = func(ctx context.Context, l *operations.Local, persist func() error) error {
		if l.ComponentState == nil {
			l.ComponentState = &firewall.InstallState{Backend: l.Action.Component.Backend}
		}
		return m.Install(ctx, l.ComponentState, func() error {
			names := map[string]string{"prepared": "准备安装", "mask_intent": "屏蔽目标服务启动", "masked": "目标服务已屏蔽", "installing": "安装软件包", "disabling": "停用目标服务开机启动", "unmasking": "解除本次服务屏蔽", "complete": "组件安装完成"}
			state := "applying"
			if l.ComponentState.Stage == "complete" {
				state = "completed"
			}
			l.Record.Steps = []protocol.OperationStep{{Name: names[l.ComponentState.Stage], State: state}}
			return persist()
		})
	}
	return &Services{Engine: engine, Start: func(ctx context.Context, id string) error {
		_, e := m.Host.Run(ctx, "systemctl", "start", "--no-block", "opsd-operation@"+id+".service")
		return e
	}, Restart: func(ctx context.Context, id string) error {
		_, e := m.Host.Run(ctx, "systemctl", "restart", "--no-block", "opsd-operation@"+id+".service")
		return e
	}}
}
func (s *Services) Handle(ctx context.Context, payload protocol.Payload) (protocol.Payload, error) {
	switch body := payload.(type) {
	case *protocol.PeerTrustTarget:
		return s.receiveTrust(ctx, *body)
	case *protocol.PeerTrustStatusQuery:
		s.trustMu.Lock()
		defer s.trustMu.Unlock()
		_, status, e := s.trustState(ctx)
		return &status, e
	case *protocol.FirewallInspect:
		unlock, e := s.lock()
		if e != nil {
			return nil, e
		}
		defer unlock()
		return s.Engine.InspectFirewall(ctx, body.Import)
	case *protocol.MonitoringRead:
		if body.Stream != "live" && body.Stream != "history" {
			return nil, firewall.Unsupported("未知采集类型")
		}
		metrics := s.Metrics.Collect(body.Stream)
		return &protocol.MonitoringResult{Metrics: metrics, Firewall: "unknown"}, nil
	case *protocol.FirewallRead:
		state, e := s.Engine.Manager.Read(ctx)
		if e != nil {
			return nil, e
		}
		if body.Family != "" || body.Scope != "" {
			rules := []protocol.FirewallRule{}
			for _, r := range state.Rules {
				if (body.Scope == "" || body.Scope == protocol.Both || r.Scope == body.Scope) && (body.Family == "" || r.Spec == nil || r.Spec.Family == body.Family) {
					rules = append(rules, r)
				}
			}
			state.Rules = rules
			policies := []protocol.DefaultPolicy{}
			for _, p := range state.DefaultPolicies {
				if (body.Scope == "" || body.Scope == protocol.Both || p.Scope == body.Scope) && (body.Family == "" || p.Family == body.Family) {
					policies = append(policies, p)
				}
			}
			state.DefaultPolicies = policies
		}
		return &protocol.FirewallReadResult{State: state}, nil
	case *protocol.FirewallPreview:
		p, e := s.Engine.Manager.Preview(ctx, body.Change)
		return &protocol.FirewallPreviewResult{Plan: p}, e
	case *protocol.OperationSubmit:
		unlock, lockErr := s.lock()
		if lockErr != nil {
			return nil, lockErr
		}
		boot, _, e := s.clock()
		if e != nil {
			unlock()
			return nil, e
		}
		l, created, e := s.Engine.Store.Receive(ctx, body.OperationID, body.ContractVersion, body.Action, boot)
		unlock()
		if e != nil {
			return nil, e
		}
		if created {
			if e = s.Start(context.WithoutCancel(ctx), body.OperationID); e != nil {
				return &protocol.OperationUpdated{Record: l.Record}, nil
			}
		}
		r, e := s.Engine.Record(ctx, body.OperationID)
		return &protocol.OperationUpdated{Record: r}, e
	case *protocol.OperationQuery:
		r, e := s.Engine.Record(ctx, body.OperationID)
		if errors.Is(e, sql.ErrNoRows) {
			return &protocol.OperationQueryResult{OperationID: body.OperationID, Found: false}, nil
		}
		if e != nil {
			return nil, e
		}
		return &protocol.OperationQueryResult{OperationID: body.OperationID, Found: true, Record: &r}, nil
	case *protocol.OperationReconcile:
		l, e := s.Engine.Store.Reconcile(ctx, body.OperationID)
		if e != nil {
			return nil, e
		}
		r, e := s.Engine.Record(ctx, l.Record.ID)
		return &protocol.OperationReconcileResult{Record: r}, e
	case *protocol.OperationConfirm:
		return s.request(ctx, body.OperationID, "confirm", body.ExpectedRevision)
	case *protocol.OperationRecover:
		return s.request(ctx, body.OperationID, "recover", body.ExpectedRevision)
	case *protocol.OperationResolve:
		if !body.AcceptCurrent {
			return nil, firewall.Unsupported("必须明确接受现状")
		}
		return s.request(ctx, body.OperationID, "resolve", body.ExpectedRevision)
	}
	return nil, firewall.Unsupported("当前版本不执行此消息")
}
func (s *Services) clock() (string, int64, error) {
	if s.Engine.Clock != nil {
		return s.Engine.Clock()
	}
	return operations.SystemClock()
}
func (s *Services) request(ctx context.Context, id, kind string, revision uint64) (protocol.Payload, error) {
	l, e := s.Engine.Store.Request(ctx, id, kind, revision)
	if e != nil {
		return nil, e
	}
	if !operations.Terminal(l.Record.State) {
		start := s.Start
		// 人工处理可能恰好落在旧进程退出前；重启任务确保新进程读取已持久请求。
		if kind != "confirm" && s.Restart != nil {
			start = s.Restart
		}
		if e = start(context.WithoutCancel(ctx), id); e != nil {
			return nil, e
		}
	}
	r, e := s.Engine.Record(ctx, id)
	return &protocol.OperationUpdated{Record: r}, e
}
func (s *Services) Recover(ctx context.Context) error {
	ids, e := s.Engine.Store.Pending(ctx)
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = s.Start(ctx, id); e != nil {
			return fmt.Errorf("恢复操作 %s：%w", id, e)
		}
	}
	return nil
}
