package service

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// DetectLANAdapters follows longest connected prefix, then interface metric.
// It supports a downstream /32 as well as a subnet and never selects a WAN default route.
func DetectLANAdapters(lans []netip.Prefix, adapters []AdapterInfo) ([]AdapterInfo, error) {
	selected := map[string]AdapterInfo{}
	for _, lan := range lans {
		type candidate struct {
			adapter AdapterInfo
			bits    int
		}
		candidates := []candidate{}
		for _, adapter := range adapters {
			if !adapter.AutoEligible {
				continue
			}
			for _, raw := range adapter.Addresses {
				local, err := netip.ParsePrefix(raw)
				if err != nil || !local.Addr().Is4() {
					continue
				}
				if local.Masked().Contains(lan.Addr()) && local.Bits() <= lan.Bits() {
					candidates = append(candidates, candidate{adapter, local.Bits()})
				}
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("未找到能直达 %s 的局域网网卡，请连接现场网络并检查下挂地址", lan)
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].bits != candidates[j].bits {
				return candidates[i].bits > candidates[j].bits
			}
			if candidates[i].adapter.Metric != candidates[j].adapter.Metric {
				return candidates[i].adapter.Metric < candidates[j].adapter.Metric
			}
			return candidates[i].adapter.ID < candidates[j].adapter.ID
		})
		best := candidates[0]
		for _, other := range candidates[1:] {
			if other.adapter.ID != best.adapter.ID && other.bits == best.bits && other.adapter.Metric == best.adapter.Metric {
				return nil, fmt.Errorf("多张网卡都能到达 %s 且优先级相同，请断开不使用的网络后重试", lan)
			}
		}
		selected[best.adapter.ID] = best.adapter
	}
	result := make([]AdapterInfo, 0, len(selected))
	for _, a := range selected {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func SuggestedLANs(adapters []AdapterInfo, tunnel []netip.Prefix) string {
	values := []netip.Prefix{}
	seen := map[netip.Prefix]bool{}
	for _, a := range adapters {
		if !a.AutoEligible {
			continue
		}
		for _, raw := range a.Addresses {
			p, err := netip.ParsePrefix(raw)
			if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() {
				continue
			}
			p = p.Masked()
			blocked := p.Overlaps(netip.MustParsePrefix("10.100.0.0/16"))
			for _, vpn := range tunnel {
				blocked = blocked || p.Overlaps(vpn)
			}
			if !blocked && !seen[p] {
				values = append(values, p)
				seen[p] = true
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return strings.Compare(values[i].String(), values[j].String()) < 0 })
	return JoinPrefixes(values)
}

func ValidateLocalRouteTargets(p Profile, adapters []AdapterInfo) error {
	routes, err := p.Routes()
	if err != nil {
		return err
	}
	for _, a := range adapters {
		for _, raw := range a.Addresses {
			local, err := netip.ParsePrefix(raw)
			if err != nil {
				continue
			}
			for _, route := range routes {
				if route.Contains(local.Addr()) {
					return fmt.Errorf("访问网段 %s 覆盖本机地址 %s，请在访问目标中填写具体远端 IP（/32）", route, local.Addr())
				}
			}
		}
	}
	return nil
}
