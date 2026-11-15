package operations

import (
	"context"
	"errors"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
	"path/filepath"
	"time"
)

// 原生锁只覆盖一次规则读取或改写，确认等待继续保留资源责任。
type lockedFirewall struct {
	firewall.Adapter
	lock func(context.Context) (func(), error)
}

func (a lockedFirewall) Read(ctx context.Context) (protocol.FirewallState, error) {
	unlock, err := a.lock(ctx)
	if err != nil {
		return protocol.FirewallState{}, err
	}
	defer unlock()
	return a.Adapter.Read(ctx)
}
func (a lockedFirewall) Write(ctx context.Context, s firewall.Step, reverse bool) error {
	unlock, err := a.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return a.Adapter.Write(ctx, s, reverse)
}
func NativeLock(directory string) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			unlock, err := localstate.Lock(filepath.Join(directory, "operation.lock"))
			if !errors.Is(err, localstate.ErrBusy) {
				return unlock, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
	}
}
