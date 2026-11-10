package terminal

import (
	"context"
	"fmt"
	"github.com/creack/pty"
	"github.com/jkfujr/opsd/internal/protocol"
	"os"
	"os/exec"
	"sync"
)

type Manager struct {
	mu       sync.Mutex
	sessions map[*session]context.CancelFunc
	closed   bool
}
type session struct {
	file    *os.File
	command *exec.Cmd
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, cancel := range m.sessions {
		cancel()
	}
}

func (m *Manager) Serve(ctx context.Context, stream protocol.TerminalStream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &session{}
	m.mu.Lock()
	if m.closed || len(m.sessions) >= 8 {
		m.mu.Unlock()
		return fmt.Errorf("终端会话已达上限")
	}
	if m.sessions == nil {
		m.sessions = map[*session]context.CancelFunc{}
	}
	m.sessions[s] = cancel
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.sessions, s); m.mu.Unlock() }()
	var err error
	s.file, s.command, err = startShell()
	if err != nil {
		return err
	}
	defer stream.Close()
	var once sync.Once
	cleanup := func() { once.Do(func() { stopShell(s.command.Process.Pid); s.file.Close() }) }
	defer cleanup()
	go func() { <-ctx.Done(); stream.Close(); cleanup() }()
	wait := make(chan error, 1)
	go func() { wait <- s.command.Wait() }()
	go func() {
		defer cancel()
		for {
			f, e := stream.Receive(ctx)
			if e != nil {
				return
			}
			switch f.Type {
			case "input":
				if _, e = s.file.Write(f.Data); e != nil {
					return
				}
			case "resize":
				if e = pty.Setsize(s.file, &pty.Winsize{Cols: f.Cols, Rows: f.Rows}); e != nil {
					return
				}
			case "exit":
				return
			default:
				return
			}
		}
	}()
	buffer := make([]byte, 16384)
	for {
		n, e := s.file.Read(buffer)
		if n > 0 {
			if err = stream.Send(ctx, protocol.TerminalFrame{Type: "output", Data: buffer[:n]}); err != nil {
				cancel()
				break
			}
		}
		if e != nil {
			break
		}
	}
	cleanup()
	result := <-wait
	message := "终端已关闭"
	if result != nil {
		message = result.Error()
	}
	if ctx.Err() == nil {
		_ = stream.Send(ctx, protocol.TerminalFrame{Type: "exit", Message: message})
	}
	return nil
}
