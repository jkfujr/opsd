package operations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

type monitoringTestRuntime struct {
	containerRuntime
	writes int
	ready  bool
}

func (*monitoringTestRuntime) PrepareMonitoring(context.Context, protocol.MonitoringDeployment) error {
	return nil
}
func (r *monitoringTestRuntime) ApplyMonitoring(context.Context, protocol.MonitoringDeployment) error {
	r.writes++
	return errors.New("启动应答丢失")
}
func (r *monitoringTestRuntime) MonitoringState(context.Context, protocol.MonitoringDeployment) (protocol.MonitoringState, error) {
	state := "starting"
	if r.ready {
		state = "ready"
	}
	return protocol.MonitoringState{State: state}, nil
}
func (*monitoringTestRuntime) DeploymentImageReady(context.Context, string) (bool, error) {
	return true, nil
}
func (*monitoringTestRuntime) PullDeploymentImage(context.Context, string) error { return nil }

func TestMonitoringUnknownDeploymentUsesOriginalOperation(t *testing.T) {
	e, _, _ := operationFixture(t)
	runtime := &monitoringTestRuntime{}
	e.Containers = runtime
	d := protocol.MonitoringDeployment{Enabled: true, ClusterID: strings.Repeat("a", 43), NodeID: strings.Repeat("b", 32), Directory: "/var/lib/opsd-monitoring", Prefix: "opsd/metrics/cluster/", DiskBudgetBytes: 1 << 30, Storage: protocol.S3Connection{Endpoint: "https://s3.example.test", Bucket: "metrics", AccessStyle: "path"}, Credentials: protocol.S3Credentials{Receive: protocol.S3Credential{AccessKey: "receive", SecretKey: "secret"}, Store: protocol.S3Credential{AccessKey: "store", SecretKey: "secret"}, Compact: protocol.S3Credential{AccessKey: "compact", SecretKey: "secret"}}}
	accept(t, e, "monitoring-original", protocol.OperationAction{Kind: "monitoring_deploy", Monitoring: &d})
	if err := e.Run(t.Context(), "monitoring-original"); err != nil {
		t.Fatal(err)
	}
	local, err := e.Store.Get(t.Context(), "monitoring-original")
	if err != nil || local.Record.State != protocol.AttentionRequired || runtime.writes != 1 || local.IntentStep == nil {
		t.Fatal("未知启动丢失了持久状态", local, err)
	}
	if err = e.Run(t.Context(), local.Record.ID); err != nil || runtime.writes != 1 {
		t.Fatal("未知启动被自动重试", runtime.writes, err)
	}
	runtime.ready = true
	if _, err = e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	e = &Engine{Store: e.Store, Clock: e.Clock, Containers: runtime}
	if err = e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatal(err)
	}
	local, err = e.Store.Get(t.Context(), local.Record.ID)
	if err != nil || local.Record.State != protocol.Succeeded || runtime.writes != 1 {
		t.Fatal("恢复未复用原组件与原操作", local, err, runtime.writes)
	}
}
