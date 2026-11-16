package metricsstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"

	"github.com/golang/snappy"
	"google.golang.org/protobuf/encoding/protowire"
)

func wireBytes(dst []byte, n protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(dst, n, protowire.BytesType), value)
}

// RemoteWrite 编码标准 prometheus.WriteRequest，标签保持字典序。
func RemoteWrite(cluster, node string, batches []Batch) []byte {
	series := map[string][]Sample{}
	for _, b := range batches {
		for _, s := range b.Samples {
			key := s.Metric + "\x00" + s.ResourceID + "\x00" + s.Backend
			series[key] = append(series[key], s)
		}
	}
	keys := make([]string, 0, len(series))
	for k := range series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var request []byte
	for _, key := range keys {
		samples := series[key]
		first := samples[0]
		labels := map[string]string{"__name__": first.Metric, "cluster_id": cluster, "node_id": node}
		if first.ResourceID != "" {
			labels["resource_id"] = first.ResourceID
		}
		if first.Backend != "" {
			labels["backend"] = first.Backend
		}
		names := []string{}
		for name := range labels {
			names = append(names, name)
		}
		sort.Strings(names)
		var ts []byte
		for _, name := range names {
			label := wireBytes(nil, 1, []byte(name))
			label = wireBytes(label, 2, []byte(labels[name]))
			ts = wireBytes(ts, 1, label)
		}
		for _, s := range samples {
			sample := protowire.AppendFixed64(protowire.AppendTag(nil, 1, protowire.Fixed64Type), math.Float64bits(s.Value))
			sample = protowire.AppendVarint(protowire.AppendTag(sample, 2, protowire.VarintType), uint64(s.At))
			ts = wireBytes(ts, 2, sample)
		}
		request = wireBytes(request, 1, ts)
	}
	return snappy.Encode(nil, request)
}

type WriteError struct {
	Status  int
	Message string
}

func (e *WriteError) Error() string { return e.Message }
func (e *WriteError) Permanent() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != 408 && e.Status != 429
}

func Write(ctx context.Context, client *http.Client, endpoint, cluster, node string, batches []Batch) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(RemoteWrite(cluster, node, batches)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("THANOS-TENANT", "opsd")
	r, err := client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return err
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return &WriteError{Status: r.StatusCode, Message: fmt.Sprintf("Receive 返回 %s：%s", strconv.Itoa(r.StatusCode), string(data))}
	}
	return nil
}
