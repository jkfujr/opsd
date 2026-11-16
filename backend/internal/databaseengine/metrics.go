package databaseengine

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type metricSnapshot struct {
	at     time.Time
	status map[string]string
	pools  []map[string]string
}

var metricSnapshots = struct {
	sync.Mutex
	values map[string]metricSnapshot
}{values: map[string]metricSnapshot{}}

func rememberMetrics(c protocol.DatabaseConnection, snapshot metricSnapshot) {
	metricSnapshots.Lock()
	defer metricSnapshots.Unlock()
	for key, old := range metricSnapshots.values {
		if snapshot.at.Sub(old.at) > 1500*time.Millisecond {
			delete(metricSnapshots.values, key)
		}
	}
	if len(metricSnapshots.values) >= 32 {
		var oldest string
		for key, value := range metricSnapshots.values {
			if oldest == "" || value.at.Before(metricSnapshots.values[oldest].at) {
				oldest = key
			}
		}
		delete(metricSnapshots.values, oldest)
	}
	metricSnapshots.values[fingerprint(c)] = snapshot
}

func recentMetrics(c protocol.DatabaseConnection) (metricSnapshot, bool) {
	metricSnapshots.Lock()
	defer metricSnapshots.Unlock()
	snapshot, ok := metricSnapshots.values[fingerprint(c)]
	age := time.Since(snapshot.at)
	return snapshot, ok && age >= 0 && age < 1500*time.Millisecond
}

// Metrics 读取运行计数，不访问账号、表结构或配置列表。
func (Manager) Metrics(ctx context.Context, id string, c protocol.DatabaseConnection) ([]protocol.ResourceMetric, error) {
	out := []protocol.ResourceMetric{}
	emit := func(name, backend string, value float64) {
		out = append(out, protocol.ResourceMetric{Metric: name, ResourceID: id, Backend: backend, Value: value})
	}
	s, err := connect(ctx, c)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if c.Kind == "proxysql" {
		values, err := rows(ctx, s, "SELECT Variable_Name,Variable_Value FROM stats_mysql_global WHERE Variable_Name IN ('Client_Connections_connected','Server_Connections_connected','Questions')")
		if err != nil {
			return nil, err
		}
		emit("opsd_proxysql_up", "", 1)
		names := map[string]string{"client_connections_connected": "opsd_proxysql_connections", "server_connections_connected": "opsd_proxysql_server_connections", "questions": "opsd_proxysql_queries_total"}
		for _, v := range values {
			if name := names[strings.ToLower(v["variable_name"])]; name != "" {
				value, err := strconv.ParseFloat(v["variable_value"], 64)
				if err != nil {
					return nil, err
				}
				emit(name, "", value)
			}
		}
		values, err = rows(ctx, s, proxyMetricsQuery)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			backend := v["hostgroup"] + "/" + v["srv_host"] + ":" + v["srv_port"]
			for field, name := range map[string]string{"connused": "opsd_proxysql_pool_used", "connfree": "opsd_proxysql_pool_free"} {
				value, err := strconv.ParseFloat(v[field], 64)
				if err != nil {
					return nil, err
				}
				emit(name, backend, value)
			}
		}
		rememberMetrics(c, metricSnapshot{at: time.Now().UTC(), pools: values})
		return out, nil
	}
	values, err := status(ctx, s)
	if err != nil {
		return nil, err
	}
	up := 0.0
	if values["wsrep_ready"] == "ON" && values["wsrep_cluster_status"] == "Primary" && values["wsrep_local_state_comment"] == "Synced" {
		up = 1
	}
	emit("opsd_database_up", "", up)
	for field, name := range map[string]string{"threads_connected": "opsd_database_connections", "wsrep_cluster_size": "opsd_galera_members", "wsrep_local_recv_queue": "opsd_galera_receive_queue", "wsrep_local_send_queue": "opsd_galera_send_queue", "wsrep_flow_control_paused": "opsd_galera_flow_control_ratio"} {
		if raw, ok := values[field]; ok {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, err
			}
			emit(name, "", v)
		}
	}
	rememberMetrics(c, metricSnapshot{at: time.Now().UTC(), status: values})
	return out, nil
}

const proxyMetricsQuery = "SELECT hostgroup,srv_host,srv_port,status,ConnUsed,ConnFree,MaxConnUsed,ConnERR,Queries,Latency_us FROM stats_mysql_connection_pool ORDER BY hostgroup,srv_host,srv_port"
