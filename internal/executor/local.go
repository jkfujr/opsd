// Package executor 只通过本地 Unix Socket 接受有类型的请求。
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

type Client struct{ HTTP *http.Client }

func NewClient(socket string) *Client {
	return &Client{HTTP: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 2}, Timeout: 35 * time.Second}}
}
func (c *Client) request(ctx context.Context, path string, b []byte) ([]byte, error) {
	r, e := http.NewRequestWithContext(ctx, "POST", "http://executor"+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	response, e := c.HTTP.Do(r)
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
	reply, e := c.request(ctx, "/rpc", b)
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
