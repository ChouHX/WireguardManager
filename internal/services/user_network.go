package services

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/models"
	"errors"
	"fmt"
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

// UserNetworkService 账号网络编排服务。
//
// 每个账号得到一个独占的网络命名空间，其中只有隧道接口与 lo：
//
//	[宿主 Default NS]  eth0 + 加密 UDP socket（接口创建时即固定在此）
//	[netns wg_<uid>]   wg0（明文落地点） + lo，ip_forward=1
//
// 加密报文由宿主命名空间的 socket 直接收发，明文流量留在命名空间内。
// 因此全程没有 veth 中转、没有 DNAT、没有 conntrack 回转。
type UserNetworkService struct {
	netnsService     *NetnsService
	wireguardService *WireguardService
	baseSubnet       string // 旧版 veth 网段前缀，仅用于回收历史规则
	basePort         int    // WireGuard 监听端口起始值
	outInterface     string // 出口网卡，仅用于回收历史规则
}

// NewUserNetworkServiceFromRuntime 用运行时配置构造（config.yaml 仅作初始默认值）。
func NewUserNetworkServiceFromRuntime() *UserNetworkService {
	cfg := config.AppConfig
	settings := GetSettings()

	baseSubnet := settings.String(SettingNetworkBaseSubnet, cfg.Network.BaseSubnet)
	basePort := settings.Int(SettingNetworkBasePort, cfg.Network.BasePort)
	outInterface := settings.String(SettingNetworkOutInterface, cfg.Network.OutInterface)

	return NewUserNetworkService(cfg.Network.ConfigDir, baseSubnet, basePort, outInterface)
}

func NewUserNetworkService(configDir, baseSubnet string, basePort int, outInterface string) *UserNetworkService {
	return &UserNetworkService{
		netnsService:     NewNetnsService(),
		wireguardService: NewWireguardService(configDir),
		baseSubnet:       baseSubnet,
		basePort:         basePort,
		outInterface:     outInterface,
	}
}

// ProvisionUserNetwork 为新账号编排网络环境。
//
// existing 由调用方从数据库读取，用于避让已被占用的端口与网段；服务层不直接
// 访问数据库，避免与 database 包形成循环依赖。
// 返回 WireguardServer 对象，调用方负责保存到数据库。
func (s *UserNetworkService) ProvisionUserNetwork(user *models.User, existing []NetworkAllocation) (*models.WireguardServer, error) {
	nsName := fmt.Sprintf("wg_%s", user.UserUID)
	wgInterface := "wg0"

	port, subnetID, err := s.allocateNetwork(existing)
	if err != nil {
		return nil, err
	}

	privateKey, publicKey, err := s.wireguardService.GenerateKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate wireguard keys: %v", err)
	}

	wgIP := tunnelAddress(subnetID)

	server := &models.WireguardServer{
		UserID:       user.ID,
		Namespace:    nsName,
		WgInterface:  wgInterface,
		WgPort:       port,
		WgPublicKey:  publicKey,
		WgPrivateKey: privateKey,
		WgAddress:    wgIP,
	}

	if err := s.createNetwork(nsName, wgInterface, user.UserUID, privateKey, wgIP, port); err != nil {
		return nil, err
	}

	return server, nil
}

// EnsureUserNetwork 让既有账号的网络回到目标状态：沿用数据库中已记录的端口与
// 隧道地址重建命名空间与接口，下发给客户端的任何参数都不改变。
//
// 两类场景会用到：升级到「原生跨命名空间」方案后的迁移；运行期发现命名空间或
// 接口缺失时的自愈。
func (s *UserNetworkService) EnsureUserNetwork(server *models.WireguardServer, userUID string) error {
	nsName := server.Namespace
	if nsName == "" {
		nsName = fmt.Sprintf("wg_%s", userUID)
	}
	wgInterface := server.WgInterface
	if wgInterface == "" {
		wgInterface = "wg0"
	}
	if server.WgPrivateKey == "" || server.WgPort <= 0 || server.WgAddress == "" {
		return fmt.Errorf("server %d is missing the parameters required to rebuild its network", server.ID)
	}

	// 旧版方案在宿主机上留了端口映射，会劫持本该直接投递给加密 socket 的报文，
	// 必须先清掉再重建。
	s.removeLegacyForwarding(server, userUID)

	// 删除旧命名空间：其中的旧接口、旧 veth 与命名空间内规则随之回收，
	// 接口销毁时内核会同步释放宿主命名空间里的加密 socket。
	if err := s.netnsService.DeleteNamespace(nsName); err != nil {
		return err
	}

	return s.createNetwork(nsName, wgInterface, userUID, server.WgPrivateKey, server.WgAddress, server.WgPort)
}

// createNetwork 按「原生跨命名空间」方案构建一个账号的网络环境。
//
// 关键顺序：
//  1. 在宿主机创建接口 —— 内核把 creating_net 记为宿主命名空间；
//  2. 移入账号命名空间并改名为 wg0；
//  3. 下发私钥与监听端口；
//  4. 配置地址并拉起接口 —— 监听 socket 在此刻于宿主命名空间建立。
func (s *UserNetworkService) createNetwork(nsName, wgInterface, userUID, privateKey, wgIP string, port int) error {
	tempLink := tempLinkName(userUID)

	rollback := func() {
		// 临时接口可能仍留在宿主命名空间（尚未成功移入时）
		s.netnsService.DeleteLinkInHost(tempLink)
		s.netnsService.DeleteNamespace(nsName)
	}

	if err := s.netnsService.CreateNamespace(nsName); err != nil {
		return err
	}

	if _, err := s.wireguardService.CreateConfig(userUID, &WireguardConfig{
		InterfaceName: wgInterface,
		ListenPort:    port,
		PrivateKey:    privateKey,
		Address:       wgIP,
	}); err != nil {
		rollback()
		return err
	}

	if err := s.netnsService.CreateWireguardDevice(tempLink); err != nil {
		rollback()
		return err
	}

	if err := s.netnsService.MoveLinkToNamespace(tempLink, nsName); err != nil {
		rollback()
		return err
	}

	if err := s.netnsService.RenameLinkInNamespace(nsName, tempLink, wgInterface); err != nil {
		rollback()
		return err
	}

	if err := s.wireguardService.ApplyInterfaceInNamespace(nsName, wgInterface, userUID, port); err != nil {
		rollback()
		return err
	}

	if err := s.netnsService.AddAddressInNamespace(nsName, wgInterface, wgIP); err != nil {
		rollback()
		return err
	}

	if err := s.netnsService.SetLinkUpInNamespace(nsName, wgInterface); err != nil {
		rollback()
		return err
	}

	// peer 互访与「访问某个 peer 背后的下挂网段」都要在命名空间内完成一次转发
	if err := s.netnsService.EnableForwardingInNamespace(nsName); err != nil {
		rollback()
		return err
	}

	return nil
}

// DestroyUserNetwork 回收账号的网络环境。
//
// 删除命名空间即完成主体回收：接口、路由与命名空间内的规则都在其中。接口销毁
// 时内核会释放宿主命名空间里的加密 socket，因此无需再逐项摘除。
func (s *UserNetworkService) DestroyUserNetwork(server *models.WireguardServer, userUID string) error {
	if server.Namespace == "" {
		return nil
	}

	s.removeLegacyForwarding(server, userUID)

	if err := s.netnsService.DeleteNamespace(server.Namespace); err != nil {
		return fmt.Errorf("failed to delete namespace: %v", err)
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
//     都留在宿主命名空间；本方案在「临时接口已创建、但尚未移入」时同样可能留下。
//     命名空间侧的网卡会随命名空间一并销毁，宿主侧的必须单独回收。
func (s *UserNetworkService) removeLegacyForwarding(server *models.WireguardServer, userUID string) {
	// 按账号名精确推导网卡名，不做通配扫描——通配会误删并发编排中正在创建的接口。
	s.netnsService.DeleteLinkInHost(tempLinkName(userUID))
	s.netnsService.DeleteLinkInHost(legacyVethHostName(userUID))

	if server.WgPort <= 0 {
		return
	}

	subnetID := subnetIDFromAddress(server.WgAddress)
	if subnetID == 0 {
		return
	}

	legacyNSIP := fmt.Sprintf("%s.%d.2", s.baseSubnet, subnetID)
	s.netnsService.RemoveLegacyPortForwarding(s.outInterface, server.WgPort, legacyNSIP, "udp")
	s.netnsService.RemoveHostNAT(fmt.Sprintf("%s.%d.0/30", s.baseSubnet, subnetID), s.outInterface)
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
		if !usedSubnets[candidate] {
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
	parts := strings.SplitN(strings.TrimSpace(address), "/", 2)
	octets := strings.Split(parts[0], ".")
	if len(octets) != 4 {
		return 0
	}

	var id int
	if _, err := fmt.Sscanf(octets[2], "%d", &id); err != nil {
		return 0
	}
	if id < 1 || id > maxSubnetID {
		return 0
	}
	return id
}

// tempLinkName 生成只在宿主命名空间短暂存在的临时接口名。
//
// 所有账号的隧道接口最终都叫 wg0。若直接在宿主命名空间以 wg0 创建，并发编排
// 时必然撞名；先以唯一名创建、移入命名空间后再改名，即可规避这一竞态
// （改名只触发 NETDEV_CHANGENAME，不影响接口归属的 creating_net）。
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
