package executor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/terminal"
)

type Services struct {
	Terminals     terminal.Manager
	Metrics       monitoring.Collector
	Engine        *operations.Engine
	Docker        *dockerengine.Manager
	Start         func(context.Context, string) error
	Restart       func(context.Context, string) error
	TaskLock      func(string) (func(), error)
	OperationLock func() (func(), error)
	ResourceLock  func(protocol.OperationAction) (func(), error)
	localMu       sync.Mutex
	trustMu       sync.Mutex
}

func NewServices(db *sql.DB) *Services {
	m := firewall.New()
	docker := dockerengine.New()
	engine := &operations.Engine{Store: operations.Store{DB: db}, Manager: m, Containers: docker, Databases: databaseengine.Manager{Docker: docker}}
	engine.Install = func(ctx context.Context, l *operations.Local, persist func() error) error {
		if l.ComponentState == nil {
			l.ComponentState = &firewall.InstallState{Backend: l.Action.Component.Backend}
		}
		save := func() error {
			names := map[string]string{"prepared": "准备安装", "mask_intent": "屏蔽目标服务启动", "masked": "目标服务已屏蔽", "installing": "安装软件包", "disabling": "停用目标服务开机启动", "unmasking": "解除本次服务屏蔽", "complete": "组件安装完成", "nft_enable_prepared": "准备启用 nftables", "nft_enabled": "nftables 已启用"}
			state := "applying"
			if l.ComponentState.Stage == "complete" || l.ComponentState.Stage == "nft_enabled" {
				state = "completed"
			}
			if l.Action.Component.Enable && l.ComponentState.Stage == "complete" {
				names["complete"] = "nftables 已在运行"
			}
			l.Record.Steps = []protocol.OperationStep{{Name: names[l.ComponentState.Stage], State: state}}
			return persist()
		}
		if l.Action.Component.Enable {
			return m.EnableNFT(ctx, l.ComponentState, save)
		}
		return m.Install(ctx, l.ComponentState, save)
	}
	return &Services{Engine: engine, Docker: docker, Start: func(ctx context.Context, id string) error {
		_, e := m.Host.Run(ctx, "systemctl", "start", "--no-block", "opsd-operation@"+id+".service")
		return e
	}, Restart: func(ctx context.Context, id string) error {
		_, e := m.Host.Run(ctx, "systemctl", "restart", "--no-block", "opsd-operation@"+id+".service")
		return e
	}}
}
func (s *Services) Handle(ctx context.Context, payload protocol.Payload) (protocol.Payload, error) {
	switch body := payload.(type) {
	case *protocol.MonitoringStateRead:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		state, err := s.Docker.MonitoringState(ctx, body.Deployment)
		return &state, err
	case *protocol.ResourceMetricsRead:
		return s.resourceMetrics(ctx, *body)
	case *protocol.ComposeRead:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		projects, e := s.Docker.Projects(ctx)
		return &protocol.ComposeReadResult{Projects: projects}, e
	case *protocol.OperationCancel:
		return s.request(ctx, body.OperationID, "cancel", body.ExpectedRevision)
	case *protocol.ExecutionLogsRead:
		logs, e := s.Engine.ExecutionLogs(ctx, *body)
		return &protocol.ExecutionLogsResult{Logs: logs}, e
	case *protocol.DatabaseDiscovery:
		return s.discoverDatabases(ctx)
	case *protocol.DatabaseDeploymentCheck:
		value, err := (databaseengine.Manager{Docker: s.Docker}).CheckDeployment(ctx, body.Deployment)
		return &value, err
	case *protocol.DatabaseDeploymentStateRead:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		id, state, err := s.Docker.DeploymentState(ctx, body.Deployment)
		return &protocol.DatabaseDeploymentState{ContainerID: id, State: state}, err
	case *protocol.DatabaseNetworkCheck:
		err := databaseengine.CheckDeploymentNetwork(ctx, *body)
		return &protocol.DatabaseNetworkChecked{Checked: err == nil}, err
	case *protocol.DatabaseDeploymentProfileRead:
		value, err := (databaseengine.Manager{Docker: s.Docker}).DeploymentProfile(ctx, *body)
		return &value, err
	case *protocol.DatabaseRead:
		return s.readDatabase(ctx, *body)
	case *protocol.DatabaseExists:
		exists, err := (databaseengine.Manager{}).DatabaseExists(ctx, body.Connection, body.ClusterID, body.Database)
		return &protocol.DatabaseExistsResult{Exists: exists}, err
	case *protocol.ProxyRoutingRead:
		value, err := (databaseengine.Manager{}).ReadProxyRouting(ctx, body.Connection, body.Hostgroups)
		return &protocol.ProxyRoutingReadResult{Routing: value}, err
	case *protocol.DockerRead:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		return &protocol.DockerReadResult{State: s.Docker.List(ctx)}, nil
	case *protocol.ContainerInspect:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		result, err := s.Docker.Inspect(ctx, body.ContainerRef)
		return &protocol.ContainerInspectResult{Container: result}, err
	case *protocol.ContainerLogsRead:
		if s.Docker == nil {
			return nil, fmt.Errorf("Docker 读取入口不可用")
		}
		result, err := s.Docker.Logs(ctx, *body)
		return &protocol.ContainerLogsResult{Logs: result}, err
	case *protocol.PeerTrustTarget:
		return s.receiveTrust(ctx, *body)
	case *protocol.PeerTrustStatusQuery:
		s.trustMu.Lock()
		defer s.trustMu.Unlock()
		_, status, e := s.trustState(ctx)
		return &status, e
	case *protocol.FirewallInspect:
		unlock, e := s.inspectionLock(ctx)
		if e != nil {
			return nil, e
		}
		defer unlock()
		return s.Engine.InspectFirewall(ctx, body.Import, body.PreviewCurrent)
	case *protocol.MonitoringRead:
		if body.Stream != "live" && body.Stream != "history" && body.Stream != "archive" {
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
		if action := body.Action.Container; action != nil && (action.ProjectKnown || action.Project != "") {
			if _, err := s.Engine.Store.Get(ctx, body.OperationID); errors.Is(err, sql.ErrNoRows) {
				if s.Docker == nil {
					return nil, fmt.Errorf("Docker 读取入口不可用")
				}
				current, err := s.Docker.Inspect(ctx, action.ContainerRef)
				if err != nil {
					return nil, err
				}
				if current.Project != action.Project {
					return nil, firewall.Conflict("容器所属项目已经变化")
				}
			} else if err != nil {
				return nil, err
			}
		}
		var unlock func()
		var lockErr error
		if s.ResourceLock != nil {
			unlock, lockErr = s.ResourceLock(body.Action)
		} else if body.Action.Shell != nil && s.TaskLock != nil {
			unlock, lockErr = s.TaskLock(body.Action.Shell.TaskID)
		} else {
			unlock, lockErr = s.lock()
		}
		if errors.Is(lockErr, localstate.ErrBusy) {
			return nil, &firewall.Fault{Code: "not_accepted_busy", Message: lockErr.Error()}
		}
		if lockErr != nil {
			return nil, lockErr
		}
		boot, _, e := s.clock()
		if e != nil {
			unlock()
			return nil, e
		}
		local, created, e := s.Engine.Store.Receive(ctx, body.OperationID, body.ContractVersion, body.Action, boot)
		unlock()
		if errors.Is(e, operations.ErrBusy) {
			return nil, &firewall.Fault{Code: "not_accepted_busy", Message: e.Error()}
		}
		if e != nil {
			return nil, e
		}
		if created {
			_ = s.startOperation(context.WithoutCancel(ctx), local.Record, false)
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
		var startError string
		if e = s.Engine.Store.DB.QueryRowContext(ctx, "SELECT CASE WHEN terminal=0 AND json_extract(record,'$.revision')=? THEN start_error ELSE '' END FROM operations WHERE id=?", r.Revision, body.OperationID).Scan(&startError); e != nil {
			return nil, e
		}
		if startError != "" {
			r.Error = &protocol.Error{Code: "operation_start_failed", Message: startError}
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

func (s *Services) inspectionLock(ctx context.Context) (func(), error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		unlock, err := s.lock()
		if !errors.Is(err, localstate.ErrBusy) {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, &firewall.Fault{Code: "node_busy", Message: "本机操作执行中，请稍后读取"}
		case <-ticker.C:
		}
	}
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

const operationStartTimeout = 45 * time.Second

type preparedRevision struct {
	id       string
	revision uint64
}

func (s *Services) Recover(ctx context.Context) error {
	_, err := s.recoverOperations(ctx, nil, time.Now)
	return err
}

func (s *Services) recoverOperations(ctx context.Context, previous map[preparedRevision]time.Time, now func() time.Time) (map[preparedRevision]time.Time, error) {
	ids, e := s.Engine.Store.Pending(ctx)
	if e != nil {
		return previous, e
	}
	current := make(map[preparedRevision]time.Time)
	var failures []error
	for _, id := range ids {
		// 待人工核对的操作不能被无条件重派：它等待的是人，不是重试。
		// 否则维护循环每 5 秒重新拉起它，既空转，也可能在无人确认时反复执行。
		//
		// 但已经持久化、且版本仍匹配的人工请求（recover／resolve／cancel）
		// 必须继续可推进：首次启动失败或执行器重启后，它是唯一的恢复入口。
		local, err := s.Engine.Store.Get(ctx, id)
		if err != nil {
			failures = append(failures, fmt.Errorf("恢复操作 %s：%w", id, err))
			continue
		}
		if local.Record.State == protocol.AttentionRequired && !manualRequest(local) {
			continue
		}
		stalled := false
		if local.Record.State == protocol.Prepared {
			key := preparedRevision{id, local.Record.Revision}
			at := now()
			since, ok := previous[key]
			if !ok {
				since = at
			}
			current[key] = since
			stalled = at.Sub(since) >= operationStartTimeout
		}
		if e = s.startOperation(ctx, local.Record, stalled); e != nil {
			failures = append(failures, fmt.Errorf("恢复操作 %s：%w", id, e))
		}
	}
	return current, errors.Join(failures...)
}

func (s *Services) startOperation(ctx context.Context, record protocol.OperationRecord, stalled bool) error {
	err := s.Start(ctx, record.ID)
	if err == nil && !stalled {
		return nil
	}
	var message string
	if err != nil {
		message = err.Error()
	} else {
		message = fmt.Sprintf("操作服务 opsd-operation@%s.service 停在 prepared，修订号 %d 已有 %v 未推进", record.ID, record.Revision, operationStartTimeout)
	}
	// 执行进程可能已经推进。超时提示也应保留同一修订上更具体的启动错误。
	_, saveErr := s.Engine.Store.DB.ExecContext(ctx, `UPDATE operations SET start_error=?
		WHERE id=? AND terminal=0 AND json_extract(record,'$.revision')=? AND (start_error='' OR ?)`,
		message, record.ID, record.Revision, err != nil)
	return errors.Join(err, saveErr)
}

func (s *Services) MaintainOperations(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var previous map[preparedRevision]time.Time
	for {
		previous, _ = s.recoverOperations(ctx, previous, time.Now)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// manualRequest 报告记录上是否有仍然有效的人工请求。
// 请求版本与记录当前版本不一致时视为过期，不能自动推进。
func manualRequest(l operations.Local) bool {
	switch l.Request {
	case "recover", "resolve", "cancel":
		return l.RequestRevision == l.Record.Revision
	}
	return false
}
