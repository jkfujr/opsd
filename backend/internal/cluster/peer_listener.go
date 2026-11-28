package cluster

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/transport"
)

// ServePeers 在同一节点监听端口识别旧 WebSocket 与私有 TCP。
func (n *Node) ServePeers(listener net.Listener, tcpOnly bool) error {
	ctx, cancel := context.WithCancel(n.ctx)
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	var connections sync.WaitGroup
	defer func() {
		cancel()
		listener.Close()
		stop()
		connections.Wait()
	}()

	var webConns chan net.Conn
	if !tcpOnly {
		webConns = make(chan net.Conn)
		web := &peerWebListener{ctx: ctx, addr: listener.Addr(), incoming: webConns, close: cancel}
		server := &http.Server{Handler: n.PeerHandler(), ReadHeaderTimeout: 10 * time.Second}
		go server.Serve(web)
		defer server.Close()
	}

	handshakes := make(chan struct{}, 32)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		select {
		case handshakes <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		connections.Go(func() {
			release := sync.OnceFunc(func() { <-handshakes })
			defer release()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			reader := bufio.NewReaderSize(conn, 64)
			first, err := reader.Peek(1)
			if err != nil {
				conn.Close()
				return
			}
			buffered := &peerBufferedConn{Conn: conn, reader: reader}
			if first[0] == transport.TCPPreface[0] {
				defer conn.Close()
				if n.Revoked(ctx, n.ID) {
					return
				}
				channel, err := transport.HandshakeTCP(ctx, buffered, n.Key, nil, false)
				if err == nil {
					conn.SetReadDeadline(time.Time{})
					n.acceptPeer(ctx, conn.RemoteAddr().String(), channel, release)
				}
				return
			}
			if webConns != nil {
				conn.SetReadDeadline(time.Time{})
				select {
				case webConns <- buffered:
					return
				case <-ctx.Done():
				}
			}
			conn.Close()
		})
	}
}

type peerWebListener struct {
	ctx      context.Context
	addr     net.Addr
	incoming <-chan net.Conn
	close    context.CancelFunc
}

func (l *peerWebListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.incoming:
		return conn, nil
	case <-l.ctx.Done():
		return nil, net.ErrClosed
	}
}

func (l *peerWebListener) Addr() net.Addr { return l.addr }
func (l *peerWebListener) Close() error   { l.close(); return nil }

type peerBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *peerBufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
