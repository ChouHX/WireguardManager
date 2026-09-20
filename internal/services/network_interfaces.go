package services

import (
	"bufio"
	"net"
	"os"
	"sort"
	"strings"
)

// NetworkInterfaceInfo 描述一个可供选择的网络接口。
type NetworkInterfaceInfo struct {
	Name       string   `json:"name"`
	Addresses  []string `json:"addresses"`
	IsUp       bool     `json:"is_up"`
	IsLoopback bool     `json:"is_loopback"`
	// IsDefault 表示它是系统默认路由的出口接口
	IsDefault bool `json:"is_default"`
	// IsVirtual 表示这是隧道/容器/网桥一类的虚拟接口，通常不该作为转发出口
	IsVirtual bool `json:"is_virtual"`
}

// virtualPrefixes 常见虚拟接口前缀，这些接口默认不推荐作为转发出口。
var virtualPrefixes = []string{
	"lo", "wg", "veth", "docker", "br-", "virbr", "tun", "tap",
	"kube", "cni", "flannel", "dummy", "vnet", "ip6tnl", "sit",
	// 第三方组网/隧道
	"tailscale", "zt", "nebula", "ipsec", "gre", "ppp", "utun", "awdl",
}

func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// defaultRouteInterface 从 /proc/net/route 解析默认路由的出口接口。
// 读取失败（非 Linux 或权限不足）时返回空字符串，由调用方回退到配置值。
func defaultRouteInterface() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		if first { // 跳过表头
			first = false
			continue
		}

		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}

		// 目标地址为全 0 即默认路由；Flag 的 0x2 位表示该路由有网关
		if fields[1] != "00000000" {
			continue
		}
		flags := fields[3]
		if len(flags) < 4 || !strings.HasSuffix(flags, "3") && !strings.HasSuffix(flags, "1") {
			continue
		}
		return fields[0]
	}

	return ""
}

// DetectNetworkInterfaces 列出主机上可用的网络接口，并返回探测到的默认出口接口名。
// 列表按"物理接口优先、默认出口最前"排序，便于前端直接作为下拉选项。
func DetectNetworkInterfaces() ([]NetworkInterfaceInfo, string) {
	defaultIface := defaultRouteInterface()

	result := make([]NetworkInterfaceInfo, 0, 8)

	ifaces, err := net.Interfaces()
	if err != nil {
		return result, defaultIface
	}

	for _, iface := range ifaces {
		info := NetworkInterfaceInfo{
			Name:       iface.Name,
			Addresses:  []string{},
			IsUp:       iface.Flags&net.FlagUp != 0,
			IsLoopback: iface.Flags&net.FlagLoopback != 0,
			IsDefault:  iface.Name == defaultIface && defaultIface != "",
			IsVirtual:  isVirtualInterface(iface.Name),
		}

		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					info.Addresses = append(info.Addresses, ipNet.String())
				}
			}
		}

		result = append(result, info)
	}

	// 排序：默认出口 → 其它物理接口 → 虚拟接口；同类按名称
	sort.Slice(result, func(i, j int) bool { return interfaceLess(result[i], result[j]) })

	return result, defaultIface
}

func interfaceLess(a, b NetworkInterfaceInfo) bool {
	rank := func(item NetworkInterfaceInfo) int {
		switch {
		case item.IsDefault:
			return 0
		case item.IsLoopback:
			return 2
		case item.IsVirtual:
			return 3
		default:
			return 1
		}
	}

	ra, rb := rank(a), rank(b)
	if ra != rb {
		return ra < rb
	}
	return a.Name < b.Name
}
