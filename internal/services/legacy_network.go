package services

import (
	"errors"
	"fmt"
	"os/exec"
)

// These rules are removed only when upgrading a legacy namespace deployment.
// maxRuleRemovals 单条规则的最大删除次数。
// 旧版本为宿主机上的每个账号都追加过一份相同的规则，因此清理时必须循环，直到该规则不再存在。
const maxRuleRemovals = 256

// removeIptablesRule 反复删除同一条规则，直到它不再存在。
func removeIptablesRule(args ...string) {
	for i := 0; i < maxRuleRemovals; i++ {
		if exec.Command("iptables", args...).Run() != nil {
			return
		}
	}
}

// hostNATRules 旧版方案在宿主机上写下的出口 NAT 规则。
// 当前架构不再使用它们，仅用于回收历史部署的残留。
func hostNATRules(subnet, outInterface string) []struct {
	table string
	chain string
	rule  []string
} {
	return []struct {
		table string
		chain string
		rule  []string
	}{
		{"filter", "FORWARD", []string{"-i", "veth+", "-o", outInterface, "-j", "ACCEPT"}},
		{"filter", "FORWARD", []string{"-i", outInterface, "-o", "veth+", "-j", "ACCEPT"}},
		{"nat", "POSTROUTING", []string{"-s", subnet, "-o", outInterface, "-j", "MASQUERADE"}},
	}
}

// RemoveHostNAT 移除旧版方案留在宿主机上的出口 NAT 规则（尽力而为）。
func (s *InterfaceService) RemoveHostNAT(subnet, outInterface string) {
	for _, r := range hostNATRules(subnet, outInterface) {
		args := append(tableArgs(r.table), "-D", r.chain)
		args = append(args, r.rule...)
		removeIptablesRule(args...)
	}
}

// RemoveLegacyPortForwarding 清理旧版（veth + DNAT）方案在宿主机上留下的端口映射规则。
//
// 旧方案把命名空间内的 wg 端口经 DNAT 暴露到物理网卡，回程依赖 conntrack 反向
// 改写，是端口漂移与非对称丢包的根源。这些规则在升级后已无用处，必须回收，
// 否则会继续劫持新方案中本应由内核直接投递给加密 socket 的报文。
func (s *InterfaceService) RemoveLegacyPortForwarding(outInterface string, port int, nsIP, protocol string) {
	portStr := fmt.Sprintf("%d", port)

	input := []string{"-i", outInterface, "-p", protocol, "--dport", portStr, "-j", "ACCEPT"}
	dnat := []string{"-i", outInterface, "-p", protocol, "--dport", portStr, "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", nsIP, port)}
	forwardIn := []string{"-i", outInterface, "-p", protocol, "--dport", portStr, "-d", nsIP, "-j", "ACCEPT"}
	forwardOut := []string{"-o", outInterface, "-p", protocol, "--sport", portStr, "-s", nsIP, "-j", "ACCEPT"}

	removeIptablesRule(append([]string{"-D", "INPUT"}, input...)...)
	removeIptablesRule(append([]string{"-t", "nat", "-D", "PREROUTING"}, dnat...)...)
	removeIptablesRule(append([]string{"-D", "FORWARD"}, forwardIn...)...)
	removeIptablesRule(append([]string{"-D", "FORWARD"}, forwardOut...)...)
}

// tableArgs 返回表参数；filter 表可省略。
func tableArgs(table string) []string {
	if table == "" || table == "filter" {
		return nil
	}
	return []string{"-t", table}
}

// ErrNoAvailablePort 表示宿主机上已无可用监听端口。
var ErrNoAvailablePort = errors.New("no available WireGuard listen port")
