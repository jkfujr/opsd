package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Upload struct {
	Batches []metricsstore.Batch `json:"batches"`
	Gaps    []metricsstore.Gap   `json:"gaps"`
}
type archiveAck = metricsstore.Receipt
type archiveInventory struct {
	Samples []string  `json:"samples"`
	At      time.Time `json:"at"`
}

func (s *Service) Dispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	switch method {
	case "archive.config":
		return s.Config(ctx)
	case "archive.status":
		return s.Status(ctx)
	case "archive.inventory":
		buffer, err := s.Buffer(ctx)
		if err != nil {
			return nil, err
		}
		samples, err := buffer.Inventory(ctx, s.Now())
		return archiveInventory{Samples: samples, At: s.Now().UTC()}, err
	case "archive.series":
		var q metricsstore.Query
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		return s.Query(ctx, q)
	}
	return nil, s.Errors.New("invalid_request", "未知监控请求")
}

func (s *Service) sampleLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		enabled := s.runtime.Config.Enabled
		s.mu.Unlock()
		if !enabled || s.Peer.Revoked(s.ctx, s.NodeID) {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 900*time.Millisecond)
		buffer, err := s.Buffer(ctx)
		if err == nil {
			err = s.Sample(ctx, buffer)
		}
		cancel()
		s.mu.Lock()
		s.runtime.SamplingError = ""
		if err != nil {
			s.runtime.SamplingError = err.Error()
		}
		s.mu.Unlock()
	}
}
func (s *Service) Sample(ctx context.Context, buffer *metricsstore.Buffer) error {
	var host protocol.MonitoringResult
	var resources protocol.ResourceMetrics
	var hostErr, resourceErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); host, hostErr = s.Source.Host(ctx, "archive") }()
	go func() { defer wg.Done(); resources, resourceErr = s.Source.Resources(ctx) }()
	wg.Wait()
	at := s.Now().UTC()
	if hostErr == nil && !host.Metrics.CollectedAt.IsZero() {
		at = host.Metrics.CollectedAt
	}
	b := metricsstore.Batch{At: at.UnixMilli(), Samples: []metricsstore.Sample{}, Errors: []string{}}
	emit := func(name, resource, backend string, v float64) {
		b.Samples = append(b.Samples, metricsstore.Sample{Metric: name, ResourceID: resource, Backend: backend, At: b.At, Value: v})
	}
	if hostErr != nil {
		b.Errors = append(b.Errors, "host: "+hostErr.Error())
	} else {
		m := host.Metrics
		emit("opsd_node_up", "", "", 1)
		emit("opsd_node_cpu_cores", "", "", float64(m.CPUCores))
		for name, v := range map[string]*float64{"cpu_usage_percent": m.CPUUsage, "network_receive_bytes_per_second": m.NetworkRX, "network_transmit_bytes_per_second": m.NetworkTX} {
			if v != nil {
				emit("opsd_node_"+name, "", "", *v)
			}
		}
		for name, v := range map[string]*uint64{"memory_used_bytes": m.MemoryUsed, "memory_total_bytes": m.MemoryTotal, "disk_used_bytes": m.DiskUsed, "disk_total_bytes": m.DiskTotal} {
			if v != nil {
				emit("opsd_node_"+name, "", "", float64(*v))
			}
		}
		b.Errors = append(b.Errors, m.Errors...)
	}
	if resourceErr != nil {
		b.Errors = append(b.Errors, "resources: "+resourceErr.Error())
	} else {
		for _, m := range resources.Metrics {
			emit(m.Metric, m.ResourceID, m.Backend, m.Value)
		}
		b.Errors = append(b.Errors, resources.Errors...)
	}
	if len(b.Errors) > 32 {
		b.Errors = b.Errors[:32]
	}
	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer writeCancel()
	appended, err := buffer.Append(writeCtx, b, s.NodeID, s.Now(), metricsstore.BufferBytes)
	if err != nil {
		return err
	}
	if appended.Sequence == 0 {
		return fmt.Errorf("采集时钟发生变化，已保留数据缺口")
	}
	s.mu.Lock()
	if hostErr == nil {
		s.runtime.Host = &host
	}
	if resourceErr == nil {
		s.runtime.Resources = &resources
	}
	s.mu.Unlock()
	if len(b.Errors) > 0 {
		return errors.New(strings.Join(b.Errors, "；"))
	}
	return nil
}

func CallStream(ctx context.Context, channel Stream, method string, input, out any, newError func(string, string) error) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	id := identity.Random()
	if err = channel.Send(ctx, protocol.Message{RequestID: id, Payload: &protocol.PeerCall{Method: method, Body: data}}); err != nil {
		return err
	}
	message, err := channel.Receive(ctx)
	if err != nil {
		return err
	}
	reply, ok := message.Payload.(*protocol.PeerReply)
	if !ok || message.ReplyTo != id {
		return fmt.Errorf("监控流应答无效")
	}
	if reply.Error != nil {
		return newError(reply.Error.Code, reply.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(reply.Body, out)
	}
	return nil
}

func (s *Service) uploadLoop() {
	var channel Stream
	var target string
	closeChannel := func() {
		if channel != nil {
			channel.Close()
			channel = nil
		}
	}
	defer closeChannel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	backoff := time.Duration(0)
	var next time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		config := s.runtime.Config
		s.mu.Unlock()
		if !config.Enabled || s.Peer.Revoked(s.ctx, s.NodeID) {
			closeChannel()
			continue
		}
		if target != config.NodeID {
			closeChannel()
			target = config.NodeID
			backoff = 0
		}
		if s.Now().Before(next) {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		buffer, err := s.Buffer(ctx)
		if err == nil && channel == nil && target != s.NodeID {
			channel, err = s.Peer.OpenStream(ctx, target)
		}
		call := func(method string, in, out any) error {
			if target == s.NodeID {
				value, e := s.receive(ctx, s.NodeID, method, in, nil)
				if e != nil {
					return e
				}
				if out != nil {
					data, _ := json.Marshal(value)
					return json.Unmarshal(data, out)
				}
				return nil
			}
			return CallStream(ctx, channel, method, in, out, s.Errors.New)
		}
		if err == nil {
			// 补传每轮限制在 20 秒内，持续采样使用独立循环。
			for round := 0; round < 12 && ctx.Err() == nil; round++ {
				batches, e := buffer.Pending(ctx, s.NodeID, s.Now())
				if e != nil {
					err = e
					break
				}
				if len(batches) == 0 {
					err = call("ping", nil, nil)
					break
				}
				ids := make([]string, len(batches))
				for i, b := range batches {
					ids[i] = b.ID
				}
				var receipts []archiveAck
				if err = call("receipts", ids, &receipts); err != nil {
					break
				}
				known := map[string]archiveAck{}
				for _, r := range receipts {
					known[r.ID] = r
				}
				pending := []metricsstore.Batch{}
				for _, b := range batches {
					if receipt, ok := known[b.ID]; ok {
						if receipt.Digest != metricsstore.Digest(b) {
							err = s.Errors.New("invalid_batch", "批次回执与原始内容不同")
							break
						}
						if receipt.Error != "" {
							err = buffer.Reject(ctx, s.NodeID, b, receipt.Error)
						} else {
							err = buffer.Acknowledge(ctx, b)
						}
						if err != nil {
							break
						}
					} else {
						pending = append(pending, b)
					}
				}
				if err != nil {
					break
				}
				if len(pending) == 0 {
					continue
				}
				status, e := buffer.Status(ctx, s.NodeID)
				if e != nil {
					err = e
					break
				}
				var acks []archiveAck
				err = call("upload", Upload{Batches: pending, Gaps: status.Gaps}, &acks)
				if err != nil {
					break
				}
				confirmed := map[string]archiveAck{}
				for _, ack := range acks {
					confirmed[ack.ID] = ack
				}
				for _, b := range pending {
					receipt := confirmed[b.ID]
					if receipt.Digest != metricsstore.Digest(b) {
						err = fmt.Errorf("监控接收缺少原批次回执")
						break
					}
					if receipt.Error != "" {
						err = buffer.Reject(ctx, s.NodeID, b, receipt.Error)
					} else {
						err = buffer.Acknowledge(ctx, b)
					}
					if err != nil {
						break
					}
				}
				if err != nil {
					break
				}
				s.mu.Lock()
				s.runtime.LastUpload = s.Now().UTC()
				s.mu.Unlock()
				if len(batches) < 5 {
					break
				}
			}
		}
		cancel()
		s.mu.Lock()
		s.runtime.UploadError = ""
		if err != nil {
			s.runtime.UploadError = err.Error()
		}
		s.mu.Unlock()
		if err != nil {
			closeChannel()
			if backoff == 0 {
				backoff = 5 * time.Second
			} else {
				backoff = min(backoff*2, time.Minute)
			}
			next = s.Now().Add(backoff)
		} else {
			backoff = 0
			next = time.Time{}
		}
	}
}

func (s *Service) Serve(ctx context.Context, origin string, channel Stream) {
	s.mu.Lock()
	if s.closed || s.streams[origin] || len(s.streams) >= 128 {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	defer s.wg.Done()
	s.streams[origin] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.streams, origin)
		s.mu.Unlock()
		channel.Close()
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	var inventory archiveInventory
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 45*time.Second)
		message, err := channel.Receive(readCtx)
		readCancel()
		if err != nil {
			return
		}
		call, ok := message.Payload.(*protocol.PeerCall)
		if !ok || !s.Peer.Active(ctx, origin) || s.Peer.Revoked(ctx, s.NodeID) {
			return
		}
		requestCtx, requestCancel := context.WithTimeout(ctx, 15*time.Second)
		var input any
		switch call.Method {
		case "ping":
		case "receipts":
			var ids []string
			err = json.Unmarshal(call.Body, &ids)
			input = ids
		case "upload":
			var batch Upload
			err = json.Unmarshal(call.Body, &batch)
			input = batch
		default:
			err = s.Errors.New("invalid_request", "监控数据通道不接受此方法")
		}
		var value any
		if err == nil {
			value, err = s.receive(requestCtx, origin, call.Method, input, &inventory)
		}
		reply := &protocol.PeerReply{}
		if err != nil {
			reply.Error = s.Errors.Wire(err)
		} else {
			reply.Body, _ = json.Marshal(value)
		}
		err = channel.Send(requestCtx, protocol.Message{RequestID: identity.Random(), ReplyTo: message.RequestID, Payload: reply})
		requestCancel()
		if err != nil {
			return
		}
	}
}

func (s *Service) receive(ctx context.Context, origin, method string, input any, inventory *archiveInventory) (any, error) {
	s.mu.Lock()
	config := s.runtime.Config
	paused := s.runtime.Paused
	s.mu.Unlock()
	if !config.Enabled || config.NodeID != s.NodeID || !s.Peer.Active(ctx, origin) || s.Peer.Revoked(ctx, s.NodeID) {
		return nil, s.Errors.New("forbidden", "当前节点未接收此来源的监控数据")
	}
	buffer, err := s.Buffer(ctx)
	if err != nil {
		return nil, err
	}
	if method == "ping" {
		return struct{}{}, nil
	}
	if method == "receipts" {
		ids, ok := input.([]string)
		if !ok || len(ids) > 5 {
			return nil, s.Errors.New("invalid_batch", "回执查询过大")
		}
		out := []archiveAck{}
		for _, id := range ids {
			receipt, e := buffer.Receipt(ctx, origin, id)
			if e == nil {
				out = append(out, receipt)
			} else if e != sql.ErrNoRows {
				return nil, e
			}
		}
		return out, nil
	}
	if method != "upload" {
		return nil, s.Errors.New("invalid_request", "未知监控流方法")
	}
	upload, ok := input.(Upload)
	if !ok || len(upload.Batches) > 5 || len(upload.Gaps) > 100 {
		return nil, s.Errors.New("invalid_batch", "监控上传过大")
	}
	if paused {
		return nil, s.Errors.New("capacity_exceeded", "监控磁盘已暂停接收")
	}
	for _, gap := range upload.Gaps {
		if gap.NodeID != origin || gap.From <= 0 || gap.To < gap.From || gap.To > s.Now().Add(time.Minute).UnixMilli() || len(gap.Reason) > 1024 {
			return nil, s.Errors.New("invalid_batch", "监控缺口来源或时间无效")
		}
	}
	select {
	case s.writeSlots <- struct{}{}:
		defer func() { <-s.writeSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := []archiveAck{}
	pending := []metricsstore.Batch{}
	var previous int64
	for _, b := range upload.Batches {
		if len(b.ID) != 43 || b.Sequence <= previous {
			return nil, s.Errors.New("invalid_batch", "监控批次顺序无效")
		}
		previous = b.Sequence
		digest := metricsstore.Digest(b)
		if receipt, e := buffer.Receipt(ctx, origin, b.ID); e == nil {
			if receipt.Digest != digest {
				return nil, s.Errors.New("invalid_batch", "批次标识内容冲突")
			}
			out = append(out, receipt)
			continue
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		if receipt, e := buffer.SequenceReceipt(ctx, origin, b.Sequence); e == nil && receipt.ID != b.ID {
			return nil, s.Errors.New("invalid_batch", "批次序号已用于其他标识")
		} else if e != nil && e != sql.ErrNoRows {
			return nil, e
		}
		err = b.Validate(s.Now())
		if err == nil {
			err = s.validateResources(ctx, origin, b, inventory)
		}
		if err != nil {
			if code := s.Errors.Code(err); code != "" && code != "invalid_batch" {
				return nil, err
			}
			if e := buffer.Received(ctx, origin, b, err.Error()); e != nil {
				return nil, e
			}
			out = append(out, archiveAck{ID: b.ID, Digest: digest, Error: err.Error()})
			continue
		}
		pending = append(pending, b)
	}
	if len(pending) > 0 {
		write := func(batches []metricsstore.Batch) error {
			return metricsstore.Write(ctx, &http.Client{Timeout: 10 * time.Second}, "http://127.0.0.1:19291/api/v1/receive", s.ClusterID(), origin, batches)
		}
		writeErr := write(pending)
		if writeErr != nil {
			var e *metricsstore.WriteError
			if !errors.As(writeErr, &e) || !e.Permanent() {
				return nil, s.Errors.New("dependency_unavailable", writeErr.Error())
			}
		}
		for _, b := range pending {
			rejected := ""
			if writeErr != nil {
				// 拆开永久失败的批次，保留同次上传中已接收的样本。
				if e := write([]metricsstore.Batch{b}); e != nil {
					var permanent *metricsstore.WriteError
					if !errors.As(e, &permanent) || !permanent.Permanent() {
						return nil, s.Errors.New("dependency_unavailable", e.Error())
					}
					rejected = e.Error()
				}
			}
			if err = buffer.Received(ctx, origin, b, rejected); err != nil {
				return nil, err
			}
			out = append(out, archiveAck{ID: b.ID, Digest: metricsstore.Digest(b), Error: rejected})
		}
	}
	for _, gap := range upload.Gaps {
		if err = buffer.Gap(ctx, gap); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.runtime.LastReceive = s.Now().UTC()
	s.mu.Unlock()
	return out, nil
}

func (s *Service) validateResources(ctx context.Context, origin string, b metricsstore.Batch, inventory *archiveInventory) error {
	keys := []string{}
	for _, s := range b.Samples {
		if metricsstore.MetricModules[s.Metric] != "monitoring" && s.Metric != "opsd_docker_up" {
			keys = append(keys, s.Metric+"/"+s.ResourceID+"/"+s.Backend)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if inventory == nil {
		inventory = &archiveInventory{}
	}
	missing := func() bool {
		for _, key := range keys {
			if !slices.Contains(inventory.Samples, key) {
				return true
			}
		}
		return false
	}
	if s.Now().Sub(inventory.At) > 15*time.Second || missing() {
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := s.Peer.Call(readCtx, origin, "archive.inventory", nil, inventory); err != nil {
			return s.Errors.New("dependency_unavailable", "资源归属读取失败")
		}
	}
	if missing() {
		return s.Errors.New("invalid_batch", "样本资源不属于认证来源节点")
	}
	return nil
}
