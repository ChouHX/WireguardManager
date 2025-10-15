package services

import (
	"cloud-platform/internal/models"
	"fmt"
	"hash/fnv"
	"strings"
)

// UserNetworkService 用户网络配置服务
type UserNetworkService struct {
	netnsService     *NetnsService
	wireguardService *WireguardService
	baseSubnet       string // 基础子网 (如 "10.200")
	basePort         int    // WireGuard起始端口 (如 51820)
	outInterface     string // 外网接口 (如 "eth0")
}

// NewUserNetworkService 创建用户网络配置服务
func NewUserNetworkService(configDir, baseSubnet string, basePort int, outInterface string) *UserNetworkService {
	return &UserNetworkService{
		netnsService:     NewNetnsService(),
		wireguardService: NewWireguardService(configDir),
		baseSubnet:       baseSubnet,
		basePort:         basePort,
		outInterface:     outInterface,
	}
}

// ProvisionUserNetwork 为用户配置完整的网络环境
// 返回 WireguardServer 对象，调用方负责保存到数据库
func (s *UserNetworkService) ProvisionUserNetwork(user *models.User) (*models.WireguardServer, error) {
	// 1. 生成配置参数（基于UserUID）
	nsName := s.generateNamespaceName(user.UserUID)
	wgInterface := "wg0"
	
	// 使用UserUID的哈希值生成端口和子网ID（避免冲突）
	userHash := s.hashUserUID(user.UserUID)
	wgPort := s.basePort + (userHash % 10000) // 限制在10000个端口范围内
	vethHost := fmt.Sprintf("veth-h-%s", user.UserUID[:6])
	vethNs := fmt.Sprintf("veth-ns-%s", user.UserUID[:6])
	
	// IP地址分配
	// 为每个用户分配独立的网段（使用哈希值的低8位作为子网ID）
	userSubnetID := (userHash % 254) + 1 // 1-254，避免0和255
	
	// veth 对使用 /30 子网（只需要2个IP：.1给主机，.2给命名空间）
	vethSubnet := fmt.Sprintf("%s.%d.0/30", s.baseSubnet, userSubnetID)
	hostIP := fmt.Sprintf("%s.%d.1/30", s.baseSubnet, userSubnetID)
	nsIP := fmt.Sprintf("%s.%d.2/30", s.baseSubnet, userSubnetID)
	
	// WireGuard 使用独立的子网（10.100.X.0/24），避免与 veth 冲突
	wgIP := fmt.Sprintf("10.100.%d.1/24", userSubnetID)

	// 2. 创建网络命名空间
	if err := s.netnsService.CreateNamespace(nsName); err != nil {
		return nil, fmt.Errorf("failed to create namespace: %v", err)
	}

	// 3. 创建veth对并配置网络
	if err := s.netnsService.CreateVethPair(vethHost, vethNs, nsName, nsIP, hostIP); err != nil {
		// 失败时清理命名空间
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to create veth pair: %v", err)
	}

	// 4. 启用NAT
	if err := s.netnsService.EnableNAT(vethSubnet, s.outInterface); err != nil {
		// 失败时清理
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to enable NAT: %v", err)
	}

	// 5. 生成WireGuard密钥
	privateKey, publicKey, err := s.wireguardService.GenerateKeys()
	if err != nil {
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to generate wireguard keys: %v", err)
	}

	// 6. 创建WireGuard配置
	wgConfig := &WireguardConfig{
		InterfaceName: wgInterface,
		ListenPort:    wgPort,
		PrivateKey:    privateKey,
		PublicKey:     publicKey,
		Address:       wgIP,
		VethInterface: vethNs,         // 命名空间内的 veth 接口名称
		OutInterface:  s.outInterface, // 外网接口（用于NAT到外网）
	}

	configPath, err := s.wireguardService.CreateConfig(user.UserUID, wgConfig)
	if err != nil {
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to create wireguard config: %v", err)
	}

	// 7. 在命名空间中启动WireGuard
	if err := s.wireguardService.StartWireguardInNamespace(nsName, configPath); err != nil {
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to start wireguard: %v", err)
	}

	// 8. 设置端口转发：将主机的 WireGuard 端口转发到命名空间内
	// 提取命名空间内的IP地址（去掉CIDR）
	nsIPAddr := strings.Split(nsIP, "/")[0]
	if err := s.netnsService.SetupPortForwarding(s.outInterface, wgPort, nsIPAddr, wgPort, "udp"); err != nil {
		s.wireguardService.StopWireguardInNamespace(nsName, configPath)
		s.netnsService.DeleteNamespace(nsName)
		return nil, fmt.Errorf("failed to setup port forwarding: %v", err)
	}

	// 9. 创建 WireguardServer 对象
	wgServer := &models.WireguardServer{
		UserID:       user.ID,
		Namespace:    nsName,
		WgInterface:  wgInterface,
		WgPort:       wgPort,
		WgPublicKey:  publicKey,
		WgPrivateKey: privateKey,
		WgAddress:    wgIP,
	}

	return wgServer, nil
}

// DestroyUserNetwork 销毁用户的网络环境
func (s *UserNetworkService) DestroyUserNetwork(server *models.WireguardServer, userUID string) error {
	if server.Namespace == "" {
		return nil // 没有配置过网络环境
	}

	// 1. 计算命名空间内的IP地址（用于删除端口转发规则）
	userHash := s.hashUserUID(userUID)
	userSubnetID := (userHash % 254) + 1
	nsIPAddr := fmt.Sprintf("%s.%d.2", s.baseSubnet, userSubnetID)

	// 2. 删除端口转发规则
	s.netnsService.RemovePortForwarding(s.outInterface, server.WgPort, nsIPAddr, server.WgPort, "udp")

	// 3. 停止WireGuard
	configPath := s.wireguardService.GetConfigPath(userUID, server.WgInterface)
	// 忽略停止错误，继续清理
	s.wireguardService.StopWireguardInNamespace(server.Namespace, configPath)

	// 4. 删除命名空间 (会自动清理其中的网络接口)
	if err := s.netnsService.DeleteNamespace(server.Namespace); err != nil {
		return fmt.Errorf("failed to delete namespace: %v", err)
	}

	return nil
}

// generateNamespaceName 生成命名空间名称（基于UserUID）
func (s *UserNetworkService) generateNamespaceName(userUID string) string {
	// 使用UserUID直接作为命名空间名称的一部分
	// UserUID是8字符的十六进制字符串，天然符合命名规则
	return fmt.Sprintf("wg_%s", userUID)
}

// hashUserUID 对UserUID进行哈希，用于生成端口号和子网ID
func (s *UserNetworkService) hashUserUID(userUID string) int {
	h := fnv.New32a()
	h.Write([]byte(userUID))
	return int(h.Sum32())
}

