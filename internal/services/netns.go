package services

import (
	"fmt"
	"os/exec"
	"strings"
)

// NetnsService 网络命名空间服务
type NetnsService struct{}

// NewNetnsService 创建网络命名空间服务实例
func NewNetnsService() *NetnsService {
	return &NetnsService{}
}

// CreateNamespace 创建网络命名空间
func (s *NetnsService) CreateNamespace(name string) error {
	// 创建命名空间
	cmd := exec.Command("ip", "netns", "add", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create namespace %s: %v, output: %s", name, err, string(output))
	}
	return nil
}

// DeleteNamespace 删除网络命名空间
func (s *NetnsService) DeleteNamespace(name string) error {
	cmd := exec.Command("ip", "netns", "delete", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete namespace %s: %v, output: %s", name, err, string(output))
	}
	return nil
}

// NamespaceExists 检查命名空间是否存在
func (s *NetnsService) NamespaceExists(name string) (bool, error) {
	cmd := exec.Command("ip", "netns", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to list namespaces: %v", err)
	}
	
	namespaces := strings.Split(string(output), "\n")
	for _, ns := range namespaces {
		// 命名空间列表格式为 "name (id: X)" 或 "name"
		nsName := strings.TrimSpace(strings.Split(ns, " ")[0])
		if nsName == name {
			return true, nil
		}
	}
	return false, nil
}

// CreateVethPair 创建veth对并配置网络连接
// vethHost: 主机端接口名
// vethNs: 命名空间端接口名
// nsName: 命名空间名称
// nsIP: 命名空间内IP地址 (CIDR格式, 如 "10.200.1.2/24")
// hostIP: 主机端IP地址 (CIDR格式, 如 "10.200.1.1/24")
func (s *NetnsService) CreateVethPair(vethHost, vethNs, nsName, nsIP, hostIP string) error {
	// 1. 创建veth对
	cmd := exec.Command("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethNs)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create veth pair: %v, output: %s", err, string(output))
	}

	// 2. 将vethNs移动到命名空间
	cmd = exec.Command("ip", "link", "set", vethNs, "netns", nsName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to move veth to namespace: %v, output: %s", err, string(output))
	}

	// 3. 启动主机端接口
	cmd = exec.Command("ip", "link", "set", vethHost, "up")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to bring up host veth: %v, output: %s", err, string(output))
	}

	// 4. 配置主机端IP
	cmd = exec.Command("ip", "addr", "add", hostIP, "dev", vethHost)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IP to host veth: %v, output: %s", err, string(output))
	}

	// 5. 在命名空间内配置网络
	// 5.1 启动lo接口
	cmd = exec.Command("ip", "netns", "exec", nsName, "ip", "link", "set", "lo", "up")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to bring up lo in namespace: %v, output: %s", err, string(output))
	}

	// 5.2 启动veth接口
	cmd = exec.Command("ip", "netns", "exec", nsName, "ip", "link", "set", vethNs, "up")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to bring up veth in namespace: %v, output: %s", err, string(output))
	}

	// 5.3 配置IP地址
	cmd = exec.Command("ip", "netns", "exec", nsName, "ip", "addr", "add", nsIP, "dev", vethNs)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IP to namespace veth: %v, output: %s", err, string(output))
	}

	// 5.4 设置默认路由 (使用主机端IP作为网关)
	gateway := strings.Split(hostIP, "/")[0] // 提取IP地址部分
	cmd = exec.Command("ip", "netns", "exec", nsName, "ip", "route", "add", "default", "via", gateway)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add default route in namespace: %v, output: %s", err, string(output))
	}

	return nil
}

// EnableNAT 为命名空间启用NAT，使其能访问外网
// nsSubnet: 命名空间子网 (CIDR格式, 如 "10.200.1.0/24")
// outInterface: 出口网络接口 (如 "eth0")
func (s *NetnsService) EnableNAT(nsSubnet, outInterface string) error {
	// 启用IP转发
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %v, output: %s", err, string(output))
	}

	// 添加iptables NAT规则
	cmd = exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", nsSubnet, "-o", outInterface, "-j", "MASQUERADE")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add NAT rule: %v, output: %s", err, string(output))
	}

	// 允许转发
	cmd = exec.Command("iptables", "-A", "FORWARD", "-i", outInterface, "-o", "veth+", "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add forward rule: %v, output: %s", err, string(output))
	}

	cmd = exec.Command("iptables", "-A", "FORWARD", "-o", outInterface, "-i", "veth+", "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add forward rule: %v, output: %s", err, string(output))
	}

	return nil
}

// SetupPortForwarding 设置端口转发，将主机端口转发到命名空间内的服务
// outInterface: 外部网络接口 (如 "eth0")
// externalPort: 外部端口
// nsIP: 命名空间内的IP地址 (不带CIDR)
// internalPort: 命名空间内的端口
// protocol: 协议 ("tcp" 或 "udp")
func (s *NetnsService) SetupPortForwarding(outInterface string, externalPort int, nsIP string, internalPort int, protocol string) error {
	// 1. 允许外部访问该端口
	cmd := exec.Command("iptables", "-A", "INPUT", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", externalPort), "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add INPUT rule: %v, output: %s", err, string(output))
	}

	// 2. 设置DNAT规则，将外部端口流量转发到命名空间内部IP和端口
	cmd = exec.Command("iptables", "-t", "nat", "-A", "PREROUTING", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", externalPort), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", nsIP, internalPort))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add DNAT rule: %v, output: %s", err, string(output))
	}

	// 3. 允许转发到命名空间
	cmd = exec.Command("iptables", "-A", "FORWARD", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", internalPort), "-d", nsIP, "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add FORWARD rule: %v, output: %s", err, string(output))
	}

	// 4. 允许从命名空间返回的流量
	cmd = exec.Command("iptables", "-A", "FORWARD", "-o", outInterface, "-p", protocol, "--sport", fmt.Sprintf("%d", internalPort), "-s", nsIP, "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add FORWARD return rule: %v, output: %s", err, string(output))
	}

	return nil
}

// RemovePortForwarding 删除端口转发规则
func (s *NetnsService) RemovePortForwarding(outInterface string, externalPort int, nsIP string, internalPort int, protocol string) error {
	// 删除规则（忽略错误）
	exec.Command("iptables", "-D", "INPUT", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", externalPort), "-j", "ACCEPT").CombinedOutput()
	exec.Command("iptables", "-t", "nat", "-D", "PREROUTING", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", externalPort), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", nsIP, internalPort)).CombinedOutput()
	exec.Command("iptables", "-D", "FORWARD", "-i", outInterface, "-p", protocol, "--dport", fmt.Sprintf("%d", internalPort), "-d", nsIP, "-j", "ACCEPT").CombinedOutput()
	exec.Command("iptables", "-D", "FORWARD", "-o", outInterface, "-p", protocol, "--sport", fmt.Sprintf("%d", internalPort), "-s", nsIP, "-j", "ACCEPT").CombinedOutput()
	return nil
}

// ExecInNamespace 在指定命名空间中执行命令
func (s *NetnsService) ExecInNamespace(nsName string, command []string) (string, error) {
	args := append([]string{"netns", "exec", nsName}, command...)
	cmd := exec.Command("ip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to exec command in namespace %s: %v, output: %s", nsName, err, string(output))
	}
	return string(output), nil
}

// AddRouteForPeer 为peer的allowedIPs添加路由规则
// 确保命名空间内访问这些IP时通过WireGuard接口
func (s *NetnsService) AddRouteForPeer(nsName, wgInterface, allowedIPs string) error {
	// allowedIPs 可能包含多个网段，用逗号分隔
	ipRanges := strings.Split(allowedIPs, ",")
	
	for _, ipRange := range ipRanges {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" {
			continue
		}
		
		// 添加路由：目标网段通过WireGuard接口
		cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "route", "add", ipRange, "dev", wgInterface)
		if output, err := cmd.CombinedOutput(); err != nil {
			// 如果路由已存在，忽略错误
			if !strings.Contains(string(output), "File exists") {
				return fmt.Errorf("failed to add route for %s: %v, output: %s", ipRange, err, string(output))
			}
		}
	}
	
	return nil
}

// DeleteRouteForPeer 删除peer的allowedIPs路由规则
func (s *NetnsService) DeleteRouteForPeer(nsName, wgInterface, allowedIPs string) error {
	ipRanges := strings.Split(allowedIPs, ",")
	
	for _, ipRange := range ipRanges {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" {
			continue
		}
		
		// 删除路由
		cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "route", "del", ipRange, "dev", wgInterface)
		if output, err := cmd.CombinedOutput(); err != nil {
			// 如果路由不存在，忽略错误
			if !strings.Contains(string(output), "No such process") && !strings.Contains(string(output), "not found") {
				return fmt.Errorf("failed to delete route for %s: %v, output: %s", ipRange, err, string(output))
			}
		}
	}
	
	return nil
}

// AddIptablesRuleForPeer 为peer添加iptables规则（如果需要NAT或转发控制）
func (s *NetnsService) AddIptablesRuleForPeer(nsName, allowedIPs string) error {
	ipRanges := strings.Split(allowedIPs, ",")
	
	for _, ipRange := range ipRanges {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" {
			continue
		}
		
		// 允许转发到该网段
		cmd := exec.Command("ip", "netns", "exec", nsName, "iptables", "-A", "FORWARD", "-d", ipRange, "-j", "ACCEPT")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add iptables forward rule for %s: %v, output: %s", ipRange, err, string(output))
		}
		
		// 允许从该网段转发回来
		cmd = exec.Command("ip", "netns", "exec", nsName, "iptables", "-A", "FORWARD", "-s", ipRange, "-j", "ACCEPT")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add iptables forward rule for %s: %v, output: %s", ipRange, err, string(output))
		}
	}
	
	return nil
}

// DeleteIptablesRuleForPeer 删除peer的iptables规则
func (s *NetnsService) DeleteIptablesRuleForPeer(nsName, allowedIPs string) error {
	ipRanges := strings.Split(allowedIPs, ",")
	
	for _, ipRange := range ipRanges {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" {
			continue
		}
		
		// 删除转发规则（忽略错误）
		cmd := exec.Command("ip", "netns", "exec", nsName, "iptables", "-D", "FORWARD", "-d", ipRange, "-j", "ACCEPT")
		cmd.CombinedOutput()
		
		cmd = exec.Command("ip", "netns", "exec", nsName, "iptables", "-D", "FORWARD", "-s", ipRange, "-j", "ACCEPT")
		cmd.CombinedOutput()
	}
	
	return nil
}
