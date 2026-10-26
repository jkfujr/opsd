package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeTypedMessages(t *testing.T) {
	tests := []struct {
		name  string
		wire  string
		check func(*testing.T, Payload)
	}{
		{"身份登记", `{"version":1,"kind":"session.open","request_id":"req-1","body":{"purpose":"business","enrollment_token":"once","agent_version":"0.1.0-dev","os":"linux","arch":"amd64","capabilities":{},"pending_operation_ids":["op-old"]}}`, func(t *testing.T, payload Payload) {
			body, ok := payload.(*SessionOpen)
			if !ok || body.EnrollmentToken != "once" || len(body.PendingOperationIDs) != 1 || body.PendingOperationIDs[0] != "op-old" {
				t.Fatalf("登记消息解码错误：%#v", payload)
			}
		}},
		{"有类型防火墙操作", `{"version":1,"kind":"operation.submit","request_id":"req-2","body":{"operation_id":"op-1","contract_version":1,"action":{"kind":"firewall","firewall":{"id":"plan-1","contract_version":1,"created_at":"2026-10-27T00:00:00Z","backend":"ufw","change":{"kind":"set_enabled","enabled":false},"baseline":{"rules":[],"default_policies":[],"docker_ports":[],"service_active":true},"affected_scopes":["host"],"requires_confirmation":true,"executable":true,"reasons":[]}}}}`, func(t *testing.T, payload Payload) {
			body, ok := payload.(*OperationSubmit)
			if !ok || body.OperationID != "op-1" || body.Action.Firewall == nil || body.Action.Firewall.Change.Enabled == nil || *body.Action.Firewall.Change.Enabled {
				t.Fatalf("操作动作解码错误：%#v", payload)
			}
		}},
		{"查询未找到", `{"version":1,"kind":"operation.query.result","request_id":"reply-1","reply_to":"req-old","body":{"operation_id":"op-unknown","found":false}}`, func(t *testing.T, payload Payload) {
			body, ok := payload.(*OperationQueryResult)
			if !ok || body.Found || body.Record != nil {
				t.Fatalf("查询结果被误作已有记录：%#v", payload)
			}
		}},
		{"核对封存记录", `{"version":1,"kind":"operation.reconcile.result","request_id":"reply-2","body":{"record":{"id":"op-unknown","state":"rejected","revision":1,"decision":"undecided","steps":[]}}}`, func(t *testing.T, payload Payload) {
			body, ok := payload.(*OperationReconcileResult)
			if !ok || body.Record.State != Rejected || body.Record.Revision != 1 {
				t.Fatalf("核对结果解码错误：%#v", payload)
			}
		}},
		{"持续互信目标", `{"version":1,"kind":"peer_trust.target","request_id":"req-3","body":{"revision":12,"enabled":true,"ipv4":["192.0.2.3"],"ipv6":[]}}`, func(t *testing.T, payload Payload) {
			body, ok := payload.(*PeerTrustTarget)
			if !ok || body.Revision != 12 || !body.Enabled || len(body.IPv4) != 1 {
				t.Fatalf("互信目标解码错误：%#v", payload)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, err := Decode([]byte(test.wire))
			if err != nil {
				t.Fatal(err)
			}
			test.check(t, message.Payload)
			encoded, err := Encode(message)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.RequestID != message.RequestID || decoded.ReplyTo != message.ReplyTo {
				t.Fatal("通信关联标识丢失")
			}
			test.check(t, decoded.Payload)
		})
	}
}

func TestRejectMalformedMessages(t *testing.T) {
	tests := []string{
		`{"version":1,"kind":"unknown","request_id":"req","body":{}}`,
		`{"version":2,"kind":"heartbeat","request_id":"req","body":{}}`,
		`{"version":1,"kind":"heartbeat","request_id":"req","body":`,
		`{"version":1,"kind":"heartbeat","request_id":"req","body":null}`,
		`{"version":1,"kind":"heartbeat","body":{}}`,
		`{"version":1,"kind":"operation.query","request_id":"req","body":{"operation_id":8}}`,
		`{"version":1,"kind":"operation.query","request_id":"req","body":{"operation_id":"op","node_id":"other-node"}}`,
		`{"version":1,"kind":"operation.query","request_id":"req","body":{}}`,
		`{"version":1,"kind":"operation.query.result","request_id":"req","body":{"operation_id":"op","found":true}}`,
		`{"version":1,"kind":"firewall.preview","request_id":"req","body":{"change":{"kind":"shell"}}}`,
		`{"version":1,"kind":"operation.submit","request_id":"req","body":{"operation_id":"op","contract_version":1,"action":{"kind":"shell","command":"uname"}}}`,
		`{"version":1,"kind":"session.open","request_id":"req","body":{"purpose":"migration_probe"}}`,
		`{"version":1,"kind":"peer_trust.query","request_id":"req","body":{}} {}`,
	}
	for _, wire := range tests {
		if _, err := Decode([]byte(wire)); err == nil {
			t.Errorf("损坏消息被接受：%s", wire)
		}
	}
	if _, err := Decode([]byte{0xff}); err == nil {
		t.Error("接受了无效 UTF-8")
	}
	if _, err := Decode(bytes.Repeat([]byte(" "), MaxMessageBytes+1)); err == nil {
		t.Error("接受了超限消息")
	}
}

func TestEncodeLimitsAndActionMismatch(t *testing.T) {
	var missing *Heartbeat
	if _, err := Encode(Message{RequestID: "req", Payload: missing}); err == nil {
		t.Fatal("接受了空指针载荷")
	}
	if _, err := Encode(Message{RequestID: "req", Payload: &RequestFailed{Error: Error{Code: "large", Message: strings.Repeat("x", MaxMessageBytes)}}}); err == nil {
		t.Fatal("编码超限消息未失败")
	}
	mismatched := `{"version":1,"kind":"operation.submit","request_id":"req","body":{"operation_id":"op","contract_version":1,"action":{"kind":"deployment","firewall":{"id":"plan","contract_version":1,"created_at":"2026-10-27T00:00:00Z","change":{"kind":"set_enabled","enabled":true}}}}}`
	if _, err := Decode([]byte(mismatched)); err == nil {
		t.Fatal("动作类型与业务载荷不匹配仍被接受")
	}
	message, err := Encode(Message{RequestID: "req", Payload: &PeerTrustTarget{Revision: 5, Enabled: false, IPv4: []string{}, IPv6: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Kind string `json:"kind"`
		Body struct {
			Revision uint64 `json:"revision"`
			Enabled  bool   `json:"enabled"`
		} `json:"body"`
	}
	if err := json.Unmarshal(message, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Kind != "peer_trust.target" || wire.Body.Revision != 5 || wire.Body.Enabled {
		t.Fatal("关闭互信的最新目标未被正确编码")
	}
}
