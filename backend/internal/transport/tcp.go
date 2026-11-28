package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
)

// TCPPreface 的首字节与 HTTP 方法区分，末字节标明记录版本。
const TCPPreface = "\x00OPSD\x01"

func HandshakeTCP(ctx context.Context, conn net.Conn, key identity.Key, peer []byte, initiator bool) (*Channel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	records := tcpRecords{conn}
	stop := context.AfterFunc(ctx, records.close)
	defer stop()
	if initiator {
		if _, err := io.WriteString(conn, TCPPreface); err != nil {
			records.close()
			return nil, err
		}
	} else {
		var preface [len(TCPPreface)]byte
		if _, err := io.ReadFull(conn, preface[:]); err != nil || string(preface[:]) != TCPPreface {
			records.close()
			return nil, ErrAuthentication
		}
	}
	channel, err := handshake(ctx, records, key, peer, initiator)
	if err != nil {
		records.close()
	}
	return channel, err
}

type tcpRecords struct{ net.Conn }

func (r tcpRecords) read(ctx context.Context) ([]byte, error) {
	stop := context.AfterFunc(ctx, r.close)
	defer stop()
	var header [2]byte
	if _, err := io.ReadFull(r.Conn, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(header[:]))
	if size == 0 || size > RecordBytes+16 {
		return nil, fmt.Errorf("TCP 加密记录长度无效")
	}
	record := make([]byte, size)
	_, err := io.ReadFull(r.Conn, record)
	return record, err
}

func (r tcpRecords) write(ctx context.Context, data []byte) error {
	if len(data) == 0 || len(data) > RecordBytes+16 {
		return fmt.Errorf("TCP 加密记录长度无效")
	}
	stop := context.AfterFunc(ctx, r.close)
	defer stop()
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(data)))
	buffers := net.Buffers{header[:], data}
	_, err := buffers.WriteTo(r.Conn)
	return err
}

func (r tcpRecords) close() { _ = r.Conn.Close() }
