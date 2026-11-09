package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

const RecordBytes = 60 * 1024

var ErrAuthentication = errors.New("身份或协议不匹配")

type Channel struct {
	socket        *websocket.Conn
	send, receive *noise.CipherState
	readMu        sync.Mutex
	writeSlot     chan struct{}
	sequence      atomic.Uint64
	fragments     map[uint64]*fragment
	fragmentBytes int
	PeerPublic    []byte
}

type fragment struct {
	total int
	body  []byte
}

func Handshake(ctx context.Context, socket *websocket.Conn, key identity.Key, peer []byte, initiator bool) (*Channel, error) {
	socket.SetReadLimit(65535)
	state, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:     noise.HandshakeIK, Initiator: initiator, Prologue: []byte("OPSD/node/v2"),
		StaticKeypair: noise.DHKey{Private: key.Private, Public: key.Public}, PeerStatic: peer,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var first, second *noise.CipherState
	if initiator {
		data, _, _, e := state.WriteMessage(nil, nil)
		if e != nil {
			return nil, e
		}
		if err = socket.Write(ctx, websocket.MessageBinary, data); err != nil {
			return nil, err
		}
		data, e = readBinary(ctx, socket)
		if e != nil {
			if websocket.CloseStatus(e) == websocket.StatusPolicyViolation {
				return nil, ErrAuthentication
			}
			return nil, e
		}
		payload, a, b, e := state.ReadMessage(nil, data)
		if e != nil || len(payload) != 0 {
			return nil, ErrAuthentication
		}
		first, second = a, b
	} else {
		data, e := readBinary(ctx, socket)
		if e != nil {
			return nil, e
		}
		payload, _, _, e := state.ReadMessage(nil, data)
		if e != nil || len(payload) != 0 {
			return nil, ErrAuthentication
		}
		data, first, second, e = state.WriteMessage(nil, nil)
		if e != nil {
			return nil, e
		}
		if err = socket.Write(ctx, websocket.MessageBinary, data); err != nil {
			return nil, err
		}
	}
	channel := &Channel{socket: socket, send: first, receive: second, writeSlot: make(chan struct{}, 1), PeerPublic: append([]byte(nil), state.PeerStatic()...)}
	if !initiator {
		channel.send, channel.receive = second, first
	}
	return channel, nil
}

func readBinary(ctx context.Context, socket *websocket.Conn) ([]byte, error) {
	kind, data, err := socket.Read(ctx)
	if err != nil {
		return nil, err
	}
	if kind != websocket.MessageBinary {
		return nil, fmt.Errorf("只接受二进制记录")
	}
	return data, nil
}

func (c *Channel) Close() { c.socket.CloseNow() }

func (c *Channel) Send(ctx context.Context, message protocol.Message) error {
	data, err := protocol.Encode(message)
	if err != nil {
		return err
	}
	id := c.sequence.Add(1)
	if id == 0 {
		c.Close()
		return fmt.Errorf("消息序号已耗尽")
	}
	for offset := 0; offset < len(data); {
		size := min(RecordBytes-16, len(data)-offset)
		plain := make([]byte, 16+size)
		binary.BigEndian.PutUint64(plain, id)
		binary.BigEndian.PutUint32(plain[8:], uint32(len(data)))
		binary.BigEndian.PutUint32(plain[12:], uint32(offset))
		copy(plain[16:], data[offset:offset+size])
		select {
		case c.writeSlot <- struct{}{}:
		case <-ctx.Done():
			if offset > 0 {
				c.Close()
			}
			return ctx.Err()
		}
		record, e := c.send.Encrypt(nil, nil, plain)
		if e == nil {
			e = c.socket.Write(ctx, websocket.MessageBinary, record)
		}
		<-c.writeSlot
		if e != nil {
			c.Close()
			return e
		}
		offset += size
	}
	return nil
}

func (c *Channel) Receive(ctx context.Context) (protocol.Message, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.fragments == nil {
		c.fragments = map[uint64]*fragment{}
	}
	for {
		data, err := c.readRecord(ctx)
		if err != nil {
			return protocol.Message{}, err
		}
		if len(data) <= 16 {
			c.Close()
			return protocol.Message{}, fmt.Errorf("消息分片不完整")
		}
		id := binary.BigEndian.Uint64(data)
		total, offset := int(binary.BigEndian.Uint32(data[8:])), int(binary.BigEndian.Uint32(data[12:]))
		if id == 0 || total <= 0 || total > protocol.MaxMessageBytes || offset+len(data)-16 > total {
			c.Close()
			return protocol.Message{}, fmt.Errorf("消息分片范围无效")
		}
		part := c.fragments[id]
		if part == nil {
			if offset != 0 || len(c.fragments) >= 64 || c.fragmentBytes+total > 34*1024*1024 {
				c.Close()
				return protocol.Message{}, fmt.Errorf("待接收消息数量或长度超限")
			}
			part = &fragment{total: total, body: make([]byte, 0, total)}
			c.fragments[id] = part
			c.fragmentBytes += total
		}
		if part.total != total || len(part.body) != offset {
			c.Close()
			return protocol.Message{}, fmt.Errorf("消息分片顺序无效")
		}
		part.body = append(part.body, data[16:]...)
		if len(part.body) == total {
			delete(c.fragments, id)
			c.fragmentBytes -= total
			message, e := protocol.Decode(part.body)
			if e != nil {
				c.Close()
			}
			return message, e
		}
	}
}

func (c *Channel) readRecord(ctx context.Context) ([]byte, error) {
	data, err := readBinary(ctx, c.socket)
	if err != nil {
		c.Close()
		return nil, err
	}
	if len(data) > RecordBytes+16 {
		c.Close()
		return nil, fmt.Errorf("加密记录超限")
	}
	plain, err := c.receive.Decrypt(nil, nil, data)
	if err != nil || len(plain) == 0 {
		c.Close()
		return nil, fmt.Errorf("加密记录无效或不完整")
	}
	return plain, nil
}
