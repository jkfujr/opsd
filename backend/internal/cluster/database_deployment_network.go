package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) previewDeploymentNetwork(ctx context.Context, targets []databaseDeploymentTarget) ([]DatabaseDeploymentNetwork, error) {
	out := []DatabaseDeploymentNetwork{}
	for _, target := range targets {
		if target.Deployment.Kind != "galera" {
			continue
		}
		var state protocol.ManagedFirewallState
		if err := n.Call(ctx, target.Ref.NodeID, "firewall.config.read", nil, &state); err != nil {
			return out, err
		}
		if state.Backend == "none" || state.ServiceActive != nil && !*state.ServiceActive {
			continue
		}
		if state.State != "in_sync" || state.Revision == 0 {
			return out, fault("conflict", target.Ref.Name+"：请先处理防火墙未完成操作")
		}
		var trust TrustRules
		_ = n.Call(ctx, target.Ref.NodeID, "peer_trust.rules", nil, &trust)
		definition := state.FirewallDefinition
		changes := []protocol.FirewallChange{}
		start := len(out)
		for _, source := range targets {
			if source.Ref.NodeID == target.Ref.NodeID {
				continue
			}
			ip, err := netip.ParseAddr(source.Deployment.Address)
			if err != nil {
				return out, fault("invalid_request", "数据库成员地址无效")
			}
			destination, err := netip.ParseAddr(target.Deployment.Address)
			if err != nil || ip.Is6() != destination.Is6() {
				return out, fault("invalid_request", "请选择相同地址族的数据库成员地址")
			}
			trusted := false
			if trust.Enabled && trust.State == "in_sync" {
				for _, rule := range trust.Rules {
					address := strings.Split(rule.Address, "/")[0]
					if address == ip.String() && rule.State == "in_sync" {
						trusted = true
						break
					}
				}
			}
			if trusted {
				continue
			}
			ports := []int{target.Deployment.SQLPort}
			if source.Deployment.Kind == "galera" {
				ports = append(ports, target.Deployment.ReplicationPort, target.Deployment.ISTPort, target.Deployment.SSTPort)
			}
			slices.Sort(ports)
			ports = slices.Compact(ports)
			texts := []string{}
			for _, port := range ports {
				texts = append(texts, strconv.Itoa(port))
			}
			protocols := []string{"tcp"}
			if source.Deployment.Kind == "galera" {
				protocols = append(protocols, "udp")
			}
			for _, transport := range protocols {
				portList := strings.Join(texts, ",")
				if transport == "udp" {
					portList = strconv.Itoa(target.Deployment.ReplicationPort)
				}
				family := protocol.AddressFamily("ipv4")
				if ip.Is6() {
					family = "ipv6"
				}
				spec := protocol.RuleSpec{Family: family, Source: ip.String(), Destination: destination.String(), Protocol: transport, Port: portList, Action: "allow", Comment: "OPSD 数据库成员"}
				if slices.ContainsFunc(definition.Rules, func(r protocol.ManagedRule) bool {
					return r.Spec.Source == spec.Source && r.Spec.Destination == spec.Destination && r.Spec.Protocol == spec.Protocol && r.Spec.Port == spec.Port && r.Spec.Action == "allow"
				}) {
					continue
				}
				change := protocol.FirewallChange{Kind: "add_rule", Scope: protocol.Both, Rule: &spec}
				if definition.Backend == "ufw" {
					for _, rule := range definition.Rules {
						if rule.Spec.Family == family && strings.HasSuffix(rule.Chain, "user-input") {
							change.BeforeRuleID = rule.ID
							break
						}
					}
				}
				if definition.Backend == "firewalld" {
					priority := -100
					change.Priority = &priority
					for _, policy := range definition.DefaultPolicies {
						if policy.Zone != "" {
							change.Zone = policy.Zone
							break
						}
					}
				}
				definition, err = firewall.ChangeConfiguration(definition, change)
				if err != nil {
					return out, fmt.Errorf("%s：%w", target.Ref.Name, err)
				}
				changes = append(changes, change)
				out = append(out, DatabaseDeploymentNetwork{NodeID: target.Ref.NodeID, Source: spec.Source, Protocol: transport, Ports: portList})
			}
		}
		if len(changes) > 0 {
			out[start].Write = FirewallWrite{ExpectedRevision: state.Revision, Changes: changes}
		}
	}
	return out, nil
}
