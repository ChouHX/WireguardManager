package services

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/models"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxSubnetID 账号隧道网段第三段的取值范围上限（10.100.<id>.0/24）。
const maxSubnetID = 254

// ErrNoAvailableSubnet 表示隧道网段已用尽。
var ErrNoAvailableSubnet = errors.New("no available tunnel subnet")

// NetworkAllocation 描述一个已存在的账号网络占用的资源，供新账号分配时避让。
type NetworkAllocation struct {
	WgPort   int
	SubnetID int
}

// AllocationsFromServers 从数据库记录提取已被占用的端口与网段号。
func AllocationsFromServers(servers []models.WireguardServer) []NetworkAllocation {
	allocations := make([]NetworkAllocation, 0, len(servers))
	for _, server := range servers {
		allocations = append(allocations, NetworkAllocation{
			WgPort:   server.WgPort,
			SubnetID: subnetIDFromAddress(server.WgAddress),
		})
	}
	return allocations
}

// UserNetworkService provisions one host interface, UDP port and routing table per account.
type UserNetworkService struct {
	interfaceService *InterfaceService
	wireguardService *WireguardService
	baseSubnet       string // 旧版 veth 网段前缀，仅用于回收历史规则
	basePort         int    // WireGuard 监听端口起始值
	outInterface     string // 出口网卡，仅用于回收历史规则
	mtu              int    // 隧道接口 MTU；0 表示不干预，沿用内核默认
}

// NewUserNetworkServiceFromRuntime 用运行时配置构造（config.yaml 仅作初始默认值）。
func NewUserNetworkServiceFromRuntime() *UserNetworkService {
	cfg := config.AppConfig
	settings := GetSettings()

	baseSubnet := settings.String(SettingNetworkBaseSubnet, cfg.Network.BaseSubnet)
	basePort := settings.Int(SettingNetworkBasePort, cfg.Network.BasePort)
	outInterface := settings.String(SettingNetworkOutInterface, cfg.Network.OutInterface)
	mtu := settings.Int(SettingNetworkMTU, cfg.Network.MTU)

	return NewUserNetworkService(cfg.Network.ConfigDir, baseSubnet, basePort, outInterface, mtu)
}

func NewUserNetworkService(configDir, baseSubnet string, basePort int, outInterface string, mtu int) *UserNetworkService {
	return &UserNetworkService{
		interfaceService: NewInterfaceService(),
		wireguardService: NewWireguardService(configDir),
		baseSubnet:       baseSubnet,
		basePort:         basePort,
		outInterface:     outInterface,
		mtu:              mtu,
	}
}

// ProvisionUserNetwork 为新账号编排网络环境。
//
// existing 由调用方从数据库读取，用于避让已被占用的端口与网段；服务层不直接
// 访问数据库，避免与 database 包形成循环依赖。
// 返回 WireguardServer 对象，调用方负责保存到数据库。
func (s *UserNetworkService) ProvisionUserNetwork(user *models.User, existing []NetworkAllocation) (*models.WireguardServer, error) {

	port, subnetID, err := s.allocateNetwork(existing)
	if err != nil {
		return nil, err
	}

	privateKey, publicKey, err := s.wireguardService.GenerateKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate wireguard keys: %v", err)
	}

	wgIP := tunnelAddress(subnetID)
	wgInterface := TenantInterface(subnetID)

	server := &models.WireguardServer{
		UserID:       user.ID,
		Namespace:    "wg_" + user.UserUID, // legacy unique column, no active namespace
		NetworkMode:  NetworkModeMultiInterface,
		Enabled:      true,
		WgInterface:  wgInterface,
		WgPort:       port,
		WgPublicKey:  publicKey,
		WgPrivateKey: privateKey,
		WgAddress:    wgIP,
	}

	if err := s.createNetwork(server, user.UserUID); err != nil {
		return nil, err
	}

	return server, nil
}

// EnsureUserNetwork rebuilds the tenant from persisted credentials. Startup runs
// before HTTP handlers and probes, so no peer mutations race with the rebuild.
func (s *UserNetworkService) EnsureUserNetwork(server *models.WireguardServer, userUID string) error {
	id := subnetIDFromAddress(server.WgAddress)
	if id == 0 || server.WgPrivateKey == "" || server.WgPort < 1 || server.WgPort > 65535 {
		return fmt.Errorf("server %d has invalid network parameters", server.ID)
	}
	link := TenantInterface(id)
	if s.interfaceService.LinkExists(link) {
		if err := s.verifyOwnership(link, userUID); err != nil {
			return err
		}
		if err := s.interfaceService.Destroy(link, server.WgAddress); err != nil {
			return err
		}
	}
	if server.NetworkMode != NetworkModeMultiInterface {
		if err := s.removeLegacyNamespace(server, userUID); err != nil {
			return err
		}
		s.removeLegacyForwarding(server, userUID)
	}
	desired := *server
	desired.WgInterface = link
	desired.NetworkMode = NetworkModeMultiInterface
	if err := s.createNetwork(&desired, userUID); err != nil {
		return err
	}
	*server = desired
	return nil
}

func (s *UserNetworkService) verifyOwnership(link, userUID string) error {
	out, err := s.interfaceService.run("ip", "-o", "link", "show", "dev", link)
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "alias wireguard-manager:"+userUID+" ") &&
		!strings.HasSuffix(strings.TrimSpace(string(out)), "alias wireguard-manager:"+userUID) {
		return fmt.Errorf("interface %s already exists without this account's ownership marker", link)
	}
	return nil
}

func (s *UserNetworkService) createNetwork(server *models.WireguardServer, userUID string) (result error) {
	prefix, err := netip.ParsePrefix(server.WgAddress)
	if err != nil || !prefix.Addr().Is4() {
		return fmt.Errorf("invalid tunnel address %q", server.WgAddress)
	}

	link := server.WgInterface
	if err := s.interfaceService.CreateWireguardDevice(link); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			if err := s.interfaceService.Destroy(link, server.WgAddress); err != nil {
				result = errors.Join(result, fmt.Errorf("rollback: %w", err))
			}
		}
	}()
	if err := s.interfaceService.command("ip", "link", "set", "dev", link, "alias", "wireguard-manager:"+userUID); err != nil {
		return err
	}
	if _, err := s.wireguardService.CreateConfig(userUID, &WireguardConfig{
		InterfaceName: link, ListenPort: server.WgPort, PrivateKey: server.WgPrivateKey, Address: server.WgAddress,
	}); err != nil {
		return err
	}
	if err := s.interfaceService.SetLinkMTU(link, s.mtu); err != nil {
		return err
	}
	// Keep the new interface unkeyed until isolation and routing are installed.
	if err := s.interfaceService.ConfigureRouting(link, server.WgAddress); err != nil {
		return err
	}
	if err := s.interfaceService.SetLinkState(link, server.Enabled); err != nil {
		return err
	}
	return s.wireguardService.ApplyInterface(link, userUID, server.WgPort)
}

func (s *UserNetworkService) DestroyUserNetwork(server *models.WireguardServer, userUID string) error {
	if server.NetworkMode != NetworkModeMultiInterface {
		// A failed migration may have already created the target host interface.
		link := TenantInterface(subnetIDFromAddress(server.WgAddress))
		if s.interfaceService.LinkExists(link) {
			if err := s.verifyOwnership(link, userUID); err != nil {
				return err
			}
			if err := s.interfaceService.Destroy(link, server.WgAddress); err != nil {
				return err
			}
		}
		if err := s.removeLegacyNamespace(server, userUID); err != nil {
			return err
		}
		s.removeLegacyForwarding(server, userUID)
		return nil
	}
	if s.interfaceService.LinkExists(server.WgInterface) {
		if err := s.verifyOwnership(server.WgInterface, userUID); err != nil {
			return err
		}
	}
	return s.interfaceService.Destroy(server.WgInterface, server.WgAddress)
}

// Legacy namespaces are accessed only during migration/cleanup. Containers need
// the one-time legacy migration compose override to see the old mount points.
func (s *UserNetworkService) removeLegacyNamespace(server *models.WireguardServer, userUID string) error {
	name := "wg_" + userUID
	if server.Namespace != "" && server.Namespace != name {
		return fmt.Errorf("unexpected legacy namespace %q", server.Namespace)
	}
	path := filepath.Join("/var/run/netns", name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if HostUDPPortInUse(server.WgPort) {
			return fmt.Errorf("legacy UDP port %d is still in use; use docker-compose.legacy-migration.yml for the first upgrade", server.WgPort)
		}
		return nil
	} else if err != nil {
		return err
	}
	// Explicitly delete the interface before unmounting: another open namespace
	// handle must not keep its WireGuard socket alive and occupy the old port.
	if out, err := exec.Command("ip", "-n", name, "link", "del", "dev", server.WgInterface).CombinedOutput(); err != nil && !strings.Contains(string(out), "Cannot find device") {
		return fmt.Errorf("remove legacy interface: %w: %s", err, out)
	}
	if out, err := exec.Command("ip", "netns", "delete", name).CombinedOutput(); err != nil {
		return fmt.Errorf("remove legacy namespace: %w: %s", err, out)
	}
	return nil
}

// removeLegacyForwarding 回收旧版方案留在宿主命名空间的全部残留。
//
// 需要清理三类：
//   - 端口映射规则：旧方案把命名空间内的 wg 端口经 DNAT 映射到物理网卡，回程靠
//     conntrack 反向改写。那既是端口漂移的根源，也会劫持新方案中本应直接投递给
//     加密 socket 的报文。
//   - 出口 NAT 规则：veth 子网的 MASQUERADE 与 FORWARD。
//   - 游离的宿主侧网卡：旧方案在「veth 对已创建、但移入命名空间失败」时会把两端
//     都留在宿主命名空间；更早版本也可能留下未移入的临时接口。
//     命名空间侧的网卡会随命名空间一并销毁，宿主侧的必须单独回收。
func (s *UserNetworkService) removeLegacyForwarding(server *models.WireguardServer, userUID string) {
	// 按账号名精确推导网卡名，不做通配扫描——通配会误删并发编排中正在创建的接口。
	s.interfaceService.DeleteLinkInHost(tempLinkName(userUID))
	s.interfaceService.DeleteLinkInHost(legacyVethHostName(userUID))

	if server.WgPort <= 0 {
		return
	}

	subnetID := subnetIDFromAddress(server.WgAddress)
	if subnetID == 0 {
		return
	}

	legacyNSIP := fmt.Sprintf("%s.%d.2", s.baseSubnet, subnetID)
	s.interfaceService.RemoveLegacyPortForwarding(s.outInterface, server.WgPort, legacyNSIP, "udp")
	s.interfaceService.RemoveHostNAT(fmt.Sprintf("%s.%d.0/30", s.baseSubnet, subnetID), s.outInterface)
}

// allocateNetwork 顺序分配监听端口与隧道网段号。
//
// 取代了早先的哈希分配：哈希在账号数增长后必然撞车（网段只有 254 个取值），
// 且冲突后要等到接口 up 失败才暴露。这里以数据库记录 + 宿主机实际端口占用为
// 依据顺序取用，分配结果天然唯一。
func (s *UserNetworkService) allocateNetwork(existing []NetworkAllocation) (int, int, error) {
	usedPorts := make(map[int]bool, len(existing))
	usedSubnets := make(map[int]bool, len(existing))
	for _, item := range existing {
		if item.WgPort > 0 {
			usedPorts[item.WgPort] = true
		}
		if item.SubnetID > 0 {
			usedSubnets[item.SubnetID] = true
		}
	}

	port := 0
	for candidate := s.basePort; candidate <= 65535; candidate++ {
		if usedPorts[candidate] || HostUDPPortInUse(candidate) {
			continue
		}
		port = candidate
		break
	}
	if port == 0 {
		return 0, 0, ErrNoAvailablePort
	}

	subnetID := 0
	for candidate := 1; candidate <= maxSubnetID; candidate++ {
		if !usedSubnets[candidate] && !s.interfaceService.LinkExists(TenantInterface(candidate)) {
			subnetID = candidate
			break
		}
	}
	if subnetID == 0 {
		return 0, 0, ErrNoAvailableSubnet
	}

	return port, subnetID, nil
}

// tunnelAddress 返回网段号对应的服务端隧道地址。
func tunnelAddress(subnetID int) string {
	return fmt.Sprintf("10.100.%d.1/24", subnetID)
}

// subnetIDFromAddress 从服务端隧道地址解析网段号（10.100.<id>.1/24 → id）。
func subnetIDFromAddress(address string) int {
	raw := strings.SplitN(strings.TrimSpace(address), "/", 2)[0]
	ip, err := netip.ParseAddr(raw)
	if err != nil || !ip.Is4() {
		return 0
	}
	octets := ip.As4()
	if octets[0] != 10 || octets[1] != 100 {
		return 0
	}
	id := int(octets[2])
	if id < 1 || id > maxSubnetID {
		return 0
	}
	return id
}

// tempLinkName 仅用于回收旧版本的临时网卡。
func tempLinkName(userUID string) string {
	return "wgx-" + shortUID(userUID, 8)
}

// legacyVethHostName 旧版方案在宿主命名空间一侧的 veth 网卡名。
func legacyVethHostName(userUID string) string {
	return "veth-h-" + shortUID(userUID, 6)
}

// shortUID 截取账号标识的前 n 个字符；接口名受 IFNAMSIZ（15 字符）限制。
func shortUID(userUID string, n int) string {
	if len(userUID) > n {
		return userUID[:n]
	}
	return userUID
}
