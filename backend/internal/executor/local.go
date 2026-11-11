// Package executor 只通过本地 Unix Socket 接受有类型的请求。
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coder/websocket"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/transport"
)

type Client struct {
	HTTP, Control *http.Client
	Socket        string
}

func NewClient(socket string) *Client {
	create := func(limit int) *http.Client {
		return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}, MaxConnsPerHost: limit, MaxIdleConnsPerHost: limit, IdleConnTimeout: time.Minute}, Timeout: 35 * time.Second}
	}
	return &Client{HTTP: create(6), Control: create(2), Socket: socket}
}

func (c *Client) Terminal(ctx context.Context) (protocol.TerminalStream, error) {
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	}}}
	socket, _, err := websocket.Dial(ctx, "ws://executor/terminal", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return nil, err
	}
	socket.SetReadLimit(65536)
	return transport.TerminalSocket{Socket: socket}, nil
}
func (c *Client) request(ctx context.Context, path string, b []byte, control ...bool) ([]byte, error) {
	r, e := http.NewRequestWithContext(ctx, "POST", "http://executor"+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if len(control) > 0 && control[0] && c.Control != nil {
		client = c.Control
	}
	response, e := client.Do(r)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, protocol.MaxMessageBytes+1))
	if e != nil {
		return nil, e
	}
	if len(data) > protocol.MaxMessageBytes {
		return nil, fmt.Errorf("本机应答过大")
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("本机执行器请求失败")
	}
	return data, nil
}
func (c *Client) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	b, e := protocol.Encode(protocol.Message{RequestID: identity.Random(), Payload: p})
	if e != nil {
		return nil, e
	}
	control := false
	switch p.(type) {
	case *protocol.OperationCancel, *protocol.OperationQuery, *protocol.OperationConfirm, *protocol.OperationRecover, *protocol.OperationResolve:
		control = true
	}
	reply, e := c.request(ctx, "/rpc", b, control)
	if e != nil {
		return nil, e
	}
	message, e := protocol.Decode(reply)
	if e != nil {
		return nil, e
	}
	if failure, ok := message.Payload.(*protocol.RequestFailed); ok {
		return nil, &firewall.Fault{Code: failure.Error.Code, Message: failure.Error.Message}
	}
	return message.Payload, nil
}
func (c *Client) Storage(ctx context.Context, s cluster.StorageConfig) (cluster.Credentials, error) {
	b, e := json.Marshal(s)
	if e != nil {
		return cluster.Credentials{}, e
	}
	reply, e := c.request(ctx, "/storage", b)
	if e != nil {
		return cluster.Credentials{}, e
	}
	var result struct {
		Credentials cluster.Credentials `json:"credentials"`
		Error       string              `json:"error,omitempty"`
	}
	if e = json.Unmarshal(reply, &result); e != nil {
		return result.Credentials, e
	}
	if result.Error != "" {
		return result.Credentials, fmt.Errorf("%s", result.Error)
	}
	return result.Credentials, nil
}

type Server struct {
	prepare   bool
	Services  *Services
	Directory string
	Host      firewall.Host
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /terminal", func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		socket.SetReadLimit(65536)
		stream := transport.TerminalSocket{Socket: socket}
		if err = s.Services.Terminals.Serve(r.Context(), stream); err != nil {
			_ = stream.Send(r.Context(), protocol.TerminalFrame{Type: "exit", Message: err.Error()})
			stream.Close()
		}
	})
	mux.HandleFunc("POST /rpc", func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.MaxMessageBytes))
		if e != nil {
			http.Error(w, "请求过大", 413)
			return
		}
		message, e := protocol.Decode(b)
		if e != nil {
			http.Error(w, "请求无效", 422)
			return
		}
		payload, e := s.Services.Handle(r.Context(), message.Payload)
		if e != nil {
			code := firewall.Code(e)
			var docker *dockerengine.Fault
			if errors.As(e, &docker) {
				code = docker.Code
			}
			payload = &protocol.RequestFailed{Error: protocol.Error{Code: code, Message: e.Error()}}
		}
		out, e := protocol.Encode(protocol.Message{RequestID: identity.Random(), ReplyTo: message.RequestID, Payload: payload})
		if e != nil {
			http.Error(w, "应答失败", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})
	mux.HandleFunc("POST /storage", func(w http.ResponseWriter, r *http.Request) {
		var c cluster.StorageConfig
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&c) != nil {
			http.Error(w, "请求无效", 422)
			return
		}
		creds, e := s.storage(r.Context(), c)
		result := map[string]any{"credentials": creds}
		if e != nil {
			result["error"] = "数据库容器未就绪，请检查 Docker 和绑定地址"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	return mux
}
func Listen(socket string) (net.Listener, error) {
	if e := os.MkdirAll(filepath.Dir(socket), 0750); e != nil {
		return nil, e
	}
	if _, e := os.Lstat(socket); e == nil {
		c, e := net.DialTimeout("unix", socket, time.Second)
		if e == nil {
			c.Close()
			return nil, fmt.Errorf("本机执行器已启动")
		}
		if e = os.Remove(socket); e != nil {
			return nil, e
		}
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(socket, 0660); e != nil {
		l.Close()
		return nil, e
	}
	return l, nil
}
