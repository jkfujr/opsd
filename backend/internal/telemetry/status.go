package telemetry

import (
	"context"
	"path/filepath"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

type runtime struct {
	Config        metricsstore.Config
	Host          *protocol.MonitoringResult
	Resources     *protocol.ResourceMetrics
	LastUpload    time.Time
	LastReceive   time.Time
	SamplingError string
	UploadError   string
	ReceiveError  string
	Paused        bool
	Deployment    protocol.MonitoringState
}
type Status struct {
	NodeID        string                    `json:"node_id"`
	Config        metricsstore.Config       `json:"config"`
	Buffer        metricsstore.BufferStatus `json:"buffer"`
	LastSample    *time.Time                `json:"last_sample,omitempty"`
	LastUpload    *time.Time                `json:"last_upload,omitempty"`
	LastReceive   *time.Time                `json:"last_receive,omitempty"`
	SamplingError string                    `json:"sampling_error,omitempty"`
	UploadError   string                    `json:"upload_error,omitempty"`
	ReceiveError  string                    `json:"receive_error,omitempty"`
	Paused        bool                      `json:"paused"`
	Streams       int                       `json:"streams"`
	Deployment    protocol.MonitoringState  `json:"deployment"`
}

func (s *Service) Buffer(ctx context.Context) (*metricsstore.Buffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	if s.buffer == nil {
		b, err := metricsstore.Open(ctx, filepath.Join(s.directory, "metrics.sqlite"))
		if err != nil {
			return nil, err
		}
		s.buffer = b
	}
	return s.buffer, nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	r := s.runtime
	count := len(s.streams)
	s.mu.Unlock()
	out := Status{NodeID: s.NodeID, Config: r.Config, SamplingError: r.SamplingError, UploadError: r.UploadError, ReceiveError: r.ReceiveError, Paused: r.Paused, Streams: count, Deployment: r.Deployment, Buffer: metricsstore.BufferStatus{Gaps: []metricsstore.Gap{}}}
	if out.Config.LocalDays == 0 {
		var err error
		out.Config, err = s.localConfig(ctx)
		if err != nil {
			return out, err
		}
	}
	if out.Deployment.Components == nil {
		out.Deployment.Components = []protocol.MonitoringComponent{}
	}
	if r.Host != nil {
		at := r.Host.Metrics.CollectedAt
		out.LastSample = &at
	}
	if !r.LastUpload.IsZero() {
		out.LastUpload = &r.LastUpload
	}
	if !r.LastReceive.IsZero() {
		out.LastReceive = &r.LastReceive
	}
	buffer, err := s.Buffer(ctx)
	if err != nil {
		return out, err
	}
	out.Buffer, err = buffer.Status(ctx, s.NodeID)
	return out, err
}

func (s *Service) CurrentHost() *protocol.MonitoringResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	host := s.runtime.Host
	if s.runtime.Config.Enabled && host != nil && s.Now().Sub(host.Metrics.CollectedAt) >= 0 && s.Now().Sub(host.Metrics.CollectedAt) < 1500*time.Millisecond {
		return host
	}
	return nil
}

func (s *Service) CurrentDocker() *protocol.DockerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	resources := s.runtime.Resources
	if s.runtime.Config.Enabled && resources != nil && resources.Docker != nil && s.Now().Sub(resources.Docker.CheckedAt) >= 0 && s.Now().Sub(resources.Docker.CheckedAt) < 1500*time.Millisecond {
		return resources.Docker
	}
	return nil
}
