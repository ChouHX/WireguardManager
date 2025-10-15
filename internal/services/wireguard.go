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

// WireguardConfig WireGuard配置
type WireguardConfig struct {
	InterfaceName string // 接口名称 (如 wg0)
	ListenPort    int    // 监听端口
	PrivateKey    string // 私钥
	PublicKey     string // 公钥
	Address       string // 接口IP地址 (CIDR格式)
	VethInterface string // veth接口名称 (命名空间内的接口，如 veth-ns-xxx)
	OutInterface  string // 外网接口名称 (如 eth0)，用于NAT到外网
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

// CreateConfig 创建WireGuard配置文件
func (s *WireguardService) CreateConfig(username string, config *WireguardConfig) (string, error) {
	// 确保配置目录存在
	userConfigDir := filepath.Join(s.configDir, username)
	if err := os.MkdirAll(userConfigDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %v", err)
	}

	// 配置文件路径
	configPath := filepath.Join(userConfigDir, fmt.Sprintf("%s.conf", config.InterfaceName))

	// 生成配置内容
	// PostUp/PostDown 规则：
	// 0. 启用 IP 转发（命名空间内）
	// 1. 允许 WireGuard 流量通过 veth 接口转发
	// 2. 允许 veth 流量通过外网接口转发（访问外网）
	// 3. 对外网流量进行 NAT
	configContent := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
ListenPort = %d
SaveConfig = false

# PostUp 规则：
# 0. 启用 IP 转发（关键：必须在命名空间内启用）
PostUp = sysctl -w net.ipv4.ip_forward=1
# 1. WireGuard <-> veth 转发
PostUp = iptables -A FORWARD -i %%i -o %s -j ACCEPT
PostUp = iptables -A FORWARD -i %s -o %%i -j ACCEPT
# 2. veth <-> 外网接口转发（关键：允许访问外网）
PostUp = iptables -A FORWARD -i %s -o %s -j ACCEPT
PostUp = iptables -A FORWARD -i %s -o %s -j ACCEPT
# 3. NAT规则：关键 - 对 WireGuard 网段通过 veth 出去的流量进行 MASQUERADE
PostUp = iptables -t nat -A POSTROUTING -s %s -o %s -j MASQUERADE
# 4. NAT规则：对通过外网接口出去的流量进行MASQUERADE
PostUp = iptables -t nat -A POSTROUTING -o %s -j MASQUERADE

# PostDown 规则：清理上述规则
PostDown = iptables -D FORWARD -i %%i -o %s -j ACCEPT
PostDown = iptables -D FORWARD -i %s -o %%i -j ACCEPT
PostDown = iptables -D FORWARD -i %s -o %s -j ACCEPT
PostDown = iptables -D FORWARD -i %s -o %s -j ACCEPT
PostDown = iptables -t nat -D POSTROUTING -s %s -o %s -j MASQUERADE
PostDown = iptables -t nat -D POSTROUTING -o %s -j MASQUERADE
`,
		config.PrivateKey,
		config.Address,
		config.ListenPort,
		// PostUp
		config.VethInterface,
		config.VethInterface,
		config.VethInterface, config.OutInterface,
		config.OutInterface, config.VethInterface,
		config.Address, config.VethInterface, // NAT for WireGuard subnet
		config.OutInterface,
		// PostDown
		config.VethInterface,
		config.VethInterface,
		config.VethInterface, config.OutInterface,
		config.OutInterface, config.VethInterface,
		config.Address, config.VethInterface, // NAT for WireGuard subnet
		config.OutInterface,
	)

	// 写入配置文件
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return "", fmt.Errorf("failed to write config file: %v", err)
	}

	return configPath, nil
}

// StartWireguardInNamespace 在命名空间中启动WireGuard
func (s *WireguardService) StartWireguardInNamespace(nsName, configPath string) error {
	// 在命名空间中启动WireGuard
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg-quick", "up", configPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start wireguard in namespace: %v, output: %s", err, string(output))
	}
	return nil
}

// StopWireguardInNamespace 在命名空间中停止WireGuard
func (s *WireguardService) StopWireguardInNamespace(nsName, configPath string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg-quick", "down", configPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop wireguard in namespace: %v, output: %s", err, string(output))
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

// AddPeer 添加WireGuard peer
func (s *WireguardService) AddPeer(nsName, interfaceName, peerPublicKey, allowedIPs, endpoint string) error {
	args := []string{"netns", "exec", nsName, "wg", "set", interfaceName, "peer", peerPublicKey, "allowed-ips", allowedIPs}
	if endpoint != "" {
		args = append(args, "endpoint", endpoint)
	}
	
	cmd := exec.Command("ip", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add peer: %v, output: %s", err, string(output))
	}
	return nil
}

// RemovePeer 移除WireGuard peer
func (s *WireguardService) RemovePeer(nsName, interfaceName, peerPublicKey string) error {
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg", "set", interfaceName, "peer", peerPublicKey, "remove")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove peer: %v, output: %s", err, string(output))
	}
	return nil
}

// GenerateClientConfig 生成客户端配置
func (s *WireguardService) GenerateClientConfig(serverPublicKey, serverEndpoint, clientPrivateKey, clientAddress, allowedIPs string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 8.8.8.8

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = %s
PersistentKeepalive = 25
`,
		clientPrivateKey,
		clientAddress,
		serverPublicKey,
		serverEndpoint,
		allowedIPs,
	)
}

// GetConfigPath 获取配置文件路径
func (s *WireguardService) GetConfigPath(username, interfaceName string) string {
	return filepath.Join(s.configDir, username, fmt.Sprintf("%s.conf", interfaceName))
}

// GetDetailedStats 获取详细的WireGuard统计信息
func (s *WireguardService) GetDetailedStats(nsName, interfaceName string) (*models.WireguardServerStats, error) {
	cmd := exec.Command("ip", "netns", "exec", nsName, "wg", "show", interfaceName, "dump")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get wireguard stats: %v, output: %s", err, string(output))
	}

	return s.parseWireguardDump(string(output), interfaceName)
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
