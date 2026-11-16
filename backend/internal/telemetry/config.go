package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type archiveWorkflow struct {
	Previous      metricsstore.Config `json:"previous"`
	Desired       metricsstore.Config `json:"desired"`
	Stage         int                 `json:"stage"`
	Request       string              `json:"request,omitempty"`
	Child         string              `json:"child,omitempty"`
	ChildRevision uint64              `json:"child_revision,omitempty"`
}

func (s *Service) localConfig(ctx context.Context) (metricsstore.Config, error) {
	config := metricsstore.DefaultConfig(s.ClusterID())
	var data []byte
	err := s.DB.QueryRowContext(ctx, "SELECT body FROM monitoring_config WHERE id=1").Scan(&data)
	if err == sql.ErrNoRows {
		return config, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &config)
	}
	return config, err
}
func (s *Service) Config(ctx context.Context) (metricsstore.Config, error) {
	if store, err := s.Shared.Store(ctx); err == nil {
		o, err := store.Object(ctx, "monitoring", "config")
		if err == nil {
			var config metricsstore.Config
			if err = json.Unmarshal(o.Body, &config); err != nil {
				return config, err
			}
			if config.State == "ready" || config.State == "disabled" {
				err = s.CacheConfig(ctx, config)
			}
			return config, err
		}
		if err != sql.ErrNoRows {
			return s.localConfig(ctx)
		}
	}
	return s.localConfig(ctx)
}
func (s *Service) CacheConfig(ctx context.Context, c metricsstore.Config) error {
	data, _ := json.Marshal(c)
	_, err := s.DB.ExecContext(ctx, "INSERT INTO monitoring_config(id,revision,body) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,body=excluded.body WHERE monitoring_config.revision<=excluded.revision", c.Revision, string(data))
	return err
}
func (s *Service) ValidateRequest(ctx context.Context, body []byte) error {
	var c metricsstore.Config
	if err := json.Unmarshal(body, &c); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return s.Errors.New("invalid_request", err.Error())
	}
	old, err := s.Config(ctx)
	if err != nil {
		return err
	}
	if old.Revision != c.Revision {
		return s.Errors.New("precondition_failed", "监控配置已经改变")
	}
	if old.State == "applying" {
		return s.Errors.New("node_busy", "监控仍有未完成的配置操作")
	}
	if c.Enabled && c.NodeID != s.NodeID || !c.Enabled && old.NodeID != "" && old.NodeID != s.NodeID {
		return s.Errors.New("invalid_request", "请向监控配置的目标节点提交")
	}
	if !s.Peer.Active(ctx, s.NodeID) {
		return s.Errors.New("forbidden", "监控节点已撤销")
	}
	if err = s.Shared.WriteAllowed(ctx); err != nil {
		return err
	}
	if c.Enabled {
		t, err := s.Storage.Target(ctx, c.StorageTargetID)
		if err != nil {
			return err
		}
		if t.NodeID != s.NodeID || !t.Enabled {
			return s.Errors.New("conflict", "S3 存储目标须在监控节点启用")
		}
		if old.StorageTargetID != "" {
			before, err := s.Storage.Target(ctx, old.StorageTargetID)
			if err != nil {
				return err
			}
			if before.Endpoint != t.Endpoint || before.Bucket != t.Bucket || old.Prefix != c.Prefix {
				return s.Errors.New("conflict", "归档位置变化需要先迁移已有数据；请沿用原 Endpoint、Bucket 和目录")
			}
		}
	}
	return nil
}

func (s *Service) persistWorkflow(ctx context.Context, parent Parent, flow archiveWorkflow) error {
	op := parent.Record()
	data, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO monitoring_workflows(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", op.ID, string(data)); err != nil {
		return err
	}
	op.Revision++
	return parent.Save(ctx, "submitted")
}
func (s *Service) loadWorkflow(ctx context.Context, id string) (archiveWorkflow, error) {
	var flow archiveWorkflow
	var data []byte
	err := s.DB.QueryRowContext(ctx, "SELECT body FROM monitoring_workflows WHERE id=?", id).Scan(&data)
	if err == nil {
		err = json.Unmarshal(data, &flow)
	}
	return flow, err
}

func (s *Service) Advance(ctx context.Context, parent Parent, body []byte) error {
	op := parent.Record()
	s.configMu.Lock()
	defer s.configMu.Unlock()
	flow, err := s.loadWorkflow(ctx, op.ID)
	attention := func(err error) error {
		op.State = protocol.AttentionRequired
		op.Error = &protocol.Error{Code: "dependency_unavailable", Message: err.Error()}
		return s.persistWorkflow(ctx, parent, flow)
	}
	if err == sql.ErrNoRows {
		if err = json.Unmarshal(body, &flow.Desired); err != nil {
			return err
		}
		flow.Previous, err = s.Config(ctx)
		if err != nil {
			return err
		}
		if flow.Previous.Revision != flow.Desired.Revision {
			return s.Errors.New("precondition_failed", "监控配置已经改变")
		}
		flow.Desired.Revision++
		flow.Desired.OperationID = op.ID
		flow.Desired.State = "applying"
		op.State = protocol.Applying
		if err = s.persistWorkflow(ctx, parent, flow); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if op.State == protocol.AttentionRequired && flow.Request == "" {
		return nil
	}
	store, err := s.Shared.Store(ctx)
	if err != nil {
		return attention(err)
	}
	if flow.Stage == 0 {
		if flow.Desired.Enabled {
			if _, _, err = s.Storage.Checked(ctx, flow.Desired.StorageTargetID, flow.Desired.Prefix); err != nil {
				return attention(err)
			}
		}
		err = store.Transact(ctx, s.NodeID, false, func(tx *sharedsql.Tx) error {
			old, e := tx.Object(ctx, "monitoring", "config")
			if e != nil && e != sql.ErrNoRows {
				return e
			}
			if e == nil {
				var c metricsstore.Config
				if e = json.Unmarshal(old.Body, &c); e != nil {
					return e
				}
				if c.OperationID == op.ID {
					return nil
				}
			}
			if old.Revision != flow.Previous.Revision {
				return s.Errors.New("precondition_failed", "监控配置已经改变")
			}
			if flow.Desired.Enabled {
				target, e := tx.Object(ctx, "storage_target", flow.Desired.StorageTargetID)
				if e != nil {
					return e
				}
				var t struct {
					Enabled bool   `json:"enabled"`
					NodeID  string `json:"node_id"`
				}
				if e = json.Unmarshal(target.Body, &t); e != nil {
					return e
				}
				if !t.Enabled || t.NodeID != s.NodeID {
					return s.Errors.New("conflict", "存储目标已停用或归属不同")
				}
			}
			return tx.Put(ctx, sharedObject("monitoring", "config", flow.Desired.Revision, flow.Desired), flow.Previous.Revision)
		})
		if err != nil {
			return attention(s.Errors.Shared(err))
		}
		flow.Stage = 1
		flow.Request = ""
		op.State = protocol.Applying
		op.Error = nil
		return s.persistWorkflow(ctx, parent, flow)
	}
	if flow.Stage == 1 || flow.Stage == 2 {
		config := flow.Previous
		enabled := false
		if flow.Stage == 2 {
			config = flow.Desired
			enabled = config.Enabled
		}
		if config.NodeID == "" || flow.Stage == 2 && !enabled {
			flow.Stage++
			flow.Child = ""
			flow.ChildRevision = 0
			return s.persistWorkflow(ctx, parent, flow)
		}
		d := protocol.MonitoringDeployment{ClusterID: s.ClusterID(), NodeID: config.NodeID, Enabled: enabled, Directory: config.Directory, Prefix: config.Prefix, DiskBudgetBytes: config.DiskBudgetBytes}
		if flow.Child == "" {
			flow.Child = s.Queue.StepID(config.NodeID, fmt.Sprintf("%s/monitoring/%d", op.ID, flow.Stage))
			if err = s.persistWorkflow(ctx, parent, flow); err != nil {
				return err
			}
		}
		child, err := s.Queue.Get(ctx, flow.Child)
		if errors.Is(err, sql.ErrNoRows) || s.Errors.Code(err) == "not_found" {
			if enabled {
				t, keys, e := s.Storage.Checked(ctx, config.StorageTargetID, config.Prefix)
				if e != nil {
					return attention(e)
				}
				d.Storage, d.Credentials = t.S3Connection, keys
			}
			payload, _ := json.Marshal(d)
			child, err = s.Queue.Submit(ctx, config.NodeID, flow.Child, parent.Actor(), payload)
		}
		if err != nil {
			return attention(err)
		}
		if child.State == protocol.Succeeded {
			flow.Stage++
			flow.Child = ""
			flow.ChildRevision = 0
			flow.Request = ""
			op.State = protocol.Applying
			op.Error = nil
			return s.persistWorkflow(ctx, parent, flow)
		}
		if flow.Request == "recover" && child.State == protocol.AttentionRequired {
			flow.ChildRevision = child.Revision
			flow.Request = "recovering"
			if err = s.persistWorkflow(ctx, parent, flow); err != nil {
				return err
			}
			err = s.Queue.Decide(ctx, "operation.recover", child.ID, child.Revision)
			if err != nil {
				return attention(err)
			}
			return nil
		}
		if child.State == protocol.AttentionRequired && flow.Request == "recovering" && child.Revision <= flow.ChildRevision {
			return nil
		}
		if child.State == protocol.AttentionRequired || operations.Terminal(child.State) {
			flow.Request = ""
			message := "监控组件操作需要处理"
			if child.Error != nil {
				message = child.Error.Message
			}
			return attention(fmt.Errorf("%s", message))
		}
		if flow.ChildRevision != child.Revision {
			flow.ChildRevision = child.Revision
			flow.Request = ""
			op.State = protocol.Applying
			op.Error = nil
			op.Steps = child.Steps
			return s.persistWorkflow(ctx, parent, flow)
		}
		return nil
	}
	flow.Desired.State = "disabled"
	if flow.Desired.Enabled {
		flow.Desired.State = "ready"
	}
	err = store.Transact(ctx, s.NodeID, false, func(tx *sharedsql.Tx) error {
		o, e := tx.Object(ctx, "monitoring", "config")
		if e != nil {
			return e
		}
		var c metricsstore.Config
		if e = json.Unmarshal(o.Body, &c); e != nil {
			return e
		}
		if c.OperationID != op.ID {
			return s.Errors.New("conflict", "监控配置已被其他操作替换")
		}
		if c.State == flow.Desired.State {
			flow.Desired = c
			return nil
		}
		flow.Desired.Revision = o.Revision + 1
		return tx.Put(ctx, sharedObject("monitoring", "config", flow.Desired.Revision, flow.Desired), o.Revision)
	})
	if err != nil {
		return attention(err)
	}
	if err = s.CacheConfig(ctx, flow.Desired); err != nil {
		return attention(err)
	}
	op.State = protocol.Succeeded
	op.Error = nil
	op.Revision++
	s.Changed()
	return parent.Save(ctx, "finished")
}

func (s *Service) Decide(ctx context.Context, method string, revision uint64, parent Parent) error {
	op := parent.Record()
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if op.State != protocol.AttentionRequired || op.Revision != revision {
		return s.Errors.New("conflict", "监控操作阶段已经改变")
	}
	flow, err := s.loadWorkflow(ctx, op.ID)
	if err != nil {
		return err
	}
	if method != "operation.recover" {
		return s.Errors.New("conflict", "请先完成原监控操作，再停用或调整监控角色")
	}
	flow.Request = "recover"
	op.State = protocol.Applying
	op.Error = nil
	err = s.persistWorkflow(ctx, parent, flow)
	return err
}

func (s *Service) Deployment(ctx context.Context, c metricsstore.Config) (protocol.MonitoringDeployment, error) {
	d := protocol.MonitoringDeployment{ClusterID: s.ClusterID(), NodeID: c.NodeID, Enabled: c.Enabled, Directory: c.Directory, Prefix: c.Prefix, DiskBudgetBytes: c.DiskBudgetBytes}
	if !c.Enabled {
		return d, nil
	}
	t, err := s.Storage.LocalTarget(ctx, c.StorageTargetID)
	if err != nil {
		return d, err
	}
	d.Storage = t.S3Connection
	d.Credentials, err = s.Storage.Credentials(ctx, t)
	return d, err
}

func (s *Service) maintenanceLoop(ctxParent context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.Refresh(ctxParent)
		select {
		case <-ctxParent.Done():
			return
		case <-ticker.C:
		}
	}
}

func sharedObject(kind, id string, revision uint64, value any) sharedsql.Object {
	body, _ := json.Marshal(value)
	return sharedsql.Object{Kind: kind, ID: id, Revision: revision, Body: body}
}

func (s *Service) Refresh(ctxParent context.Context) {
	sharedCtx, sharedCancel := context.WithTimeout(ctxParent, time.Second)
	_, _ = s.Config(sharedCtx)
	sharedCancel()
	ctx, cancel := context.WithTimeout(ctxParent, 4*time.Second)
	c, err := s.localConfig(ctx)
	if err == nil {
		if c.Enabled {
			if buffer, e := s.Buffer(ctx); e == nil {
				_ = buffer.Cleanup(ctx, s.Now())
			}
		}
		s.mu.Lock()
		s.runtime.Config = c
		s.mu.Unlock()
		if c.Enabled && c.NodeID == s.NodeID {
			d, e := s.Deployment(ctx, c)
			var state protocol.MonitoringState
			if e == nil {
				var reply protocol.Payload
				reply, e = s.Executor(ctx, &protocol.MonitoringStateRead{Deployment: d})
				if e == nil {
					if value, ok := reply.(*protocol.MonitoringState); ok {
						state = *value
					} else {
						e = fmt.Errorf("监控组件状态应答无效")
					}
				}
			}
			if e == nil {
				if buffer, bufferErr := s.Buffer(ctx); bufferErr == nil {
					size, sizeErr := buffer.DiskBytes(ctx)
					if sizeErr == nil {
						state.DiskUsedBytes += size
					}
				}
			}
			s.mu.Lock()
			if e != nil {
				s.runtime.ReceiveError = e.Error()
				s.runtime.Paused = true
			} else {
				s.runtime.Deployment = state
				s.runtime.ReceiveError = ""
				s.runtime.Paused = metricsstore.DiskPaused(s.runtime.Paused, state.DiskUsedBytes, c.DiskBudgetBytes, state.DiskAvailableBytes)
			}
			s.mu.Unlock()
		}
	}
	cancel()
}
