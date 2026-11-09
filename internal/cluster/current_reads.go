package cluster

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type ReadStatus = protocol.ReadStatus

type currentRead struct {
	value   json.RawMessage
	status  ReadStatus
	used    time.Time
	running bool
}

// CurrentRead 返回已有结果；活动页面共享刷新，各目标的期限从取得读取名额后开始。
func (n *Node) CurrentRead(ctx context.Context, target, method string, body, out any, interval time.Duration) ReadStatus {
	b, _ := json.Marshal(body)
	key := target + "/" + method + "/" + string(b)
	n.mu.Lock()
	if n.current == nil {
		n.current = map[string]*currentRead{}
		n.readSlots = make(chan struct{}, 8)
	}
	entry := n.current[key]
	if entry == nil {
		if len(n.current) >= 4096 {
			for k, v := range n.current {
				if !v.running && n.Now().Sub(v.used) > time.Minute {
					delete(n.current, k)
				}
			}
			if len(n.current) >= 4096 {
				n.mu.Unlock()
				return ReadStatus{State: "error", Error: &protocol.Error{Code: "node_busy", Message: "读取资源过多"}}
			}
		}
		entry = &currentRead{status: ReadStatus{State: "queued"}}
		n.current[key] = entry
	}
	entry.used = n.Now()
	status, value := entry.status, entry.value
	if status.DataAt != nil && n.Now().Sub(*status.DataAt) > interval*2 {
		status.State = "stale"
	}
	if !entry.running && !n.closed && n.ctx.Err() == nil {
		entry.running = true
		n.wg.Add(1)
		go n.refreshCurrent(entry, target, method, b, interval)
	}
	n.mu.Unlock()
	if len(value) > 0 && out != nil {
		_ = json.Unmarshal(value, out)
	}
	return status
}

func (n *Node) refreshCurrent(entry *currentRead, target, method string, body json.RawMessage, interval time.Duration) {
	defer n.wg.Done()
	defer func() { n.mu.Lock(); entry.running = false; n.mu.Unlock() }()
	for {
		select {
		case n.readSlots <- struct{}{}:
		case <-n.ctx.Done():
			return
		}
		n.mu.Lock()
		at := n.Now().UTC()
		entry.status.AttemptedAt = &at
		if entry.value == nil {
			entry.status.State = "reading"
		}
		n.mu.Unlock()
		ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		var result json.RawMessage
		err := n.Call(ctx, target, method, body, &result)
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		<-n.readSlots
		n.mu.Lock()
		if err == nil {
			completed := n.Now().UTC()
			entry.value = result
			entry.status = ReadStatus{State: "ready", DataAt: &completed, AttemptedAt: &at}
		} else {
			entry.status.State = "error"
			if !n.connectedLocked(target) {
				entry.status.State = "offline"
			}
			entry.status.Error = wireError(err)
			if timedOut {
				entry.status.State = "timeout"
			}
		}
		n.mu.Unlock()
		timer := time.NewTimer(interval)
		select {
		case <-n.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		n.mu.Lock()
		active := n.Now().Sub(entry.used) <= interval*2+5*time.Second
		n.mu.Unlock()
		if !active {
			return
		}
	}
}

func (n *Node) connectedLocked(id string) bool {
	return id == n.ID || n.connections[id] != nil || (!n.seen[id].IsZero() && n.Now().Sub(n.seen[id]) < 15*time.Second)
}
