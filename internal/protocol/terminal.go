package protocol

import (
	"context"
	"fmt"
)

// TerminalStream 是独立于业务请求的数据通道。
type TerminalStream interface {
	Send(context.Context, TerminalFrame) error
	Receive(context.Context) (TerminalFrame, error)
	Close()
}

type TerminalFrame struct {
	Type    string `json:"type"`
	Data    []byte `json:"data,omitempty"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Message string `json:"message,omitempty"`
}

func (*TerminalFrame) Kind() Kind { return "terminal.frame" }
func (*TerminalFrame) isPayload() {}
func (f TerminalFrame) Validate() error {
	if len(f.Data) > 32768 || len(f.Message) > 1024 {
		return fmt.Errorf("终端消息过大")
	}
	switch f.Type {
	case "input", "output", "exit":
		return nil
	case "resize":
		if f.Cols >= 2 && f.Cols <= 500 && f.Rows >= 2 && f.Rows <= 300 {
			return nil
		}
	}
	return fmt.Errorf("终端消息无效")
}
