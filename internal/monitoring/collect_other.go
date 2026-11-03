//go:build !linux

package monitoring

import (
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func collect() (protocol.Metrics, counters) {
	return protocol.Metrics{CollectedAt: time.Now().UTC(), Errors: []string{"此系统不支持资源采集"}}, counters{}
}
