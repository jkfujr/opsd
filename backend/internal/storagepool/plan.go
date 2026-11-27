package storagepool

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

func Fault(code, message string) error {
	return &firewall.Fault{Code: code, Message: message}
}

func Normalize(in protocol.PoolInput) (protocol.PoolInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 64 || in.PortOffset < 0 || in.PortOffset > 16_000 || len(in.Nodes) < 3 || len(in.Nodes) > 64 {
		return in, Fault("invalid_request", "名称须为 1–64 个字符，成员须为 3–64 个，端口偏移须为 0–16000")
	}
	if in.ECEnabled {
		return in, Fault("invalid_request", "当前版本只支持跨故障域双副本")
	}
	in.Nodes = slices.Clone(in.Nodes)
	seen := make(map[string]bool, len(in.Nodes))
	for i, m := range in.Nodes {
		ip, err := netip.ParseAddr(m.PrivateIP)
		if err != nil || ip.Zone() != "" || !IsPrivate(ip.Unmap()) || !operations.ValidID(m.NodeID) ||
			seen[m.NodeID] || m.CapacityGiB < 1 || m.CapacityGiB > (1<<33)-1 ||
			(m.StorageRootID != "" && !operations.ValidID(m.StorageRootID)) || len(m.FailureDomain) > 120 {
			return in, Fault("invalid_request", "成员、私网 IP、容量、存储根或故障域无效")
		}
		if m.CapacityGiB < protocol.PoolMinCapacityGiB {
			return in, Fault("insufficient_storage", fmt.Sprintf("成员 %s 的 B 数据卷至少需要 6 GiB，普通成员总配额至少 %d GiB，Master／etcd 至少 %d GiB",
				m.NodeID, protocol.PoolMinCapacityGiB, protocol.PoolMinControlCapacityGiB))
		}
		seen[m.NodeID] = true
		m.PrivateIP = ip.Unmap().String()
		m.FailureDomain = strings.TrimSpace(m.FailureDomain)
		if m.StorageRootID == "" {
			m.StorageRootID = "default"
		}
		in.Nodes[i] = m
	}
	slices.SortFunc(in.Nodes, func(a, b protocol.PoolMemberInput) int {
		if a.CapacityGiB != b.CapacityGiB {
			if a.CapacityGiB > b.CapacityGiB {
				return -1
			}
			return 1
		}
		return strings.Compare(a.NodeID, b.NodeID)
	})
	return in, nil
}

var sharedIP = netip.MustParsePrefix("100.64.0.0/10")

func IsPrivate(ip netip.Addr) bool { return ip.IsPrivate() || sharedIP.Contains(ip) }

func Address(ip string, port int) string { return net.JoinHostPort(ip, strconv.Itoa(port)) }

func Ports(offset int) protocol.PoolPorts {
	return protocol.PoolPorts{S3: 8333 + offset, S3GRPC: 18333 + offset,
		Volume: 8080 + offset, VolumeGRPC: 18080 + offset, Filer: 8888 + offset, FilerGRPC: 18888 + offset,
		Master: 9333 + offset, MasterGRPC: 19333 + offset, EtcdClient: 2379 + offset, EtcdPeer: 2380 + offset}
}

func PortGroups(p protocol.PoolPorts) ([]int, []int) {
	return []int{p.S3, p.S3GRPC, p.Volume, p.VolumeGRPC, p.Filer, p.FilerGRPC},
		[]int{p.Master, p.MasterGRPC, p.EtcdClient, p.EtcdPeer}
}

func Quotas(capacity int64, control bool) (data, meta int64) {
	minimum := 256 * protocol.PoolMiB
	if control {
		minimum = 6 * protocol.PoolGiB
	}
	meta = max(minimum, ((capacity-1)/(10*protocol.PoolMiB)+1)*protocol.PoolMiB)
	return capacity - meta, meta
}

func Enough(a, b protocol.PoolDisk, control, data int64) bool {
	if data < protocol.PoolMinDataBytes || a.UUID == "" || b.UUID == "" {
		return false
	}
	if a.UUID == b.UUID {
		return min(a.AvailableBytes, b.AvailableBytes)-max(a.ReserveBytes, b.ReserveBytes)-max(a.ArchiveBytes, b.ArchiveBytes)-control >= data
	}
	return a.AvailableBytes-a.ReserveBytes-a.ArchiveBytes >= control && b.AvailableBytes-b.ReserveBytes-b.ArchiveBytes >= data
}

func CapacityMessage(node string, a, b protocol.PoolDisk, control, data int64, controller bool) string {
	if data < protocol.PoolMinDataBytes {
		role, total := "普通成员", protocol.PoolMinCapacityGiB
		if controller {
			role, total = "Master／etcd", protocol.PoolMinControlCapacityGiB
		}
		return fmt.Sprintf("成员 %s 作%s时总配额至少 %d GiB，B 数据卷至少 6 GiB", node, role, total)
	}
	gib := func(bytes int64) float64 { return float64(bytes) / float64(protocol.PoolGiB) }
	if a.UUID == b.UUID {
		need := gib(control) + gib(data) + gib(max(a.ReserveBytes, b.ReserveBytes)) + gib(max(a.ArchiveBytes, b.ArchiveBytes))
		return fmt.Sprintf("成员 %s 的 B 数据卷需要 %.2f GiB。A/B 同盘需可用 %.2f GiB（含预留），当前可用 %.2f GiB",
			node, gib(data), need, gib(min(a.AvailableBytes, b.AvailableBytes)))
	}
	return fmt.Sprintf("成员 %s 的 A 盘需可用 %.2f GiB，当前 %.2f GiB。B 盘需可用 %.2f GiB（含 %.2f GiB 数据卷及预留），当前 %.2f GiB",
		node, gib(control)+gib(a.ReserveBytes)+gib(a.ArchiveBytes), gib(a.AvailableBytes),
		gib(data)+gib(b.ReserveBytes)+gib(b.ArchiveBytes), gib(data), gib(b.AvailableBytes))
}

func Select(in protocol.PoolInput, probes map[string]protocol.PoolHostResult) ([]protocol.PoolNodePlan, error) {
	domains := make(map[string]bool)
	addresses := make(map[string]bool)
	out := make([]protocol.PoolNodePlan, 0, len(in.Nodes))
	for _, m := range in.Nodes {
		p, ok := probes[m.NodeID]
		if !ok {
			return nil, Fault("dependency_unavailable", "成员 "+m.NodeID+" 无法核对磁盘或端口")
		}
		if m.FailureDomain == "" {
			m.FailureDomain = p.HostID
		}
		if m.FailureDomain == "" || addresses[m.PrivateIP] {
			return nil, Fault("invalid_request", "故障域缺失或私网 IP 重复")
		}
		domains[m.FailureDomain], addresses[m.PrivateIP] = true, true
		if !p.PortsFree {
			return nil, Fault("conflict", "成员 "+m.NodeID+" 的数据或 S3 端口已占用")
		}
		out = append(out, protocol.PoolNodePlan{PoolMemberInput: m, Ports: Ports(in.PortOffset), ControlDisk: p.ControlDisk, DataDisk: p.DataDisk})
	}
	if len(domains) < 3 {
		return nil, Fault("invalid_request", "存储池至少需要三个故障域")
	}
	clear(domains)
	var controlShortfalls []string
	for i := range out {
		member := &out[i]
		p := probes[member.NodeID]
		capacity := member.CapacityGiB * protocol.PoolGiB
		data, control := Quotas(capacity, true)
		if len(domains) < 3 && !domains[member.FailureDomain] && p.ControlPortsFree && Enough(p.ControlDisk, p.DataDisk, control, data) {
			member.Roles = []string{"master", "etcd"}
			domains[member.FailureDomain] = true
		} else {
			if len(domains) < 3 && !domains[member.FailureDomain] && p.ControlPortsFree {
				controlShortfalls = append(controlShortfalls, CapacityMessage(member.NodeID, p.ControlDisk, p.DataDisk, control, data, true))
			}
			data, control = Quotas(capacity, false)
		}
		if !Enough(p.ControlDisk, p.DataDisk, control, data) {
			return nil, Fault("insufficient_storage", CapacityMessage(member.NodeID, p.ControlDisk, p.DataDisk, control, data, member.Control()))
		}
		member.DataBytes, member.ControlBytes = data, control
		member.Roles = append(member.Roles, "volume", "filer", "s3")
	}
	if len(domains) < 3 {
		controlDomains := map[string]bool{}
		for _, member := range out {
			if probes[member.NodeID].ControlPortsFree {
				controlDomains[member.FailureDomain] = true
			}
		}
		if len(controlDomains) < 3 {
			return nil, Fault("conflict", "三个独立故障域的控制端口不可用")
		}
		return nil, Fault("insufficient_storage", "可承担 Master 和 etcd 的独立故障域少于三个。"+strings.Join(controlShortfalls, "。"))
	}
	return out, nil
}

func ValidateCredentials(c protocol.PoolCredentials) error {
	if len(c.AccessKey) < 3 || len(c.AccessKey) > 128 || len(c.SecretKey) < 8 || len(c.SecretKey) > 128 {
		return Fault("invalid_request", "访问密钥须为 3–128 字节，秘密密钥须为 8–128 字节")
	}
	for _, ch := range c.AccessKey {
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return Fault("invalid_request", "访问密钥只支持字母、数字、下划线和连字符")
		}
	}
	return nil
}
