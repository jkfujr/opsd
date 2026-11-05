// Package operations 管理 Agent 本机的持久执行与恢复，不依赖通信连接。
package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

var ErrBusy = errors.New("节点存在未结束的写操作")
var ErrExpired = errors.New("确认期限已到")
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidID(id string) bool { return validID.MatchString(id) }

type Local struct {
	Action          *protocol.OperationAction
	Record          protocol.OperationRecord
	Recipe          []firewall.Step
	ComponentState  *firewall.InstallState
	BootID          string
	DeadlineNS      int64
	NextStep        int
	IntentStep      *int
	Request         string
	RequestRevision uint64
}
type Store struct{ DB *sql.DB }
type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Terminal(state protocol.OperationState) bool {
	switch state {
	case protocol.Succeeded, protocol.Rejected, protocol.Failed, protocol.Reverted, protocol.ResolvedManually:
		return true
	}
	return false
}
func (s Store) Get(ctx context.Context, id string) (Local, error) { return load(ctx, s.DB, id) }
func load(ctx context.Context, q querier, id string) (Local, error) {
	var l Local
	var action, recipe, component sql.NullString
	var record string
	var intent sql.NullInt64
	err := q.QueryRowContext(ctx, "SELECT action,record,recipe,component_state,boot_id,deadline_ns,next_step,intent_step,request,request_revision FROM operations WHERE id=?", id).Scan(&action, &record, &recipe, &component, &l.BootID, &l.DeadlineNS, &l.NextStep, &intent, &l.Request, &l.RequestRevision)
	if err != nil {
		return l, err
	}
	if err = json.Unmarshal([]byte(record), &l.Record); err != nil {
		return l, err
	}
	if action.Valid {
		if err = json.Unmarshal([]byte(action.String), &l.Action); err != nil {
			return l, err
		}
	}
	if recipe.Valid {
		if err = json.Unmarshal([]byte(recipe.String), &l.Recipe); err != nil {
			return l, err
		}
	}
	if component.Valid {
		if err = json.Unmarshal([]byte(component.String), &l.ComponentState); err != nil {
			return l, err
		}
	}
	if intent.Valid {
		v := int(intent.Int64)
		l.IntentStep = &v
	}
	return l, nil
}
func (s Store) List(ctx context.Context, pendingOnly bool) ([]Local, error) {
	query := "SELECT id FROM operations"
	if pendingOnly {
		query += " WHERE terminal=0"
	}
	query += " ORDER BY id"
	rows, e := s.DB.QueryContext(ctx, query)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	items := []Local{}
	for _, id := range ids {
		l, e := s.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		items = append(items, l)
	}
	return items, nil
}
func (s Store) Pending(ctx context.Context) ([]string, error) {
	list, e := s.List(ctx, true)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for _, l := range list {
		ids = append(ids, l.Record.ID)
	}
	return ids, nil
}
func begin(ctx context.Context, db *sql.DB) (*sql.Conn, error) {
	c, e := db.Conn(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = c.ExecContext(ctx, "BEGIN IMMEDIATE"); e != nil {
		c.Close()
		return nil, e
	}
	return c, nil
}
func finish(c *sql.Conn) { c.ExecContext(context.Background(), "ROLLBACK"); c.Close() }
func (s Store) Receive(ctx context.Context, id string, version int, action protocol.OperationAction, bootID string) (Local, bool, error) {
	if !validID.MatchString(id) || version != protocol.Version {
		return Local{}, false, firewall.Unsupported("操作标识或契约版本无效")
	}
	if _, e := protocol.Encode(protocol.Message{RequestID: "validation", Payload: &protocol.OperationSubmit{OperationID: id, ContractVersion: version, Action: action}}); e != nil {
		return Local{}, false, e
	}
	if action.Kind != "firewall" && action.Kind != "firewall_config" && !(action.Kind == "component" && action.Component != nil && action.Component.Component == "firewall" && action.Component.Action == "install") {
		return Local{}, false, firewall.Unsupported("当前版本不执行此类动作")
	}
	c, e := begin(ctx, s.DB)
	if e != nil {
		return Local{}, false, e
	}
	defer finish(c)
	l, e := load(ctx, c, id)
	if e == nil {
		if l.Action != nil {
			a, _ := json.Marshal(action)
			b, _ := json.Marshal(l.Action)
			if string(a) != string(b) {
				return l, false, firewall.Conflict("同一操作标识不能使用不同参数")
			}
		}
		return l, false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return l, false, e
	}
	var n int
	if e = c.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE terminal=0").Scan(&n); e != nil {
		return l, false, e
	}
	if n != 0 {
		return l, false, ErrBusy
	}
	if action.FirewallConfig != nil {
		if e = firewall.ValidateConfiguration(action.FirewallConfig.Configuration); e != nil {
			return l, false, e
		}
		confirmed, err := confirmedFrom(ctx, c)
		if err != nil {
			return l, false, err
		}
		if action.FirewallConfig.Automatic {
			if !sameTarget(confirmed, *action.FirewallConfig) {
				return l, false, firewall.Conflict("只能自动恢复已确认配置")
			}
		} else if action.FirewallConfig.Revision <= confirmed.Revision {
			return l, false, firewall.Conflict("配置版本已经改变")
		}
		if action.FirewallConfig.PeerTrustMigration != nil {
			if e = validateMigration(ctx, c, confirmed, *action.FirewallConfig); e != nil {
				return l, false, e
			}
		}
	}
	l = Local{Action: &action, BootID: bootID, Record: protocol.OperationRecord{ID: id, State: protocol.Prepared, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}}
	a, _ := json.Marshal(action)
	r, _ := json.Marshal(l.Record)
	if _, e = c.ExecContext(ctx, "INSERT INTO operations(id,action,contract_version,record,boot_id) VALUES(?,?,?,?,?)", id, string(a), version, string(r), bootID); e != nil {
		return l, false, e
	}
	_, e = c.ExecContext(ctx, "COMMIT")
	return l, e == nil, e
}
func (s Store) Reconcile(ctx context.Context, id string) (Local, error) {
	if !validID.MatchString(id) {
		return Local{}, firewall.Unsupported("操作标识无效")
	}
	c, e := begin(ctx, s.DB)
	if e != nil {
		return Local{}, e
	}
	defer finish(c)
	l, e := load(ctx, c, id)
	if e == nil {
		return l, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return l, e
	}
	l.Record = protocol.OperationRecord{ID: id, State: protocol.Rejected, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}, Error: &protocol.Error{Code: "not_accepted", Message: "Agent 未受理，已封存该操作标识"}}
	r, _ := json.Marshal(l.Record)
	if _, e = c.ExecContext(ctx, "INSERT INTO operations(id,contract_version,record,terminal) VALUES(?,?,?,1)", id, protocol.Version, string(r)); e != nil {
		return l, e
	}
	_, e = c.ExecContext(ctx, "COMMIT")
	return l, e
}

// Save 只有持有本机执行锁的进程调用；请求字段由通信进程独立登记。
func (s Store) Save(ctx context.Context, l *Local) error {
	previous := l.Record.Revision
	l.Record.Revision++
	l.Record.ConfirmationRemainingSeconds = nil
	r, e := json.Marshal(l.Record)
	if e != nil {
		return e
	}
	recipe, e := json.Marshal(l.Recipe)
	if e != nil {
		return e
	}
	component, e := json.Marshal(l.ComponentState)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, "UPDATE operations SET record=?,recipe=?,component_state=?,terminal=?,boot_id=?,deadline_ns=?,next_step=?,intent_step=? WHERE id=? AND json_extract(record,'$.revision')=?", string(r), string(recipe), string(component), Terminal(l.Record.State), l.BootID, l.DeadlineNS, l.NextStep, l.IntentStep, l.Record.ID, previous)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return firewall.Conflict("操作状态已改变")
	}
	if l.Record.State == protocol.Succeeded && l.Action != nil && l.Action.FirewallConfig != nil && !l.Action.FirewallConfig.Automatic {
		if e = saveConfirmed(ctx, tx, *l.Action.FirewallConfig); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s Store) Request(ctx context.Context, id, kind string, revision uint64) (Local, error) {
	c, e := begin(ctx, s.DB)
	if e != nil {
		return Local{}, e
	}
	defer finish(c)
	l, e := load(ctx, c, id)
	if e != nil {
		return l, e
	}
	if kind == "confirm" && l.Record.State == protocol.Succeeded && l.Record.Decision == "keep" {
		return l, nil
	}
	if l.Record.Revision != revision {
		return l, firewall.Conflict("操作阶段已变化，请重新读取")
	}
	if kind == "confirm" && l.Record.State != protocol.AwaitingConfirmation {
		return l, firewall.Conflict("操作不在待确认阶段")
	}
	if kind == "resolve" && l.Record.State != protocol.AttentionRequired {
		return l, firewall.Conflict("只能人工结束需处理操作")
	}
	if Terminal(l.Record.State) {
		return l, firewall.Conflict("操作已经结束")
	}
	if kind != "confirm" && kind != "recover" && kind != "resolve" {
		return l, fmt.Errorf("未知操作处理请求")
	}
	if _, e = c.ExecContext(ctx, "UPDATE operations SET request=?,request_revision=? WHERE id=?", kind, revision, id); e != nil {
		return l, e
	}
	_, e = c.ExecContext(ctx, "COMMIT")
	return l, e
}

// Keep 在取得 SQLite 写入资格后裁决期限，保留决定与成功结果同事务提交。
func (s Store) Keep(ctx context.Context, id string, revision uint64, clock func() (string, int64, error)) (Local, error) {
	c, e := begin(ctx, s.DB)
	if e != nil {
		return Local{}, e
	}
	defer finish(c)
	l, e := load(ctx, c, id)
	if e != nil {
		return l, e
	}
	if l.Record.State == protocol.Succeeded && l.Record.Decision == "keep" {
		return l, nil
	}
	if l.Record.State != protocol.AwaitingConfirmation || l.Record.Revision != revision || l.Record.Decision == "revert" {
		return l, firewall.Conflict("操作已退出待确认阶段")
	}
	boot, n, e := clock()
	if e != nil {
		return l, e
	}
	if boot != l.BootID || n >= l.DeadlineNS {
		return l, ErrExpired
	}
	l.Record.State = protocol.Succeeded
	l.Record.Decision = "keep"
	l.Record.Error = nil
	l.Record.Revision++
	l.Record.ConfirmationRemainingSeconds = nil
	data, e := json.Marshal(l.Record)
	if e != nil {
		return l, e
	}
	if _, e = c.ExecContext(ctx, "UPDATE operations SET record=?,terminal=1 WHERE id=?", string(data), id); e != nil {
		return l, e
	}
	if l.Action != nil && l.Action.FirewallConfig != nil && !l.Action.FirewallConfig.Automatic {
		if e = saveConfirmed(ctx, c, *l.Action.FirewallConfig); e != nil {
			return l, e
		}
	}
	_, e = c.ExecContext(ctx, "COMMIT")
	return l, e
}
