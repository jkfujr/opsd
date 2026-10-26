package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
)

const RecordBytes = 60 * 1024

var ErrAuthentication = errors.New("身份或协议不匹配")

type Channel struct {
	socket          *websocket.Conn
	send, receive   *noise.CipherState
	writeMu, readMu sync.Mutex
	PeerPublic      []byte
}

func Handshake(ctx context.Context, socket *websocket.Conn, key identity.Key, peer []byte, initiator bool) (*Channel, error) {
	socket.SetReadLimit(65535)
	state, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:     noise.HandshakeIK, Initiator: initiator, Prologue: []byte("OPSD/node/v1"),
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
	channel := &Channel{socket: socket, send: first, receive: second, PeerPublic: append([]byte(nil), state.PeerStatic()...)}
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	plain := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(plain, uint32(len(data)))
	copy(plain[4:], data)
	for len(plain) > 0 {
		size := min(RecordBytes, len(plain))
		record, err := c.send.Encrypt(nil, nil, plain[:size])
		if err != nil {
			c.Close()
			return err
		}
		if err := c.socket.Write(ctx, websocket.MessageBinary, record); err != nil {
			c.Close()
			return err
		}
		plain = plain[size:]
	}
	return nil
}

func (c *Channel) Receive(ctx context.Context) (protocol.Message, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	data, err := c.readRecord(ctx)
	if err != nil {
		return protocol.Message{}, err
	}
	if len(data) < 4 {
		c.Close()
		return protocol.Message{}, fmt.Errorf("消息长度前缀不完整")
	}
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size <= 0 || size > protocol.MaxMessageBytes {
		c.Close()
		return protocol.Message{}, fmt.Errorf("消息长度超限")
	}
	body := make([]byte, 0, size)
	body = append(body, data[4:]...)
	for len(body) < size {
		data, err = c.readRecord(ctx)
		if err != nil {
			return protocol.Message{}, err
		}
		body = append(body, data...)
	}
	if len(body) != size {
		c.Close()
		return protocol.Message{}, fmt.Errorf("消息记录长度不匹配")
	}
	message, err := protocol.Decode(body)
	if err != nil {
		c.Close()
	}
	return message, err
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
