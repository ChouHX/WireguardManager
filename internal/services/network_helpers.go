package services

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ApplyRateLimit 在租户隧道接口上实施双向限速。
//
// 方向对应客户端的直观感受：
//   - downloadMbps 限制「服务端 → 设备」的流量，即客户端的下载速率，用 egress 整形（tbf）；
//   - uploadMbps 限制「设备 → 服务端」的流量，即客户端的上传速率，用 ingress 限速
//     （ingress qdisc + police，超速直接丢弃）。ingress 方向无法整形只能限速，
//     对「限速」这个语义来说足够。
//
// 取值为 0 表示该方向不限速；两者都为 0 时清除全部规则，恢复到不限速状态。
//
// qdisc 挂在 netdev 上，因此接口 down/up 都不会丢规则；但接口被销毁重建后
// netdev 是新对象，规则随之消失，需要由收敛流程重新下发（见 reconcile）。
func (s *InterfaceService) ApplyRateLimit(link string, downloadMbps, uploadMbps int) error {
	// 先清空既有规则，保证该操作幂等：重复设置不会叠加出多条 qdisc
	if err := s.clearRateLimit(link); err != nil {
		return err
	}

	if downloadMbps <= 0 && uploadMbps <= 0 {
		return nil
	}

	if downloadMbps > 0 {
		burst := rateLimitBurstBytes(downloadMbps)
		cmd := exec.Command("tc", "qdisc", "replace", "dev", link, "root", "tbf",
			"rate", fmt.Sprintf("%dmbit", downloadMbps),
			"burst", strconv.Itoa(burst),
			"latency", "400ms")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to set download rate limit (%d Mbps) on %s: %v, output: %s",
				downloadMbps, link, err, string(output))
		}
	}

	if uploadMbps > 0 {
		cmd := exec.Command("tc", "qdisc", "add", "dev", link, "handle", "ffff:", "ingress")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add ingress qdisc on %s: %v, output: %s", link, err, string(output))
		}

		cmd = exec.Command("tc", "filter", "add", "dev", link, "parent", "ffff:",
			"protocol", "all", "u32", "match", "u32", "0", "0",
			"police", "rate", fmt.Sprintf("%dmbit", uploadMbps),
			"burst", strconv.Itoa(rateLimitBurstBytes(uploadMbps)),
			"drop", "flowid", ":1")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to set upload rate limit (%d Mbps) on %s: %v, output: %s",
				uploadMbps, link, err, string(output))
		}
	}

	return nil
}

// clearRateLimit 清除接口上的限速规则，使 ApplyRateLimit 可以幂等重放。
//
// 刻意忽略失败：设备上的默认 root qdisc（noqueue/pfifo_fast）不允许删除，
// 「规则本来就不存在」是这里的常态而非异常。真正需要报错的情况会在随后的
// replace / add 中暴露出来。
func (s *InterfaceService) clearRateLimit(link string) error {
	for _, args := range [][]string{
		{"tc", "qdisc", "del", "dev", link, "root"},
		{"tc", "qdisc", "del", "dev", link, "ingress"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		_ = cmd.Run()
	}
	return nil
}

// rateLimitBurstBytes 计算 tbf/police 的突发额度（字节）。
//
// 突发额度决定了「瞬时允许多大的流量高于限定速率」，太小会让限速口吃、
// 吞吐远低于设定值。这里取约 25ms 的额度（速率 × 3200 字节），并以 MTU
// 与一个上限做兜底，覆盖 100000 Mbps 这类极端取值时不至于溢出。
func rateLimitBurstBytes(rateMbps int) int {
	const (
		minBurst = 16000             // 约 10 个 MTU，保证小速率下也有足够缓冲
		maxBurst = 128 * 1024 * 1024 // 防止极端速率下算出过大的值
	)
	burst := rateMbps * 3200
	if burst < minBurst {
		burst = minBurst
	}
	if burst > maxBurst {
		burst = maxBurst
	}
	return burst
}

// TunnelMTUOverhead 是 WireGuard 封装在隧道载荷之外额外占用、且必须由底层链路
// 承载的字节数：20 字节外层 IPv4 + 8 字节 UDP + 32 字节 WireGuard 头。
const TunnelMTUOverhead = 60

// DefaultTunnelMTU 是内核为新建 WireGuard 接口设定的默认 MTU。
// 它对应「底层链路 1500」这一前提。
const DefaultTunnelMTU = 1420

// OutInterfaceMTU 读取宿主出口接口的 MTU；读取失败返回 0。
//
// 直接读 sysfs 而非调用 ip：无需 fork，且在容器内同样可用。
func OutInterfaceMTU(name string) int {
	if name == "" {
		return 0
	}
	raw, err := os.ReadFile("/sys/class/net/" + name + "/mtu")
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return value
}

// CheckTunnelMTUFit 检查出口链路的 MTU 是否容得下隧道 MTU，并返回一条人类可读的
// 不匹配说明；匹配或无判定依据时返回空串。
//
// 存在的意义：隧道 MTU 不匹配是「能连上、却传不动数据」这类故障的常见成因，
// 但它完全静默——握手是小包，能通过；只有满长数据包会被悄悄丢弃。启动时主动
// 比对一次，把这条最难自己发现的故障提前摆到日志里。
func CheckTunnelMTUFit(outInterface string, tunnelMTU int) string {
	outMTU := OutInterfaceMTU(outInterface)
	if outMTU <= 0 {
		return ""
	}

	if tunnelMTU <= 0 {
		tunnelMTU = DefaultTunnelMTU
	}

	// 底层链路能承载的隧道载荷上限
	capacity := outMTU - TunnelMTUOverhead
	if capacity >= tunnelMTU {
		return ""
	}

	return fmt.Sprintf(
		"出口接口 %s 的 MTU 为 %d，最多只能承载 %d 字节的隧道载荷，小于隧道当前的 MTU %d。"+
			"此时握手与探测（小包）正常，但满长数据包会被底层丢弃，表现为「能连上却传不动数据」。"+
			"请把隧道 MTU 调整为 %d 或更小（管理界面「系统设置 → network.mtu」，或设 WM_NETWORK_MTU=%d）。",
		outInterface, outMTU, capacity, tunnelMTU, capacity, capacity)
}

// HostUDPPortInUse 判断宿主命名空间中指定 UDP 端口是否已被占用。
//
// 加密 socket 由内核建立在宿主命名空间，因此这是分配监听端口时必须避让的
// 唯一权威视图：端口一旦已被占用，接口 up 会因 EADDRINUSE 失败。
func HostUDPPortInUse(port int) bool {
	suffix := fmt.Sprintf(":%04X", port)

	for _, path := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		occupied := scanProcNetUDP(path, suffix)
		if occupied {
			return true
		}
	}
	return false
}

func scanProcNetUDP(path, portSuffix string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// 第二列形如 00000000:CB1F（地址:端口，均为十六进制）
		if strings.HasSuffix(strings.ToUpper(fields[1]), portSuffix) {
			return true
		}
	}
	return false
}

func isDefaultRoute(cidr string) bool { return cidr == "0.0.0.0/0" || cidr == "::/0" }
