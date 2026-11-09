package firewall

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

func nftTrustFile(text, chain string, target protocol.PeerTrustTarget) (string, error) {
	lines := strings.Split(text, "\n")
	start, end, e := nftChainRegion(lines, chain)
	if e != nil {
		return "", e
	}
	family := strings.Split(chain, "/")[0]
	inside := []string{}
	insert := 0
	for _, line := range lines[start+1 : end] {
		if strings.Contains(line, `comment "`+trustMarker+`"`) {
			if _, e := verifyNFTTrust(parseNFTText(strings.TrimSpace(line), family)); e != nil {
				return "", e
			}
			continue
		}
		inside = append(inside, line)
		if nftPolicyMetadata.MatchString(strings.TrimSpace(line)) || nftInputMetadata.MatchString(strings.TrimSpace(line)) {
			insert = len(inside)
		}
	}
	rules := []string{}
	for _, f := range nftFamilies(family) {
		for _, ip := range trustList(target, f) {
			rules = append(rules, "\t\t"+nftExpression(trustNFTSpec(ip, f)))
		}
	}
	out := append([]string(nil), lines[:start+1]...)
	out = append(out, inside[:insert]...)
	out = append(out, rules...)
	out = append(out, inside[insert:]...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), nil
}

func syncNFTTrust(ctx context.Context, h Host, target protocol.PeerTrustTarget) []protocol.ScopeResult {
	n := &NFT{Host: h}
	sources, e := n.sources(ctx)
	if e != nil {
		return failedTrust(e)
	}
	chains := []string{}
	for chain, source := range sources.Chains {
		if source.Policy != "" && source.Input {
			chains = append(chains, chain)
		}
	}
	sort.Strings(chains)
	if len(chains) == 0 {
		return failedTrust(Unsupported("没有可维护的持久入站基础链"))
	}
	files := map[string]string{}
	original := map[string][]byte{}
	for _, chain := range chains {
		path := sources.Chains[chain].Path
		text, ok := files[path]
		if !ok {
			b, e := h.Read(path)
			if e != nil {
				return failedTrust(e)
			}
			original[path] = b
			text = string(b)
		}
		next, e := nftTrustFile(text, chain, target)
		if e != nil {
			return failedTrust(e)
		}
		files[path] = next
	}
	for path, text := range files {
		if text != string(original[path]) {
			if e = h.Replace(path, original[path], []byte(text)); e != nil {
				return failedTrust(e)
			}
		}
		b, e := h.Read(path)
		if e != nil {
			return failedTrust(e)
		}
		if string(b) != text {
			return failedTrust(Conflict("互信持久配置读回不一致"))
		}
	}
	out := []protocol.ScopeResult{}
	supported := map[protocol.AddressFamily]bool{}
	for _, chain := range chains {
		for _, f := range nftFamilies(strings.Split(chain, "/")[0]) {
			supported[f] = true
		}
	}
	for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		r := trustResult(f, protocol.Permanent, trustList(target, f), nil)
		if !supported[f] && len(trustList(target, f)) > 0 {
			r = trustResult(f, protocol.Permanent, nil, Unsupported("没有对应地址族的入站链"))
			r.State = "unsupported"
		}
		out = append(out, r)
	}
	active, err := h.Run(ctx, "systemctl", "is-active", "nftables.service")
	if err != nil || strings.TrimSpace(string(active)) != "active" {
		for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
			r := trustResult(f, protocol.Runtime, nil, nil)
			r.State = "inactive"
			out = append(out, r)
		}
		return out
	}
	doc, e := readTrustNFT(ctx, h)
	if e != nil {
		return appendTrustErrors(out, e)
	}
	commands := []string{}
	refs := map[string]bool{}
	for _, entry := range doc.NFTables {
		if c := entry.Chain; c != nil && c.Hook == "input" && c.Type == "filter" && len(nftFamilies(c.Family)) > 0 {
			ref := c.Family + "/" + c.Table + "/" + c.Name
			refs[ref] = true
			if _, ok := sources.Chains[ref]; !ok {
				return appendTrustErrors(out, Conflict("存在持久来源未知的入站链"))
			}
		}
	}
	for _, chain := range chains {
		if !refs[chain] {
			return appendTrustErrors(out, Conflict("运行入站链缺失"))
		}
		parts := strings.Split(chain, "/")
		ref := strings.Join(parts, " ")
		wanted := []string{}
		for _, family := range nftFamilies(parts[0]) {
			wanted = append(wanted, trustList(target, family)...)
		}
		actual := []string{}
		handles := []uint64{}
		first := true
		prefix := true
		for _, entry := range doc.NFTables {
			r := entry.Rule
			if r == nil || r.Family+"/"+r.Table+"/"+r.Chain != chain {
				continue
			}
			if r.Comment != trustMarker {
				first = false
				continue
			}
			if !first {
				prefix = false
			}
			ip, e := verifyNFTTrust(parseNFTExpressions(r.Expr, r.Family, r.Comment))
			if e != nil {
				return appendTrustErrors(out, e)
			}
			actual = append(actual, ip)
			handles = append(handles, r.Handle)
		}
		if slices.Equal(actual, wanted) && prefix {
			continue
		}
		for _, handle := range handles {
			commands = append(commands, "delete rule "+ref+" handle "+strconv.FormatUint(handle, 10))
		}
		for i := len(wanted) - 1; i >= 0; i-- {
			family := protocol.IPv4
			if strings.Contains(wanted[i], ":") {
				family = protocol.IPv6
			}
			commands = append(commands, "insert rule "+ref+" "+nftExpression(trustNFTSpec(wanted[i], family)))
		}
	}
	if len(commands) > 0 {
		if e = inputRules(h, ctx, strings.Join(commands, "\n")+"\n", "nft", "-f", "-"); e != nil {
			return appendTrustErrors(out, e)
		}
		doc, e = readTrustNFT(ctx, h)
		if e != nil {
			return appendTrustErrors(out, e)
		}
	}
	for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		actual := []string{}
		var checkErr error
		for _, chain := range chains {
			if !slices.Contains(nftFamilies(strings.Split(chain, "/")[0]), f) {
				continue
			}
			group := []string{}
			for _, entry := range doc.NFTables {
				r := entry.Rule
				if r == nil || r.Family+"/"+r.Table+"/"+r.Chain != chain || r.Comment != trustMarker {
					continue
				}
				spec := parseNFTExpressions(r.Expr, r.Family, r.Comment)
				if spec != nil && spec.Family == f {
					ip, e := verifyNFTTrust(spec)
					if e != nil {
						checkErr = e
					} else {
						group = append(group, ip)
					}
				}
			}
			if !slices.Equal(group, trustList(target, f)) {
				checkErr = Conflict("互信运行规则读回不一致")
			}
			for _, ip := range group {
				if !slices.Contains(actual, ip) {
					actual = append(actual, ip)
				}
			}
		}
		r := trustResult(f, protocol.Runtime, actual, checkErr)
		if !supported[f] && len(trustList(target, f)) > 0 {
			r = trustResult(f, protocol.Runtime, nil, Unsupported("没有对应地址族的入站链"))
			r.State = "unsupported"
		}
		out = append(out, r)
	}
	return out
}
func readTrustNFT(ctx context.Context, h Host) (nftDocument, error) {
	var doc nftDocument
	b, e := h.Run(ctx, "nft", "-j", "-a", "-s", "list", "ruleset")
	if e == nil {
		e = json.Unmarshal(b, &doc)
	}
	return doc, e
}
func appendTrustErrors(out []protocol.ScopeResult, e error) []protocol.ScopeResult {
	for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		out = append(out, trustResult(f, protocol.Runtime, nil, e))
	}
	return out
}
