package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/coder/websocket"
	"github.com/opsd-labs/opsd/internal/buildinfo"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
	"github.com/opsd-labs/opsd/internal/transport"
)

type PermanentError struct{ Message string }

func (e *PermanentError) Error() string { return e.Message }

func UnsupportedCapabilities() protocol.Capabilities {
	no := protocol.Capability{Reason: "此版本尚未实现"}
	return protocol.Capabilities{Read: no, EditRules: no, Reorder: no, DefaultPolicy: no, Enable: no, Install: no, DockerPorts: no, PeerTrustHostIPv4: no, PeerTrustHostIPv6: no, PeerTrustDockerIPv4: no, PeerTrustDockerIPv6: no}
}

// Run 在每次重连时重新加载已持久确认的身份，不依据一次响应丢失重新登记。
func Run(ctx context.Context, db *sql.DB, ready func()) error {
	attempts := 0
	for {
		value, err := Load(ctx, db)
		if err != nil {
			return err
		}
		connected := false
		err = connect(ctx, db, value, func() {
			attempts = 0
			connected = true
			if ready != nil {
				ready()
				ready = nil
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		var permanent *PermanentError
		if errors.As(err, &permanent) || errors.Is(err, transport.ErrAuthentication) {
			return err
		}
		if err != nil {
			log.Print("节点连接中断，等待重连")
		}
		seconds := min(1<<min(attempts, 5), 30)
		if !connected {
			attempts++
		}
		delay := time.Duration(float64(seconds) * (.8 + rand.Float64()*.4) * float64(time.Second))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func connect(ctx context.Context, db *sql.DB, value Identity, ready func()) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	socket, response, err := websocket.Dial(dialCtx, value.ControlURL+"/api/v1/agent/connect", &websocket.DialOptions{HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	cancel()
	if err != nil {
		if response != nil && response.StatusCode >= 400 && response.StatusCode < 500 {
			return &PermanentError{Message: "控制端拒绝接入，请核对地址与协议版本"}
		}
		return err
	}
	defer socket.CloseNow()
	channel, err := transport.Handshake(ctx, socket, value.Key, value.ControlPublic, true)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	requestID := identity.Random()
	registerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = channel.Send(registerCtx, protocol.Message{RequestID: requestID, Payload: &protocol.SessionOpen{Hostname: hostname, Purpose: "business", EnrollmentToken: value.EnrollmentToken, AgentVersion: buildinfo.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Capabilities: UnsupportedCapabilities(), PendingOperationIDs: []string{}}})
	if err != nil {
		cancel()
		return err
	}
	responseMessage, err := channel.Receive(registerCtx)
	cancel()
	if err != nil {
		return err
	}
	if responseMessage.ReplyTo != requestID {
		return &PermanentError{Message: "控制端登记应答不匹配"}
	}
	if failed, ok := responseMessage.Payload.(*protocol.RequestFailed); ok {
		return &PermanentError{Message: failed.Error.Message + "（" + failed.Error.Code + "）"}
	}
	accepted, ok := responseMessage.Payload.(*protocol.SessionAccepted)
	if !ok {
		return &PermanentError{Message: "控制端登记应答无效"}
	}
	if value.NodeID != "" && value.NodeID != accepted.NodeID {
		return &PermanentError{Message: "控制端返回了不同节点身份"}
	}
	if len(accepted.PendingOperationIDs) != 0 {
		return &PermanentError{Message: "存在当前版本无法解释的未决操作，已停止接入"}
	}
	if _, err := db.ExecContext(ctx, "UPDATE agent_identity SET node_id=?,enrollment_token='' WHERE id=1", accepted.NodeID); err != nil {
		return err
	}
	ready()
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	type incoming struct {
		message protocol.Message
		err     error
	}
	messages := make(chan incoming, 1)
	go func() {
		for {
			deadline, cancel := context.WithTimeout(readCtx, 45*time.Second)
			message, err := channel.Receive(deadline)
			cancel()
			select {
			case messages <- incoming{message, err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	responseDeadline := time.NewTimer(time.Hour)
	responseDeadline.Stop()
	defer responseDeadline.Stop()
	awaiting := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-responseDeadline.C:
			return fmt.Errorf("心跳应答超时")
		case value := <-messages:
			if value.err != nil {
				return value.err
			}
			switch body := value.message.Payload.(type) {
			case *protocol.Heartbeat:
				if awaiting == "" || value.message.ReplyTo != awaiting {
					return fmt.Errorf("心跳应答无效")
				}
				awaiting = ""
				responseDeadline.Stop()
			case *protocol.RequestFailed:
				return &PermanentError{Message: body.Error.Message}
			default:
				deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := channel.Send(deadline, protocol.Message{RequestID: identity.Random(), ReplyTo: value.message.RequestID, Payload: &protocol.RequestFailed{Error: protocol.Error{Code: "unsupported_message", Message: "当前版本未实现此消息"}}})
				cancel()
				if err != nil {
					return err
				}
			}
		case <-ticker.C:
			if awaiting != "" {
				continue
			}
			awaiting = identity.Random()
			heartbeatCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = channel.Send(heartbeatCtx, protocol.Message{RequestID: awaiting, Payload: &protocol.Heartbeat{SentAt: time.Now().UTC()}})
			cancel()
			if err != nil {
				return err
			}
			responseDeadline.Reset(30 * time.Second)
		}
	}
}
