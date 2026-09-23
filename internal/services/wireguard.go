package services

import (
	"cloud-platform/internal/models"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WireguardService WireGuard服务
type WireguardService struct {
	configDir string
}

// NewWireguardService 创建WireGuard服务实例
func NewWireguardService(configDir string) *WireguardService {
	return &WireguardService{
		configDir: configDir,
	}
}

// WireguardConfig 服务端接口配置。
//
// 不再包含 veth / 出口网卡：加密报文由宿主命名空间的 socket 直接收发，
// 明文流量的转发与伪装已移出本层。
type WireguardConfig struct {
	InterfaceName string // 接口名称 (如 wg0)
	ListenPort    int    // 监听端口
	PrivateKey    string // 私钥
	PublicKey     string // 公钥
	Address       string // 接口IP地址 (CIDR格式)
}

// ServerAllowedIPs 计算服务端为某个 peer 声明的 allowed-ips：
// 设备自身地址 + 它背后的网段（用于跨网段转发）。
//
// 全局代理（0.0.0.0/0、::/0）会被排除——那是客户端把流量送进隧道的行为，
// 若写进服务端 allowed-ips，会导致所有流量都被转发给该设备。
func ServerAllowedIPs(peer *models.WireguardPeer) string {
	self := peer.PeerAddress + "/32"
	parts := []string{self}

	for _, cidr := range strings.Split(peer.AllowedIPs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" || cidr == self || cidr == peer.PeerAddress {
			continue
		}
		if cidr == "0.0.0.0/0" || cidr == "::/0" {
			continue
		}
		parts = append(parts, cidr)
	}

	return strings.Join(parts, ",")
}

// GenerateKeys 生成WireGuard密钥对
func (s *WireguardService) GenerateKeys() (privateKey, publicKey string, err error) {
	// 生成私钥
	cmd := exec.Command("wg", "genkey")
	privOutput, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %v", err)
	}
	privateKey = strings.TrimSpace(string(privOutput))

	// 从私钥生成公钥
	cmd = exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey)
	pubOutput, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %v", err)
	}
	publicKey = strings.TrimSpace(string(pubOutput))

	return privateKey, publicKey, nil
}

// CreateConfig 写入服务端接口的配置记录，并落一份私钥文件供 wg set 使用。
//
// 生成的是 WireGuard 原生格式：只有 [Interface] 段。旧版写在这里的 Address 与
// PostUp/PostDown 属于 wg-quick 专属语法，wg 的解析器既不认识、也不再需要——
// 接口地址由命名空间层用 ip addr 直接下发，而加密报文全程不经过任何转发或
// NAT，那批 iptables 规则随之消失。
//
// 返回的配置文件仅作为可读记录（便于人工排查与审计），真正生效的配置通过
// wg set 逐项下发。
func (s *WireguardService) CreateConfig(userUID string, config *WireguardConfig) (string, error) {
	userConfigDir := filepath.Join(s.configDir, userUID)
	if err := os.MkdirAll(userConfigDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %v", err)
	}

	configPath := filepath.Join(userConfigDir, fmt.Sprintf("%s.conf", config.InterfaceName))

	configContent := fmt.Sprintf(`# 账号 %s 的服务端接口配置记录（由平台自动生成，请勿手工修改）。
#
# 加密报文由宿主命名空间的 UDP socket 直接收发，因此这里没有
# Address 与 PostUp/PostDown：地址由命名空间层下发，转发与 NAT 均不需要。
[Interface]
PrivateKey = %s
ListenPort = %d
`, userUID, config.PrivateKey, config.ListenPort)

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return "", fmt.Errorf("failed to write config file: %v", err)
	}

	if err := s.writePrivateKeyFile(userUID, config.PrivateKey); err != nil {
		return "", err
	}

	return configPath, nil
}

// PrivateKeyPath 返回账号私钥文件路径（供 wg set 读取）。
func (s *WireguardService) PrivateKeyPath(userUID string) string {
	return filepath.Join(s.configDir, userUID, "private.key")
}

// writePrivateKeyFile 落一份 0600 的私钥文件。
// wg set 只接受从文件读取私钥，因此这一步是接口配置的前置条件。
func (s *WireguardService) writePrivateKeyFile(userUID, privateKey string) error {
	path := s.PrivateKeyPath(userUID)
	if err := os.WriteFile(path, []byte(privateKey+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write private key file: %v", err)
	}
	// 已存在的文件不会被 WriteFile 收紧权限，显式兜一次
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("failed to secure private key file: %v", err)
	}
	return nil
}

// ApplyInterfaceInNamespace 用 wg set 逐项下发接口参数。
//
// 刻意不用 wg setconf/syncconf：二者都以整份配置为单位，setconf 会把未出现在
// 配置里的 peer 全部移除，syncconf 也要求配置覆盖全部 peer。接口参数（私钥、
// 监听端口）与 peer 列表的生命周期完全独立，用 wg set 只改前者，任何时刻执行
// 都不会波及已下发的设备。
//
// 设置 listen-port 会触发内核重建 UDP socket；因为接口诞生于宿主命名空间，
// 重建后的 socket 依然落在宿主命名空间，这正是握手稳定性的来源。
func (s *WireguardService) ApplyInterfaceInNamespace(nsName, interfaceName, userUID string, listenPort int) error {
	cmd := exec.Command("ip", "netns", "exec", nsName,
		"wg", "set", interfaceName,
		"private-key", s.PrivateKeyPath(userUID),
		"listen-port", strconv.Itoa(listenPort))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to apply interface config for %s in namespace %s: %v, output: %s",
			interfaceName, nsName, err, string(output))
	}
	return nil
}

// GetWireguardStatus 获取WireGuard状态
func (s *WireguardService) GetWireguardStatus(nsName, interfaceName string) (string, error) {
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg", "show", interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get wireguard status: %v, output: %s", err, string(output))
	}
	return string(output), nil
}

// GeneratePresharedKey 生成预共享密钥（对称加密，服务端与客户端共用同一个值）。
func (s *WireguardService) GeneratePresharedKey() (string, error) {
	cmd := exec.Command("wg", "genpsk")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to generate preshared key: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// SetPeerAllowedIPs 更新已存在 peer 的 allowed-ips。
// 这是 WireGuard 的加密路由表，决定把哪些目标网段的流量发给该 peer。
func (s *WireguardService) SetPeerAllowedIPs(nsName, interfaceName, peerPublicKey, allowedIPs string) error {
	if strings.TrimSpace(allowedIPs) == "" {
		return fmt.Errorf("allowed-ips must not be empty")
	}

	// 同时补设服务端保活：该函数会在修改网段、切换 PSK 等路径上被调用，
	// 只更新 allowed-ips 会让保活停留在创建时的状态；对早期创建的 peer
	// （服务端保活特性上线之前）来说，那就等于永不发送保活包。
	cmd := exec.Command("ip", "netns", "exec", nsName,
		"wg", "set", interfaceName, "peer", peerPublicKey,
		"allowed-ips", allowedIPs,
		"persistent-keepalive", strconv.Itoa(serverKeepaliveSeconds))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set peer allowed-ips: %v, output: %s", err, string(output))
	}

	InvalidateStatsCache(nsName, interfaceName)
	return nil
}

// SetPeerPresharedKey 为已存在的 peer 设置预共享密钥。
// wg 要求从文件读取密钥，这里写入临时文件（0600）后立即删除。
func (s *WireguardService) SetPeerPresharedKey(nsName, interfaceName, peerPublicKey, presharedKey string) error {
	if strings.TrimSpace(presharedKey) == "" {
		return fmt.Errorf("preshared key must not be empty")
	}

	file, err := os.CreateTemp("", "wg-psk-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for preshared key: %v", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if _, err := file.WriteString(presharedKey + "\n"); err != nil {
		return fmt.Errorf("failed to write preshared key: %v", err)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("failed to secure preshared key file: %v", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to flush preshared key: %v", err)
	}

	cmd := exec.Command("ip", "netns", "exec", nsName,
		"wg", "set", interfaceName, "peer", peerPublicKey, "preshared-key", file.Name())
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set preshared key: %v, output: %s", err, string(output))
	}
	return nil
}

// serverKeepaliveSeconds 服务端向设备发送保活包的间隔。
//
// 必要性：WireGuard 客户端的 persistent-keepalive 定时器只有在「收到对端数据包」
// 后才会续期（内核的 timer_need_another_keepalive 标志）。服务端若从不主动发包，
// 客户端会在首个保活之后停止发送，直到 120 秒重协商才恢复——表现为「设了 25 秒
// 却两分钟才动一次」。双向保活后，客户端的保活会持续生效，接收方向的流量
// 才能作为可靠的在线证据。
const serverKeepaliveSeconds = 10

// AddPeer 添加WireGuard peer
func (s *WireguardService) AddPeer(nsName, interfaceName, peerPublicKey, allowedIPs, endpoint string) error {
	args := []string{
		"netns", "exec", nsName,
		"wg", "set", interfaceName, "peer", peerPublicKey,
		"allowed-ips", allowedIPs,
		"persistent-keepalive", strconv.Itoa(serverKeepaliveSeconds),
	}
	if endpoint != "" {
		args = append(args, "endpoint", endpoint)
	}

	cmd := exec.Command("ip", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add peer: %v, output: %s", err, string(output))
	}

	// 新增设备后让缓存立即失效，否则在一个 TTL 窗口内它不会出现在流量列表里
	InvalidateStatsCache(nsName, interfaceName)
	return nil
}

// EnsurePeerKeepalive 为已存在的 peer 补设服务端保活。
//
// 用途：服务端保活是后加的配置项，此前创建的 peer 从未设置过它，
// 于是服务端不会主动发包，客户端表现为"0 B received"、握手长期不更新。
// 启动时对全部 peer 执行一次即可修正，成本极低。
func (s *WireguardService) EnsurePeerKeepalive(nsName, interfaceName string, peers []string) error {
	for _, publicKey := range peers {
		cmd := exec.Command("ip", "netns", "exec", nsName,
			"wg", "set", interfaceName, "peer", publicKey,
			"persistent-keepalive", strconv.Itoa(serverKeepaliveSeconds))
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to ensure keepalive for %s: %v, output: %s",
				publicKey, err, string(output))
		}
	}
	return nil
}

// RemovePeer 移除WireGuard peer
func (s *WireguardService) RemovePeer(nsName, interfaceName, peerPublicKey string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg", "set", interfaceName, "peer", peerPublicKey, "remove")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove peer: %v, output: %s", err, string(output))
	}

	// 删除设备后让缓存立即失效，否则已删除的设备会在一个 TTL 窗口内继续显示
	InvalidateStatsCache(nsName, interfaceName)
	return nil
}

// GetConfigPath 获取配置文件路径
func (s *WireguardService) GetConfigPath(username, interfaceName string) string {
	return filepath.Join(s.configDir, username, fmt.Sprintf("%s.conf", interfaceName))
}

// 统计数据缓存：一次 wg show 同时服务于流量接口与在线判定，
// 避免同一时间窗内为同一个接口重复 spawn 进程。
const statsCacheTTL = time.Second

type statsCacheEntry struct {
	stats *models.WireguardServerStats
	at    time.Time
}

var (
	statsCacheMu sync.Mutex
	statsCache   = map[string]statsCacheEntry{}
)

// GetDetailedStats 获取详细的WireGuard统计信息（带 1 秒缓存）。
// 注意：返回的是副本，调用方可以安全地改写字段（例如补充设备备注）。
func (s *WireguardService) GetDetailedStats(nsName, interfaceName string) (*models.WireguardServerStats, error) {
	key := nsName + "/" + interfaceName

	statsCacheMu.Lock()
	if entry, ok := statsCache[key]; ok && time.Since(entry.at) < statsCacheTTL {
		statsCacheMu.Unlock()
		return cloneStats(entry.stats), nil
	}
	statsCacheMu.Unlock()

	cmd := exec.Command("ip", "netns", "exec", nsName, "wg", "show", interfaceName, "dump")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get wireguard stats: %v, output: %s", err, string(output))
	}

	stats, err := s.parseWireguardDump(string(output), interfaceName)
	if err != nil {
		return nil, err
	}

	statsCacheMu.Lock()
	// 顺手清理长期未访问的条目，避免接口删除后缓存残留
	for cacheKey, entry := range statsCache {
		if time.Since(entry.at) > 8*statsCacheTTL {
			delete(statsCache, cacheKey)
		}
	}
	statsCache[key] = statsCacheEntry{stats: stats, at: time.Now()}
	statsCacheMu.Unlock()

	return cloneStats(stats), nil
}

// InvalidateStatsCache 让指定接口的缓存立即失效（设备增删后调用）。
func InvalidateStatsCache(nsName, interfaceName string) {
	statsCacheMu.Lock()
	delete(statsCache, nsName+"/"+interfaceName)
	statsCacheMu.Unlock()
}

func cloneStats(stats *models.WireguardServerStats) *models.WireguardServerStats {
	if stats == nil {
		return nil
	}
	out := *stats
	out.Peers = make([]models.WireguardPeerStats, len(stats.Peers))
	copy(out.Peers, stats.Peers)
	return &out
}

// parseWireguardDump 解析 wg show dump 输出
// 输出格式：
// 第一行：interface private-key public-key listen-port fwmark
// 后续行：public-key preshared-key endpoint allowed-ips latest-handshake transfer-rx transfer-tx persistent-keepalive
func (s *WireguardService) parseWireguardDump(output, interfaceName string) (*models.WireguardServerStats, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty output")
	}

	stats := &models.WireguardServerStats{
		Interface: interfaceName,
		Peers:     []models.WireguardPeerStats{},
	}

	// 解析第一行（接口信息）
	interfaceFields := strings.Fields(lines[0])
	if len(interfaceFields) >= 4 {
		stats.PublicKey = interfaceFields[2]
		if port, err := strconv.Atoi(interfaceFields[3]); err == nil {
			stats.ListenPort = port
		}
	}

	// 解析peers信息
	for i := 1; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) < 8 {
			continue
		}

		peerStats := models.WireguardPeerStats{
			PublicKey:  fields[0],
			Endpoint:   fields[2],
			AllowedIPs: fields[3],
		}

		// 解析最后握手时间
		if handshake, err := strconv.ParseInt(fields[4], 10, 64); err == nil && handshake > 0 {
			peerStats.LatestHandshake = time.Unix(handshake, 0)
		}

		// 解析接收字节数
		if rx, err := strconv.ParseInt(fields[5], 10, 64); err == nil {
			peerStats.TransferRx = rx
			stats.TotalRx += rx
		}

		// 解析发送字节数
		if tx, err := strconv.ParseInt(fields[6], 10, 64); err == nil {
			peerStats.TransferTx = tx
			stats.TotalTx += tx
		}

		// 解析persistent keepalive
		if keepalive, err := strconv.Atoi(fields[7]); err == nil {
			peerStats.PersistentKeepalive = keepalive
		}

		stats.Peers = append(stats.Peers, peerStats)
	}

	stats.PeerCount = len(stats.Peers)
	return stats, nil
}

// GetPeerStatsMap 获取peer统计信息的映射（以公钥为key）
func (s *WireguardService) GetPeerStatsMap(nsName, interfaceName string) (map[string]*models.WireguardPeerStats, error) {
	stats, err := s.GetDetailedStats(nsName, interfaceName)
	if err != nil {
		return nil, err
	}

	peerMap := make(map[string]*models.WireguardPeerStats)
	for i := range stats.Peers {
		peerMap[stats.Peers[i].PublicKey] = &stats.Peers[i]
	}

	return peerMap, nil
}

// ParseWireguardShowOutput 解析 wg show 的人类可读输出（备用方法）
func (s *WireguardService) ParseWireguardShowOutput(output string) (*models.WireguardServerStats, error) {
	stats := &models.WireguardServerStats{
		Peers: []models.WireguardPeerStats{},
	}

	// 正则表达式匹配
	interfaceRegex := regexp.MustCompile(`interface:\s+(\w+)`)
	publicKeyRegex := regexp.MustCompile(`public key:\s+([A-Za-z0-9+/=]+)`)
	portRegex := regexp.MustCompile(`listening port:\s+(\d+)`)
	endpointRegex := regexp.MustCompile(`endpoint:\s+([^\s]+)`)
	allowedIPsRegex := regexp.MustCompile(`allowed ips:\s+([^\n]+)`)
	transferRegex := regexp.MustCompile(`transfer:\s+([0-9.]+\s+\w+)\s+received,\s+([0-9.]+\s+\w+)\s+sent`)

	// 解析接口信息
	if match := interfaceRegex.FindStringSubmatch(output); len(match) > 1 {
		stats.Interface = match[1]
	}
	if match := publicKeyRegex.FindStringSubmatch(output); len(match) > 1 {
		stats.PublicKey = match[1]
	}
	if match := portRegex.FindStringSubmatch(output); len(match) > 1 {
		if port, err := strconv.Atoi(match[1]); err == nil {
			stats.ListenPort = port
		}
	}

	// 按peer分割
	peerSections := strings.Split(output, "peer:")
	for _, section := range peerSections[1:] {
		peerStats := models.WireguardPeerStats{}

		// 公钥在section开头
		lines := strings.Split(section, "\n")
		if len(lines) > 0 {
			peerStats.PublicKey = strings.TrimSpace(lines[0])
		}

		// 解析其他字段
		if match := endpointRegex.FindStringSubmatch(section); len(match) > 1 {
			peerStats.Endpoint = match[1]
		}
		if match := allowedIPsRegex.FindStringSubmatch(section); len(match) > 1 {
			peerStats.AllowedIPs = strings.TrimSpace(match[1])
		}

		// 解析流量（需要转换单位）
		if match := transferRegex.FindStringSubmatch(section); len(match) > 2 {
			peerStats.TransferRx = parseTransferSize(match[1])
			peerStats.TransferTx = parseTransferSize(match[2])
			stats.TotalRx += peerStats.TransferRx
			stats.TotalTx += peerStats.TransferTx
		}

		stats.Peers = append(stats.Peers, peerStats)
	}

	stats.PeerCount = len(stats.Peers)
	return stats, nil
}

// parseTransferSize 解析流量大小（如 "1.23 MiB" -> 字节数）
func parseTransferSize(sizeStr string) int64 {
	parts := strings.Fields(strings.TrimSpace(sizeStr))
	if len(parts) != 2 {
		return 0
	}

	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToUpper(parts[1])
	multiplier := int64(1)

	switch unit {
	case "B":
		multiplier = 1
	case "KIB":
		multiplier = 1024
	case "MIB":
		multiplier = 1024 * 1024
	case "GIB":
		multiplier = 1024 * 1024 * 1024
	case "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	}

	return int64(value * float64(multiplier))
}
