package protocol

import "encoding/json"

func ControlMethod(method string) bool {
	switch method {
	case "operation.get", "operation.confirm", "operation.cancel", "operation.recover", "operation.resolve", "ping", "api_token.verify":
		return true
	}
	return false
}

type PeerHello struct {
	Stream      string          `json:"stream,omitempty"`
	Token       string          `json:"token,omitempty"`
	Member      json.RawMessage `json:"member"`
	Directory   json.RawMessage `json:"directory,omitempty"`
	Resident    bool            `json:"resident"`
	ProbeFamily AddressFamily   `json:"probe_family,omitempty"`
}

func (*PeerHello) Kind() Kind { return "peer.hello" }
func (*PeerHello) isPayload() {}

type PeerCall struct {
	Method string          `json:"method"`
	Body   json.RawMessage `json:"body"`
}

func (*PeerCall) Kind() Kind { return "peer.call" }
func (*PeerCall) isPayload() {}

type PeerReply struct {
	Body  json.RawMessage `json:"body,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

func (*PeerReply) Kind() Kind { return "peer.reply" }
func (*PeerReply) isPayload() {}
