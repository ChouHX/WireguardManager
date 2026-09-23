package services

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// NetnsService 网络命名空间编排。
//
// 加密信道采用 WireGuard 的「跨命名空间原生」设计：网卡在【宿主命名空间】
// 创建，内核把创建时的命名空间记进 struct wg_device.creating_net；此后每次
// 接口 up 都在该命名空间重建 UDP socket，收发加密报文时的路由查找
// （sock_net(sock)）与源地址选择也都在该命名空间完成。网卡实体本身位于
// 账号独占的命名空间，明文流量因此被封闭其中。
//
// 由此彻底不需要 DNAT、端口映射与 conntrack 回转：宿主机上的 <listen-port>
// 就是客户端要连的端口，回程源端口恒等于它，不存在端口漂移。
//
// 两条由内核语义决定的操作约束（已在本机实测确认）：
//   - 移入命名空间会让接口强制 down，wg_stop 随即销毁 socket；必须重新 up，
//     socket 才会在宿主命名空间重建。
//   - socket 归属只取决于「创建接口时所在命名空间」，与在哪里执行 up 无关。
type NetnsService struct{}

// NewNetnsService 创建网络命名空间服务实例
func NewNetnsService() *NetnsService {
	return &NetnsService{}
}

// CreateNamespace 创建网络命名空间
func (s *NetnsService) CreateNamespace(name string) error {
	cmd := exec.Command("ip", "netns", "add", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "File exists") {
			return nil
		}
		return fmt.Errorf("failed to create namespace %s: %v, output: %s", name, err, string(output))
	}
	return nil
}

// DeleteNamespace 删除网络命名空间
func (s *NetnsService) DeleteNamespace(name string) error {
	cmd := exec.Command("ip", "netns", "delete", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		// 命名空间不存在时视为已达成目标
		if strings.Contains(string(output), "No such file or directory") ||
			strings.Contains(string(output), "Cannot remove namespace file") {
			return nil
		}
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

	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(strings.Split(line, " ")[0]) == name {
			return true, nil
		}
	}
	return false, nil
}

// CreateWireguardDevice 在【宿主命名空间】创建 WireGuard 网卡。
//
// 这是整套方案的关键一步：接口诞生于宿主命名空间，内核据此把
// creating_net 固定为宿主网络栈，加密报文的收发路径就此与账号命名空间解耦。
// 调用方应传入唯一名称（避免与宿主机上其它接口或并发编排撞名），
// 移入目标命名空间后再改回 wg0。
func (s *NetnsService) CreateWireguardDevice(name string) error {
	cmd := exec.Command("ip", "link", "add", "dev", name, "type", "wireguard")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create wireguard device %s in host namespace: %v, output: %s", name, err, string(output))
	}
	return nil
}

// MoveLinkToNamespace 把网卡移入目标命名空间。
//
// 内核会先让接口 down（若原本是 up 状态，wg_stop 会销毁已有 socket），
// 调用方必须在移入后重新 up，socket 才会在宿主命名空间重建。
func (s *NetnsService) MoveLinkToNamespace(link, nsName string) error {
	cmd := exec.Command("ip", "link", "set", "dev", link, "netns", nsName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to move %s into namespace %s: %v, output: %s", link, nsName, err, string(output))
	}
	return nil
}

// RenameLinkInNamespace 在命名空间内重命名网卡。
// 改名只触发 NETDEV_CHANGENAME，不影响 creating_net，加密 socket 归属不变。
func (s *NetnsService) RenameLinkInNamespace(nsName, oldName, newName string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "link", "set", "dev", oldName, "name", newName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to rename %s to %s in namespace %s: %v, output: %s", oldName, newName, nsName, err, string(output))
	}
	return nil
}

// DeleteLinkInNamespace 删除命名空间内的网卡（接口不存在时视为成功）。
func (s *NetnsService) DeleteLinkInNamespace(nsName, link string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "link", "del", link)
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "Cannot find device") {
			return nil
		}
		return fmt.Errorf("failed to delete %s in namespace %s: %v, output: %s", link, nsName, err, string(output))
	}
	return nil
}

// LinkExistsInNamespace 判断命名空间内是否存在指定网卡。
func (s *NetnsService) LinkExistsInNamespace(nsName, link string) bool {
	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "link", "show", "dev", link)
	return cmd.Run() == nil
}

// DeleteLinkInHost 删除宿主命名空间中的网卡（接口不存在时视为成功）。
// 用于回收尚未成功移入账号命名空间的临时接口。
func (s *NetnsService) DeleteLinkInHost(link string) error {
	cmd := exec.Command("ip", "link", "del", link)
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "Cannot find device") {
			return nil
		}
		return fmt.Errorf("failed to delete %s in host namespace: %v, output: %s", link, err, string(output))
	}
	return nil
}

// LinkIsUpInNamespace 判断命名空间内网卡是否处于 UP 状态（即 IFF_UP 管理状态）。
//
// 注意不能看 operstate：WireGuard 接口即便已拉起，operstate 仍是 UNKNOWN，
// 必须解析 `<...>` 里的标志位。这里只认独立的 UP 标志，正是 `ip link set up/down`
// 所切换的那一位。
func (s *NetnsService) LinkIsUpInNamespace(nsName, link string) bool {
	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "-o", "link", "show", "dev", link)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// 形如：3: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN
	line := string(output)
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start < 0 || end <= start {
		return false
	}

	for _, flag := range strings.Split(line[start+1:end], ",") {
		if strings.TrimSpace(flag) == "UP" {
			return true
		}
	}
	return false
}

// ApplyRateLimit 在命名空间内的隧道接口上实施双向限速。
//
// 方向对应客户端的直观感受：
//   - downloadMbps 限制「服务端 → 设备」的流量，即客户端的下载速率，用 egress 整形（tbf）；
//   - uploadMbps 限制「设备 → 服务端」的流量，即客户端的上传速率，用 ingress 限速
//     （ingress qdisc + police，超速直接丢弃）。ingress 方向无法整形只能限速，
//     对「限速」这个语义来说足够。
//
// 取值为 0 表示该方向不限速；两者都为 0 时清除全部规则，恢复到不限速状态。
//
// qdisc 挂在 netdev 上，因此接口 down/up 都不会丢规则；但命名空间被销毁重建后
// netdev 是新对象，规则随之消失，需要由收敛流程重新下发（见 reconcile）。
func (s *NetnsService) ApplyRateLimit(nsName, link string, downloadMbps, uploadMbps int) error {
	// 先清空既有规则，保证该操作幂等：重复设置不会叠加出多条 qdisc
	if err := s.clearRateLimit(nsName, link); err != nil {
		return err
	}

	if downloadMbps <= 0 && uploadMbps <= 0 {
		return nil
	}

	if downloadMbps > 0 {
		burst := rateLimitBurstBytes(downloadMbps)
		cmd := exec.Command("ip", "netns", "exec", nsName,
			"tc", "qdisc", "replace", "dev", link, "root", "tbf",
			"rate", fmt.Sprintf("%dmbit", downloadMbps),
			"burst", strconv.Itoa(burst),
			"latency", "400ms")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to set download rate limit (%d Mbps) on %s: %v, output: %s",
				downloadMbps, link, err, string(output))
		}
	}

	if uploadMbps > 0 {
		cmd := exec.Command("ip", "netns", "exec", nsName,
			"tc", "qdisc", "add", "dev", link, "handle", "ffff:", "ingress")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add ingress qdisc on %s: %v, output: %s", link, err, string(output))
		}

		cmd = exec.Command("ip", "netns", "exec", nsName,
			"tc", "filter", "add", "dev", link, "parent", "ffff:",
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
func (s *NetnsService) clearRateLimit(nsName, link string) error {
	for _, args := range [][]string{
		{"tc", "qdisc", "del", "dev", link, "root"},
		{"tc", "qdisc", "del", "dev", link, "ingress"},
	} {
		cmd := exec.Command("ip", append([]string{"netns", "exec", nsName}, args...)...)
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

// SetLinkUpInNamespace 在命名空间内拉起网卡（WireGuard 接口在此刻建立监听 socket）。
func (s *NetnsService) SetLinkUpInNamespace(nsName, link string) error {
	return s.SetLinkStateInNamespace(nsName, link, true)
}

// SetLinkStateInNamespace 切换命名空间内隧道接口的启停状态。
//
// 这是「启用/禁用账号」的执行手段：把接口 down 会触发内核销毁该设备的加密
// UDP socket，客户端立即连不上；重新 up 时内核会在设备诞生地（宿主命名空间）
// 重建 socket，因此恢复后监听端口与握手路径跟禁用前完全一致。
//
// 相比删除配置再重建，这样做保留了全部 peer、密钥与路由，禁用期间不产生任何
// 状态丢失，开关可以随时来回切换。
func (s *NetnsService) SetLinkStateInNamespace(nsName, link string, up bool) error {
	state := "down"
	if up {
		state = "up"
	}

	// 拉起隧道前先确保 lo 可用：命名空间内的转发与探测都依赖它
	if up {
		cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "link", "set", "lo", "up")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to bring up lo in namespace %s: %v, output: %s", nsName, err, string(output))
		}
	}

	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "link", "set", "dev", link, state)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set %s %s in namespace %s: %v, output: %s",
			link, state, nsName, err, string(output))
	}
	return nil
}

// AddAddressInNamespace 为命名空间内的网卡配置地址。
func (s *NetnsService) AddAddressInNamespace(nsName, link, address string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "addr", "add", address, "dev", link)
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "File exists") {
			return nil
		}
		return fmt.Errorf("failed to add address %s to %s in namespace %s: %v, output: %s", address, link, nsName, err, string(output))
	}
	return nil
}

// EnableForwardingInNamespace 打开命名空间内的 IPv4 转发。
//
// peer 互访、以及访问某个 peer 背后的下挂网段，都要在命名空间内完成
// 一次转发（包从 wg0 进、再按 cryptokey routing 从 wg0 出），因此这一项必需。
// 命名空间内不再需要任何 iptables 规则：新建 netns 的 FORWARD 策略本就是
// ACCEPT，且该命名空间由单个账号独占。
func (s *NetnsService) EnableForwardingInNamespace(nsName string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "sysctl", "-w", "net.ipv4.ip_forward=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable ip_forward in namespace %s: %v, output: %s", nsName, err, string(output))
	}
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

// AddRouteForPeer 为 peer 声明的网段补一条经由隧道接口的路由。
//
// 必须显式添加。WireGuard 内核模块只维护 cryptokey routing（决定把包加密发给
// 哪个 peer），并不会往内核路由表里写任何路由；wg-quick 正是靠自身的 add_route
// 补上这一步。缺少这条路由时，命名空间内发往对端下挂网段的包会因查不到路由而
// 被丢弃，表现为「隧道通、但访问不到对端内网」。
//
// 默认路由（0.0.0.0/0、::/0）不在此添加——那是客户端把流量送进隧道的行为。
func (s *NetnsService) AddRouteForPeer(nsName, wgInterface, allowedIPs string) error {
	for _, ipRange := range strings.Split(allowedIPs, ",") {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" || isDefaultRoute(ipRange) {
			continue
		}

		cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "route", "add", ipRange, "dev", wgInterface)
		if output, err := cmd.CombinedOutput(); err != nil {
			if !strings.Contains(string(output), "File exists") {
				return fmt.Errorf("failed to add route for %s: %v, output: %s", ipRange, err, string(output))
			}
		}
	}
	return nil
}

// DeleteRouteForPeer 删除 peer 的 allowedIPs 路由。
func (s *NetnsService) DeleteRouteForPeer(nsName, wgInterface, allowedIPs string) error {
	for _, ipRange := range strings.Split(allowedIPs, ",") {
		ipRange = strings.TrimSpace(ipRange)
		if ipRange == "" || isDefaultRoute(ipRange) {
			continue
		}

		cmd := exec.Command("ip", "netns", "exec", nsName, "ip", "route", "del", ipRange, "dev", wgInterface)
		if output, err := cmd.CombinedOutput(); err != nil {
			if !strings.Contains(string(output), "No such process") && !strings.Contains(string(output), "not found") {
				return fmt.Errorf("failed to delete route for %s: %v, output: %s", ipRange, err, string(output))
			}
		}
	}
	return nil
}

// isDefaultRoute 判断是否是全局默认路由。
func isDefaultRoute(cidr string) bool {
	return cidr == "0.0.0.0/0" || cidr == "::/0"
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
func (s *NetnsService) RemoveHostNAT(subnet, outInterface string) {
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
func (s *NetnsService) RemoveLegacyPortForwarding(outInterface string, port int, nsIP, protocol string) {
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
