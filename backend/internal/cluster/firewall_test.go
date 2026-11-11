package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type configExecutor struct {
	lostExecutor
	inspection  protocol.FirewallInspected
	inspectErr  error
	inspections int
	targets     []protocol.FirewallTarget
	fail        bool
}

func TestFirewallChecksKeepOnlyActualStateChanges(t *testing.T) {
	n, _ := openTestNode(t)
	d := protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{}}
	x := &configExecutor{inspection: protocol.FirewallInspected{Target: &protocol.FirewallTarget{Revision: 1, Configuration: d}, Backend: "ufw", InSync: true, CheckedAt: time.Now().UTC()}}
	n.Executor = x
	for range 10 {
		n.checkFirewall(t.Context())
		x.inspection.CheckedAt = x.inspection.CheckedAt.Add(time.Second)
	}
	var count int
	if err := n.DB.QueryRow("SELECT count(*) FROM records WHERE kind='firewall'").Scan(&count); err != nil || count != 1 {
		t.Fatal("重复观测仍追加共享记录", count, err)
	}
	x.inspection.Error = &protocol.Error{Code: "dependency_unavailable", Message: "实际读取失败"}
	n.checkFirewall(t.Context())
	if err := n.DB.QueryRow("SELECT count(*) FROM records WHERE kind='firewall'").Scan(&count); err != nil || count != 2 {
		t.Fatal("实际错误没有进入历史", count, err)
	}
}

func TestFirewallCleanupKeepsChangesLatestAndPendingRecords(t *testing.T) {
	n, _ := openTestNode(t)
	ctx := t.Context()
	ids := []string{}
	for i, state := range []string{"in_sync", "in_sync", "in_sync", "in_sync", "failed", "failed", "failed", "in_sync", "in_sync", "in_sync"} {
		at := time.Now().UTC().Add(time.Duration(i) * time.Second)
		tx, err := n.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = n.appendTx(ctx, tx, "firewall", n.ID, protocol.ManagedFirewallState{State: state, CheckedAt: &at}, at); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var id string
		if err = n.DB.QueryRow("SELECT id FROM records WHERE kind='firewall' ORDER BY sequence DESC LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := n.DB.Exec("UPDATE records SET uploaded=1 WHERE kind='firewall' AND id<>?", ids[1]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := n.compactFirewallRecords(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range ids {
		var count int
		if err := n.DB.QueryRow("SELECT count(*) FROM records WHERE id=?", id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := i == 0 || i == 1 || i == 3 || i == 4 || i == 6 || i == 7 || i == 9
		if (count == 1) != want {
			t.Fatalf("第%d条保留状态不符：%d", i, count)
		}
	}
}

func (x *configExecutor) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p := p.(type) {
	case *protocol.FirewallInspect:
		x.inspections++
		return &x.inspection, x.inspectErr
	case *protocol.OperationSubmit:
		x.submits++
		x.targets = append(x.targets, *p.Action.FirewallConfig)
		state := protocol.Succeeded
		if x.fail {
			state = protocol.Reverted
		}
		x.record = &protocol.OperationRecord{ID: p.OperationID, State: state, Revision: 2, Steps: []protocol.OperationStep{}}
		return nil, errors.New("应答丢失")
	}
	return x.lostExecutor.Call(ctx, p)
}

func TestManagedFirewallKeepsConfirmedStateDuringBusyInspection(t *testing.T) {
	n, _ := openTestNode(t)
	d := protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{{Family: protocol.IPv4, Action: "deny"}}}
	x := &configExecutor{inspection: protocol.FirewallInspected{Target: &protocol.FirewallTarget{Revision: 1, Configuration: d}, Backend: "ufw", InSync: true, CheckedAt: time.Now().UTC()}}
	n.Executor = x
	n.checkFirewall(t.Context())
	before, _ := n.FirewallConfig(t.Context())
	x.inspectErr = &firewall.Fault{Code: "node_busy", Message: "本机操作执行中"}
	n.checkFirewall(t.Context())
	after, err := n.FirewallConfig(t.Context())
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if err != nil || string(a) != string(b) || after.State != "in_sync" {
		t.Fatal("锁繁忙改写了已确认配置状态", after, err)
	}
	x.inspectErr = errors.New("防火墙读取失败")
	n.checkFirewall(t.Context())
	after, err = n.FirewallConfig(t.Context())
	if err != nil || after.State != "failed" || after.Error == nil || after.ConfirmedRevision != before.ConfirmedRevision {
		t.Fatal("实际读取失败未保留错误及确认版本", after, err)
	}
}

func TestManagedFirewallSavedReadsIdempotencyAndFailedRevision(t *testing.T) {
	n, _ := openTestNode(t)
	d := protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{{Family: protocol.IPv4, Action: "deny"}}}
	x := &configExecutor{inspection: protocol.FirewallInspected{Target: &protocol.FirewallTarget{Revision: 1, Configuration: d}, Backend: "ufw", InSync: true, CheckedAt: time.Now().UTC()}}
	n.Executor = x
	n.checkFirewall(t.Context())
	for range 5 {
		state, err := n.FirewallConfig(t.Context())
		if err != nil || state.Revision != 1 {
			t.Fatal(state, err)
		}
	}
	if x.inspections != 1 {
		t.Fatal("普通配置读取调用了执行器")
	}
	request := FirewallWrite{ExpectedRevision: 1, Key: "save", Change: &protocol.FirewallChange{Kind: "set_default_policy", Action: "allow"}}
	saved, err := n.writeFirewall(t.Context(), n.ID, request, false)
	if err != nil || saved.Configuration.State != "pending" || saved.Configuration.Revision != 2 || saved.Configuration.ConfirmedRevision != 1 {
		t.Fatal(saved, err)
	}
	again, err := n.writeFirewall(t.Context(), n.ID, request, false)
	if err != nil || again.OperationID != saved.OperationID {
		t.Fatal(again, err)
	}
	request.Key = "stale"
	if _, err = n.writeFirewall(t.Context(), n.ID, request, false); err == nil {
		t.Fatal("旧版本被接受")
	}
	if _, err = n.writeFirewall(t.Context(), n.ID, FirewallWrite{ExpectedRevision: 2, Key: "premature-retry"}, false); err == nil {
		t.Fatal("未结束操作被重试")
	}
	n.advance() // 执行器受理，但响应丢失。
	n.advance() // 只查询同一个操作。
	state, err := n.FirewallConfig(t.Context())
	if err != nil || state.State != "in_sync" || state.ConfirmedRevision != 2 || x.submits != 1 {
		t.Fatal(state, err, x.submits)
	}
	x.inspection.Target = &protocol.FirewallTarget{Revision: 2, Configuration: state.FirewallDefinition}
	request = FirewallWrite{ExpectedRevision: 2, Key: "failure", Change: &protocol.FirewallChange{Kind: "set_default_policy", Action: "reject"}}
	if _, err = n.writeFirewall(t.Context(), n.ID, request, false); err != nil {
		t.Fatal(err)
	}
	x.fail = true
	n.advance()
	n.advance()
	state, err = n.FirewallConfig(t.Context())
	if err != nil || state.State != "failed" || state.Revision != 3 || state.ConfirmedRevision != 2 {
		t.Fatal(state, err)
	}
	for range 3 {
		n.checkFirewall(t.Context())
		n.advance()
	}
	if x.submits != 2 {
		t.Fatal("失败配置被自动重试", x.submits)
	}
	x.inspection.InSync = false
	n.checkFirewall(t.Context())
	n.advance()
	last := x.targets[len(x.targets)-1]
	if !last.Automatic || last.Revision != 2 || last.Configuration.DefaultPolicies[0].Action != "allow" {
		t.Fatalf("外部修改没有恢复已确认配置：%+v", last)
	}
	var path string
	if err = n.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	second, err := Open(t.Context(), path, Options{Executor: x})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	before, _ := n.FirewallConfig(t.Context())
	after, err := second.FirewallConfig(t.Context())
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if err != nil || string(b1) != string(b2) {
		t.Fatal("重启后配置被重新导入", err)
	}
}
