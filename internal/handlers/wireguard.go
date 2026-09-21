package handlers

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// 流量统计缓存
// ---------------------------------------------------------------------------

const (
	// 同一 namespace/interface 的 `wg show dump` 结果在 1 秒内复用，
	// 覆盖用户页 3 秒轮询 + 并发请求造成的重复进程创建。
	trafficCacheTTL = time.Second

	// 缓存条目上限（约等于服务器数量），超限时清理过期条目
	trafficCacheMaxEntries = 1024

	// 管理端批量抓取的并发上限：既要并行，也要避免同时 spawn 过多子进程
	adminTrafficMaxConcurrency = 8
)

type trafficCacheEntry struct {
	mu        sync.Mutex
	stats     *models.WireguardServerStats
	expiresAt time.Time
}

var (
	trafficCacheMu sync.Mutex
	trafficCache   = make(map[string]*trafficCacheEntry)
)

// getTrafficStatsCached 返回指定命名空间/接口的 WireGuard 统计。
// 命中未过期缓存时直接返回；未命中时按 key 加锁抓取，使同一时间窗内的并发请求只执行一次 `wg show`。
// 返回值始终是副本，调用方可安全改写（例如填充 peer 的 Comment）而不污染缓存。
func getTrafficStatsCached(wgService *services.WireguardService, namespace, wgInterface string) (*models.WireguardServerStats, error) {
	key := namespace + "/" + wgInterface

	trafficCacheMu.Lock()
	entry, ok := trafficCache[key]
	if !ok {
		entry = &trafficCacheEntry{}
		trafficCache[key] = entry
	}
	if len(trafficCache) > trafficCacheMaxEntries {
		pruneTrafficCacheLocked(time.Now())
	}
	trafficCacheMu.Unlock()

	// 每个 key 独立加锁：不同用户的抓取互不阻塞，同一用户的并发请求只会真正执行一次
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.stats != nil && time.Now().Before(entry.expiresAt) {
		return cloneServerStats(entry.stats), nil
	}

	stats, err := wgService.GetDetailedStats(namespace, wgInterface)
	if err != nil {
		return nil, err
	}

	entry.stats = cloneServerStats(stats)
	entry.expiresAt = time.Now().Add(trafficCacheTTL)

	return stats, nil
}

// cloneServerStats 深拷贝统计结果（Peers 为值切片，按值复制即可）。
func cloneServerStats(stats *models.WireguardServerStats) *models.WireguardServerStats {
	if stats == nil {
		return nil
	}
	cp := *stats
	cp.Peers = append([]models.WireguardPeerStats(nil), stats.Peers...)
	return &cp
}

// pruneTrafficCacheLocked 清理从未成功填充或已过期的缓存条目。
func pruneTrafficCacheLocked(now time.Time) {
	for key, entry := range trafficCache {
		if entry.stats == nil || now.After(entry.expiresAt) {
			delete(trafficCache, key)
		}
	}
}

// GetMyTrafficStats 获取当前用户的流量统计（完整信息）
func GetMyTrafficStats(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	// 查询用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	// 创建WireGuard服务
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 获取流量统计（走缓存）
	stats, err := getTrafficStatsCached(wgService, wgServer.Namespace, wgServer.WgInterface)
	if err != nil {
		response.InternalError(c, "Failed to get traffic stats: "+err.Error())
		return
	}

	// 从数据库获取peer信息以添加备注
	var peers []models.WireguardPeer
	database.DB.Where("server_id = ?", wgServer.ID).Find(&peers)

	// 创建peer公钥到备注的映射
	peerComments := make(map[string]string)
	for _, peer := range peers {
		peerComments[peer.PublicKey] = peer.Comment
	}

	// 添加备注到统计信息
	for i := range stats.Peers {
		if comment, ok := peerComments[stats.Peers[i].PublicKey]; ok {
			stats.Peers[i].Comment = comment
		}
	}

	serverInfo := wgServer.ToResponse()
	trafficStats := &models.UserTrafficStats{
		UserID:      u.ID,
		UserUID:     u.UserUID,
		Email:       u.Email,
		ServerInfo:  &serverInfo,
		ServerStats: stats,
	}

	response.Success(c, "Traffic stats retrieved successfully", trafficStats)
}

// GetMyTrafficSummary 获取当前用户的流量摘要（用于轮询）
func GetMyTrafficSummary(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	// 查询用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	// 创建WireGuard服务
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 获取流量统计（走缓存，避免每次轮询都 spawn wg 进程）
	stats, err := getTrafficStatsCached(wgService, wgServer.Namespace, wgServer.WgInterface)
	if err != nil {
		response.InternalError(c, "Failed to get traffic stats: "+err.Error())
		return
	}

	// 从数据库获取peer信息以添加备注
	var peers []models.WireguardPeer
	database.DB.Where("server_id = ?", wgServer.ID).Find(&peers)

	// 创建peer公钥到备注的映射
	peerComments := make(map[string]string)
	for _, peer := range peers {
		peerComments[peer.PublicKey] = peer.Comment
	}

	// 构建流量摘要
	peerSummaries := make([]models.PeerTrafficSummary, 0, len(stats.Peers))
	for _, peer := range stats.Peers {
		peerSummary := models.PeerTrafficSummary{
			PublicKey:       peer.PublicKey,
			LatestHandshake: peer.LatestHandshake,
			TransferRx:      peer.TransferRx,
			TransferTx:      peer.TransferTx,
		}
		if comment, ok := peerComments[peer.PublicKey]; ok {
			peerSummary.Comment = comment
		}
		peerSummaries = append(peerSummaries, peerSummary)
	}

	summary := &models.UserTrafficSummary{
		PeerCount: stats.PeerCount,
		TotalRx:   stats.TotalRx,
		TotalTx:   stats.TotalTx,
		Peers:     peerSummaries,
	}

	response.Success(c, "Traffic summary retrieved successfully", summary)
}

// GetAdminTrafficStats 获取所有用户的流量统计（管理员，精简版）
func GetAdminTrafficStats(c *gin.Context) {
	var servers []models.WireguardServer
	// 固定按 id 升序取回，配合按下标回填即可得到稳定的 server_id 升序结果
	if err := database.DB.Preload("User").Order("id ASC").Find(&servers).Error; err != nil {
		response.InternalError(c, "Failed to fetch wireguard servers")
		return
	}

	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 并发抓取：信号量限制并发数，results 按下标回填保证顺序稳定
	results := make([]*models.AdminUserTraffic, len(servers))
	sem := make(chan struct{}, adminTrafficMaxConcurrency)
	var wg sync.WaitGroup

	for i := range servers {
		server := servers[i]

		wg.Add(1)
		go func(index int, server models.WireguardServer) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			stats, err := getTrafficStatsCached(wgService, server.Namespace, server.WgInterface)
			if err != nil {
				// 单个用户抓取失败只记录并跳过，不静默吞掉原因，也不影响其他用户
				log.Printf("Failed to collect traffic stats for server %d (user %d, %s/%s): %v",
					server.ID, server.UserID, server.Namespace, server.WgInterface, err)
				return
			}

			results[index] = &models.AdminUserTraffic{
				ServerID:     server.ID,
				UserID:       server.UserID,
				UserUID:      server.User.UserUID,
				Email:        server.User.Email,
				PeerCount:    stats.PeerCount,
				TotalRx:      stats.TotalRx,
				TotalTx:      stats.TotalTx,
				WgPort:       server.WgPort,
				WgAddress:    server.WgAddress,
				Namespace:    server.Namespace,
				Enabled:      server.Enabled,
				DownloadRate: server.DownloadRate,
				UploadRate:   server.UploadRate,
			}
		}(i, server)
	}

	wg.Wait()

	adminTraffic := make([]models.AdminUserTraffic, 0, len(servers))
	for _, item := range results {
		if item != nil {
			adminTraffic = append(adminTraffic, *item)
		}
	}

	response.Success(c, "Admin traffic stats retrieved successfully", adminTraffic)
}

// GetUserTrafficStats 获取指定用户的流量统计（管理员）
func GetUserTrafficStats(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid user ID", nil)
		return
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		response.NotFound(c, "User not found")
		return
	}

	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", user.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	stats, err := getTrafficStatsCached(wgService, wgServer.Namespace, wgServer.WgInterface)
	if err != nil {
		response.InternalError(c, "Failed to get traffic stats: "+err.Error())
		return
	}

	// 从数据库获取peer信息
	var peers []models.WireguardPeer
	database.DB.Where("server_id = ?", wgServer.ID).Find(&peers)

	peerComments := make(map[string]string)
	for _, peer := range peers {
		peerComments[peer.PublicKey] = peer.Comment
	}

	for i := range stats.Peers {
		if comment, ok := peerComments[stats.Peers[i].PublicKey]; ok {
			stats.Peers[i].Comment = comment
		}
	}

	serverInfo := wgServer.ToResponse()
	trafficStats := &models.UserTrafficStats{
		UserID:      user.ID,
		UserUID:     user.UserUID,
		Email:       user.Email,
		ServerInfo:  &serverInfo,
		ServerStats: stats,
	}

	response.Success(c, "User traffic stats retrieved successfully", trafficStats)
}

// GetMyPeers 获取当前用户的所有peers
func GetMyPeers(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	// 获取用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	var peers []models.WireguardPeer
	if err := database.DB.Where("server_id = ?", wgServer.ID).Find(&peers).Error; err != nil {
		response.InternalError(c, "Failed to fetch peers")
		return
	}

	// 转换为响应格式
	peerResponses := make([]models.WireguardPeerResponse, 0, len(peers))
	for _, peer := range peers {
		peerResponses = append(peerResponses, peer.ToResponse())
	}

	response.Success(c, "Peers retrieved successfully", peerResponses)
}

// AddPeerRequest 添加peer请求
type AddPeerRequest struct {
	AllowedIPs          string `json:"allowed_ips"` // peer可以访问的IP地址或网段，留空则默认为peer自己的IP
	PersistentKeepalive int    `json:"persistent_keepalive"`
	Comment             string `json:"comment"`
	EnableForwarding    bool   `json:"enable_forwarding"` // 是否启用转发（作为网关）
	ForwardInterface    string `json:"forward_interface"` // 转发接口名称（如 eth0）
	// UsePresharedKey 是否启用预共享密钥；留空则取运行时配置中的默认值
	UsePresharedKey *bool `json:"use_preshared_key"`
}

// AddPeer 添加新的peer
func AddPeer(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	var req AddPeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	// 获取用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)
	netnsService := services.NewNetnsService()

	// 1. 生成密钥、分配 IP 并写入数据库（唯一约束冲突时自动重试）
	peer, err := createPeerRecord(wgService, &wgServer, req)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	// 2. 依次应用 WireGuard 配置、路由、iptables；任一步失败会回滚已生效的外网配置
	if err := provisionPeerResources(wgService, netnsService, wgServer.Namespace, wgServer.WgInterface, peer); err != nil {
		// 回滚数据库记录（数据库层错误不向客户端暴露细节）
		if delErr := database.DB.Delete(&peer).Error; delErr != nil {
			log.Printf("Warning: failed to roll back peer record %d of server %d: %v", peer.ID, wgServer.ID, delErr)
		}
		response.InternalError(c, err.Error())
		return
	}

	response.Created(c, "Peer added successfully", peer.ToResponse())
}

// peerRecordCreateAttempts 创建 peer 记录的重试次数：覆盖 IP 唯一约束冲突等并发场景
const peerRecordCreateAttempts = 3

// createPeerRecord 生成密钥、分配 IP 并写入 peer 记录。
// 命中唯一约束冲突时会重新生成密钥并重新分配 IP，最多重试 peerRecordCreateAttempts 次。
// 返回的错误文案面向客户端，故保留原有 "Failed to xxx: cause" 形式。
// buildServerAllowedIPs 计算服务端该 peer 的 allowed-ips：
// 设备自身地址 + 它背后的网段（用于跨网段转发）。
//
// 全局代理（0.0.0.0/0、::/0）会被排除——那是客户端把流量送进隧道的行为，
// 若写进服务端 allowed-ips 会导致所有流量都被转发给该设备。
func buildServerAllowedIPs(peer *models.WireguardPeer) string {
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

// usePresharedKeyForNewPeer 决定新设备是否启用预共享密钥：
// 请求显式指定优先，否则取运行时配置中的默认值。
func usePresharedKeyForNewPeer(explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	if settings := services.GetSettings(); settings != nil {
		return settings.Bool(services.SettingPeerDefaultPSK, false)
	}
	return false
}

func createPeerRecord(wgService *services.WireguardService, wgServer *models.WireguardServer, req AddPeerRequest) (*models.WireguardPeer, error) {
	var lastErr error

	for attempt := 1; attempt <= peerRecordCreateAttempts; attempt++ {
		// 1. 自动生成peer的密钥对
		privateKey, publicKey, err := wgService.GenerateKeys()
		if err != nil {
			return nil, fmt.Errorf("Failed to generate peer keys: %w", err)
		}

		// 2. 自动分配peer IP地址（从服务器网段中分配）
		peerIP, err := allocatePeerIP(wgServer.ID, wgServer.WgAddress)
		if err != nil {
			return nil, fmt.Errorf("Failed to allocate peer IP: %w", err)
		}

		// 3. 如果用户未指定allowed_ips，默认使用peer自己的IP
		allowedIPs := req.AllowedIPs
		if allowedIPs == "" {
			allowedIPs = peerIP + "/32"
		}

		// 4. 预共享密钥：请求未显式指定时取运行时默认值
		presharedKey := ""
		if usePresharedKeyForNewPeer(req.UsePresharedKey) {
			presharedKey, err = wgService.GeneratePresharedKey()
			if err != nil {
				return nil, fmt.Errorf("Failed to generate preshared key: %w", err)
			}
		}

		// 5. 创建peer记录
		peer := models.WireguardPeer{
			ServerID:            wgServer.ID,
			PublicKey:           publicKey,
			PrivateKey:          privateKey,
			PresharedKey:        presharedKey,
			PeerAddress:         peerIP,
			AllowedIPs:          allowedIPs,
			PersistentKeepalive: req.PersistentKeepalive,
			Comment:             req.Comment,
			EnableForwarding:    req.EnableForwarding,
			ForwardInterface:    req.ForwardInterface,
		}

		if err := database.DB.Create(&peer).Error; err != nil {
			if !isUniqueViolation(err) {
				log.Printf("Failed to create peer record for server %d: %v", wgServer.ID, err)
				return nil, errors.New("Failed to create peer record")
			}

			lastErr = err
			log.Printf("Peer record for server %d hit a unique constraint conflict (attempt %d/%d, address %s), retrying: %v",
				wgServer.ID, attempt, peerRecordCreateAttempts, peerIP, err)
			continue
		}

		return &peer, nil
	}

	log.Printf("Failed to create peer record for server %d after %d attempts: %v", wgServer.ID, peerRecordCreateAttempts, lastErr)
	return nil, errors.New("Failed to create peer record")
}

// provisionPeerResources 依次应用 peer 的网络侧配置：WireGuard peer → 路由 → iptables。
// 任一步失败都会回滚已经生效的步骤，保持原子性；错误文案沿用原有 "Failed to ..." 形式。
func provisionPeerResources(wgService *services.WireguardService, netnsService *services.NetnsService, namespace, wgInterface string, peer *models.WireguardPeer) error {
	peerAllowedIPs := peer.PeerAddress + "/32"

	// 5. 添加到WireGuard配置。allowed-ips 需要包含该设备背后的网段，
	// 否则命名空间内的转发会失败：WireGuard 依据 allowed-ips 决定把包加密发给哪个
	// peer（cryptokey routing），只加 ip route 而不声明 allowed-ips 时包会被直接丢弃。
	// 声明之后内核会自动为这些网段生成指向本接口的路由，无需再手工添加。
	serverAllowedIPs := buildServerAllowedIPs(peer)
	if err := wgService.AddPeer(namespace, wgInterface, peer.PublicKey, serverAllowedIPs, ""); err != nil {
		return fmt.Errorf("Failed to add peer to WireGuard: %w", err)
	}

	// 5.1 若该设备启用了预共享密钥，紧接着下发
	if peer.PresharedKey != "" {
		if err := wgService.SetPeerPresharedKey(namespace, wgInterface, peer.PublicKey, peer.PresharedKey); err != nil {
			wgService.RemovePeer(namespace, wgInterface, peer.PublicKey)
			return fmt.Errorf("Failed to apply preshared key: %w", err)
		}
	}

	// 6. 路由：allowed-ips 声明后内核通常已自动生成路由；这里仅在缺失时补一条，
	// 作为兜底（重复添加会被判定 File exists 并忽略）。
	// 全局代理（0.0.0.0/0）不参与服务端路由，它只是客户端的行为。
	needExtraRouting := peer.AllowedIPs != peerAllowedIPs && peer.AllowedIPs != "0.0.0.0/0"
	if !needExtraRouting {
		return nil
	}

	if err := netnsService.AddRouteForPeer(namespace, wgInterface, peer.AllowedIPs); err != nil {
		wgService.RemovePeer(namespace, wgInterface, peer.PublicKey)
		return fmt.Errorf("Failed to add route for peer: %w", err)
	}

	// 7. 同步iptables规则：允许转发peer网段的流量
	if err := netnsService.AddIptablesRuleForPeer(namespace, peer.AllowedIPs); err != nil {
		netnsService.DeleteRouteForPeer(namespace, wgInterface, peer.AllowedIPs)
		wgService.RemovePeer(namespace, wgInterface, peer.PublicKey)
		return fmt.Errorf("Failed to add iptables rule for peer: %w", err)
	}

	return nil
}

// peerIPAllocMu 保护"查询已用 IP → 选择空位"的临界区：
// 多个并发的 AddPeer 若不串行化，会读到同一份已用 IP 列表并分配出同一个地址。
var peerIPAllocMu sync.Mutex

// allocatePeerIP 为peer分配IP地址
func allocatePeerIP(serverID uint, serverAddress string) (string, error) {
	// 解析服务器地址（如 10.100.1.1/24）
	parts := strings.Split(serverAddress, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid server address format")
	}

	ipParts := strings.Split(parts[0], ".")
	if len(ipParts) != 4 {
		return "", fmt.Errorf("invalid IP address format")
	}

	peerIPAllocMu.Lock()
	defer peerIPAllocMu.Unlock()

	// 获取该服务器已分配的所有peer IP
	var peers []models.WireguardPeer
	if err := database.DB.Where("server_id = ?", serverID).Find(&peers).Error; err != nil {
		return "", err
	}

	// 构建已使用的IP集合
	usedIPs := make(map[string]bool)
	usedIPs[parts[0]] = true // 服务器IP本身
	for _, peer := range peers {
		usedIPs[peer.PeerAddress] = true
	}

	// 从 .2 开始分配（.1 是服务器）
	baseIP := fmt.Sprintf("%s.%s.%s", ipParts[0], ipParts[1], ipParts[2])
	for i := 2; i <= 254; i++ {
		candidateIP := fmt.Sprintf("%s.%d", baseIP, i)
		if !usedIPs[candidateIP] {
			return candidateIP, nil
		}
	}

	return "", fmt.Errorf("no available IP addresses in the subnet")
}

// isUniqueViolation 判断数据库错误是否是唯一约束冲突（Postgres 错误码 23505）。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "sqlstate 23505")
}

// DeletePeer 删除peer
func DeletePeer(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	peerIDStr := c.Param("id")
	peerID, err := strconv.ParseUint(peerIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid peer ID", nil)
		return
	}

	// 获取用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	var peer models.WireguardPeer
	if err := database.DB.First(&peer, peerID).Error; err != nil {
		response.NotFound(c, "Peer not found")
		return
	}

	// 确保peer属于当前用户的服务器
	if peer.ServerID != wgServer.ID {
		response.Forbidden(c, "You don't have permission to delete this peer")
		return
	}

	// 清理iptables规则
	netnsService := services.NewNetnsService()
	netnsService.DeleteIptablesRuleForPeer(wgServer.Namespace, peer.AllowedIPs)

	// 清理路由规则
	netnsService.DeleteRouteForPeer(wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)

	// 从WireGuard配置中删除
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)
	if err := wgService.RemovePeer(wgServer.Namespace, wgServer.WgInterface, peer.PublicKey); err != nil {
		response.InternalError(c, "Failed to remove peer from WireGuard: "+err.Error())
		return
	}

	// 从数据库删除
	if err := database.DB.Delete(&peer).Error; err != nil {
		response.InternalError(c, "Failed to delete peer record")
		return
	}

	response.Success(c, "Peer deleted successfully", nil)
}

// UpdatePeerRequest 更新peer请求
type UpdatePeerRequest struct {
	AllowedIPs          string `json:"allowed_ips"`
	PersistentKeepalive *int   `json:"persistent_keepalive"`
	Comment             string `json:"comment"`
	EnableForwarding    *bool  `json:"enable_forwarding"`
	ForwardInterface    string `json:"forward_interface"`
	// UsePresharedKey 切换预共享密钥（启用时自动生成并下发，关闭时重新建立已无密钥的 peer）
	UsePresharedKey *bool `json:"use_preshared_key"`
}

// UpdatePeer 更新peer信息
func UpdatePeer(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	peerIDStr := c.Param("id")
	peerID, err := strconv.ParseUint(peerIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid peer ID", nil)
		return
	}

	var req UpdatePeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	// 获取用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	var peer models.WireguardPeer
	if err := database.DB.First(&peer, peerID).Error; err != nil {
		response.NotFound(c, "Peer not found")
		return
	}

	// 确保peer属于当前用户的服务器
	if peer.ServerID != wgServer.ID {
		response.Forbidden(c, "You don't have permission to update this peer")
		return
	}

	updates := make(map[string]interface{})

	needWgUpdate := false
	if req.AllowedIPs != "" && req.AllowedIPs != peer.AllowedIPs {
		updates["allowed_ips"] = req.AllowedIPs
		needWgUpdate = true
	}

	if req.PersistentKeepalive != nil {
		updates["persistent_keepalive"] = *req.PersistentKeepalive
	}

	if req.Comment != "" {
		updates["comment"] = req.Comment
	}

	if req.EnableForwarding != nil {
		updates["enable_forwarding"] = *req.EnableForwarding
	}

	if req.ForwardInterface != "" {
		updates["forward_interface"] = req.ForwardInterface
	}

	// 预共享密钥切换：开启时生成并下发；关闭时重建 peer（wg 无法就地清空 PSK）
	if req.UsePresharedKey != nil {
		wantPSK := *req.UsePresharedKey
		hasPSK := peer.PresharedKey != ""
		wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)
		peerAllowedIPs := peer.PeerAddress + "/32"

		switch {
		case wantPSK && !hasPSK:
			generated, err := wgService.GeneratePresharedKey()
			if err != nil {
				response.InternalError(c, "Failed to generate preshared key: "+err.Error())
				return
			}
			if err := wgService.SetPeerPresharedKey(wgServer.Namespace, wgServer.WgInterface, peer.PublicKey, generated); err != nil {
				response.InternalError(c, "Failed to apply preshared key: "+err.Error())
				return
			}
			updates["preshared_key"] = generated

		case !wantPSK && hasPSK:
			if err := wgService.RemovePeer(wgServer.Namespace, wgServer.WgInterface, peer.PublicKey); err != nil {
				response.InternalError(c, "Failed to reset peer before disabling preshared key: "+err.Error())
				return
			}
			if err := wgService.AddPeer(wgServer.Namespace, wgServer.WgInterface, peer.PublicKey, peerAllowedIPs, ""); err != nil {
				response.InternalError(c, "Failed to re-add peer without preshared key: "+err.Error())
				return
			}
			updates["preshared_key"] = ""
		}
	}

	if len(updates) == 0 {
		response.BadRequest(c, "No valid fields to update", nil)
		return
	}

	// 如果需要更新WireGuard配置（AllowedIPs变化）
	// 注意：AllowedIPs 是 peer 可以访问的网段，不影响 WireGuard 配置中的 allowed-ips
	// WireGuard 配置中的 allowed-ips 始终是 peer 的 IP 地址
	if needWgUpdate {
		netnsService := services.NewNetnsService()
		wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

		// 1. 先同步服务端 allowed-ips（cryptokey routing 的依据），
		//    否则改了网段也转发不到该设备——只调整路由是无效的。
		updatedPeer := peer
		updatedPeer.AllowedIPs = req.AllowedIPs
		if err := wgService.SetPeerAllowedIPs(
			wgServer.Namespace, wgServer.WgInterface, peer.PublicKey, buildServerAllowedIPs(&updatedPeer),
		); err != nil {
			response.InternalError(c, "Failed to update peer allowed-ips: "+err.Error())
			return
		}

		// 2. 清理旧的路由和iptables规则
		clearPeerRoutes(netnsService, wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)

		// 3. 添加新的路由和iptables规则
		if req.AllowedIPs != "" && req.AllowedIPs != "0.0.0.0/0" {
			if err := netnsService.AddRouteForPeer(wgServer.Namespace, wgServer.WgInterface, req.AllowedIPs); err != nil {
				// 尝试恢复旧规则
				restorePeerRoutes(netnsService, wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)
				response.InternalError(c, "Failed to add route for peer: "+err.Error())
				return
			}

			if err := netnsService.AddIptablesRuleForPeer(wgServer.Namespace, req.AllowedIPs); err != nil {
				// 回滚新规则并恢复旧规则
				netnsService.DeleteRouteForPeer(wgServer.Namespace, wgServer.WgInterface, req.AllowedIPs)
				restorePeerRoutes(netnsService, wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)
				response.InternalError(c, "Failed to add iptables rule for peer: "+err.Error())
				return
			}
		}
	}

	// 更新数据库
	if err := database.DB.Model(&peer).Updates(updates).Error; err != nil {
		response.InternalError(c, "Failed to update peer")
		return
	}

	// 重新加载peer
	database.DB.First(&peer, peer.ID)

	response.Success(c, "Peer updated successfully", peer.ToResponse())
}

// clearPeerRoutes 删除 peer 现有的路由与 iptables 规则（尽力而为，错误由 netns 层忽略）。
func clearPeerRoutes(netnsService *services.NetnsService, namespace, wgInterface, allowedIPs string) {
	if allowedIPs == "" || allowedIPs == "0.0.0.0/0" {
		return
	}

	netnsService.DeleteIptablesRuleForPeer(namespace, allowedIPs)
	netnsService.DeleteRouteForPeer(namespace, wgInterface, allowedIPs)
}

// restorePeerRoutes 尝试恢复 peer 旧的路由与 iptables 规则，用于新规则写入失败时的回滚。
func restorePeerRoutes(netnsService *services.NetnsService, namespace, wgInterface, allowedIPs string) {
	if allowedIPs == "" || allowedIPs == "0.0.0.0/0" {
		return
	}

	if err := netnsService.AddRouteForPeer(namespace, wgInterface, allowedIPs); err != nil {
		log.Printf("Warning: failed to restore route %s in %s/%s: %v", allowedIPs, namespace, wgInterface, err)
	}
	if err := netnsService.AddIptablesRuleForPeer(namespace, allowedIPs); err != nil {
		log.Printf("Warning: failed to restore iptables rule %s in %s: %v", allowedIPs, namespace, err)
	}
}

// livenessMonitor 由 main 注入；为 nil 表示未启用存活探测。
var livenessMonitor *services.LivenessMonitor

// SetLivenessMonitor 注入存活探测监控器。
func SetLivenessMonitor(monitor *services.LivenessMonitor) {
	livenessMonitor = monitor
}

// GetLiveness 返回当前用户设备的实时在线状态。
//
// 探测在服务端后台进行（TCP SYN/RST），客户端无需安装任何 Agent；
// 这里只读取内存中的结论快照，因此响应是零网络开销的。
func GetLiveness(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	if !livenessEnabled() {
		response.Success(c, "Liveness probing is disabled", gin.H{
			"enabled":                   false,
			"online":                    0,
			"total":                     0,
			"handshake_timeout_seconds": config.AppConfig.Liveness.HandshakeTimeoutSeconds,
			"peers":                     gin.H{},
		})
		return
	}

	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	var peers []models.WireguardPeer
	if err := database.DB.Select("public_key").
		Where("server_id = ?", wgServer.ID).Find(&peers).Error; err != nil {
		response.InternalError(c, "Failed to fetch peers")
		return
	}

	snapshot := livenessMonitor.Snapshot()
	peerResults := make(map[string]services.LivenessResult, len(peers))
	online := 0

	for _, peer := range peers {
		if result, found := snapshot[peer.PublicKey]; found {
			peerResults[peer.PublicKey] = result
			if result.State == services.LivenessOnline {
				online++
			}
			continue
		}
		// 尚未产生探测结论（刚创建或探测刚启动）
		peerResults[peer.PublicKey] = services.LivenessResult{State: services.LivenessUnknown}
	}

	response.Success(c, "Liveness retrieved successfully", gin.H{
		"enabled":                   true,
		"online":                    online,
		"total":                     len(peers),
		"handshake_timeout_seconds": config.AppConfig.Liveness.HandshakeTimeoutSeconds,
		"peers":                     peerResults,
	})
}

// GetAdminLiveness 返回全部设备的在线状态，并按服务器聚合，供管理端列表使用。
func GetAdminLiveness(c *gin.Context) {
	if !livenessEnabled() {
		response.Success(c, "Liveness probing is disabled", gin.H{
			"enabled": false,
			"summary": gin.H{"online": 0, "offline": 0, "unknown": 0},
			"servers": gin.H{},
		})
		return
	}

	type peerRow struct {
		ServerID  uint
		PublicKey string
	}

	var peers []peerRow
	if err := database.DB.Model(&models.WireguardPeer{}).
		Select("server_id", "public_key").Find(&peers).Error; err != nil {
		response.InternalError(c, "Failed to fetch peers")
		return
	}

	type serverAggregate struct {
		Online  int `json:"online"`
		Offline int `json:"offline"`
		Unknown int `json:"unknown"`
		Total   int `json:"total"`
	}

	snapshot := livenessMonitor.Snapshot()
	aggregates := make(map[uint]*serverAggregate)

	for _, peer := range peers {
		aggregate, found := aggregates[peer.ServerID]
		if !found {
			aggregate = &serverAggregate{}
			aggregates[peer.ServerID] = aggregate
		}
		aggregate.Total++

		switch result, ok := snapshot[peer.PublicKey]; {
		case !ok:
			aggregate.Unknown++
		case result.State == services.LivenessOnline:
			aggregate.Online++
		case result.State == services.LivenessOffline:
			aggregate.Offline++
		default:
			aggregate.Unknown++
		}
	}

	// 以字符串为键，避免 JSON 序列化 map[uint] 的限制
	servers := make(map[string]serverAggregate, len(aggregates))
	for id, aggregate := range aggregates {
		servers[strconv.FormatUint(uint64(id), 10)] = *aggregate
	}

	online, offline, unknown := livenessMonitor.Counts()

	response.Success(c, "Admin liveness retrieved successfully", gin.H{
		"enabled": true,
		"summary": gin.H{"online": online, "offline": offline, "unknown": unknown},
		"servers": servers,
	})
}

func livenessEnabled() bool {
	return livenessMonitor != nil && livenessMonitor.ProbeEnabled()
}

// GetNetworkInterfaces 返回可作为转发出口的网络接口列表。
// 默认值优先取系统默认路由的出口接口，探测不到时回退到配置的 network.out_interface。
func GetNetworkInterfaces(c *gin.Context) {
	if _, ok := currentUser(c); !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	interfaces, detected := services.DetectNetworkInterfaces()

	defaultIface := detected
	if defaultIface == "" {
		defaultIface = strings.TrimSpace(config.AppConfig.Network.OutInterface)
	}

	response.Success(c, "Network interfaces retrieved successfully", gin.H{
		"default":    defaultIface,
		"detected":   detected != "",
		"interfaces": interfaces,
	})
}

// clientAllowedIPs 计算下发给客户端的 AllowedIPs（客户端把哪些流量送进隧道）。
//
// 取值优先级：
//  1. network.client_allowed_ips 显式配置（需要全局代理时写 "0.0.0.0/0, ::/0"）；
//  2. 按 peer 所在网段推导：以 peer 地址配合服务端接口掩码求网络地址，
//     例如服务端 10.100.0.1/24、该 peer 分配到 10.100.0.2 → 10.100.0.0/24；
//  3. 推导失败时回退为 peer 自身地址（/32 或 /128），始终避免下发全流量。
func clientAllowedIPs(serverAddress, peerAddress string) string {
	configured := strings.TrimSpace(
		services.GetSettings().String(services.SettingNetworkClientAllowedIPs, config.AppConfig.Network.ClientAllowedIPs),
	)
	if configured != "" {
		return configured
	}

	if derived, ok := deriveNetworkCIDR(serverAddress, peerAddress); ok {
		return derived
	}

	if peer, err := netip.ParseAddr(strings.TrimSpace(peerAddress)); err == nil {
		peer = peer.Unmap()
		if peer.Is4() {
			return netip.PrefixFrom(peer, 32).String()
		}
		return netip.PrefixFrom(peer, 128).String()
	}

	return ""
}

// deriveNetworkCIDR 推导 peer 所属网段的 CIDR。
// 掩码优先取服务端接口地址（如 10.100.0.1/24 → 24），仅在地址族一致时使用，
// 否则按主机位长处理，保证 IPv6 peer 不会被套上 IPv4 掩码。
func deriveNetworkCIDR(serverAddress, peerAddress string) (string, bool) {
	peer, err := netip.ParseAddr(strings.TrimSpace(peerAddress))
	if err != nil {
		return "", false
	}
	peer = peer.Unmap()

	bits := 32
	if peer.Is6() {
		bits = 128
	}

	if prefix, err := netip.ParsePrefix(strings.TrimSpace(serverAddress)); err == nil {
		if serverIP := prefix.Addr().Unmap(); serverIP.Is4() == peer.Is4() {
			bits = prefix.Bits()
		}
	}

	return netip.PrefixFrom(peer, bits).Masked().String(), true
}

// GetPeerConfig 获取peer的WireGuard配置（统一接口，返回JSON格式）
func GetPeerConfig(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	peerIDStr := c.Param("id")
	peerID, err := strconv.ParseUint(peerIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid peer ID", nil)
		return
	}

	// 获取用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	// 获取peer信息
	var peer models.WireguardPeer
	if err := database.DB.First(&peer, peerID).Error; err != nil {
		response.NotFound(c, "Peer not found")
		return
	}

	// 确保peer属于当前用户的服务器
	if peer.ServerID != wgServer.ID {
		response.Forbidden(c, "You don't have permission to access this peer")
		return
	}

	// 客户端 DNS 与服务器地址取自运行时设置（config.yaml 仅作为初始默认值）
	runtimeSettings := services.GetSettings()
	dns := strings.TrimSpace(runtimeSettings.String(services.SettingNetworkDNS, config.AppConfig.Network.DNS))
	if dns == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}

	// 生成服务器端点地址：优先使用服务器记录的 endpoint，缺失时回退到运行时设置的 ServerIP:端口
	serverEndpoint := strings.TrimSpace(wgServer.ServerEndpoint)
	if serverEndpoint == "" {
		serverIP := runtimeSettings.String(services.SettingNetworkServerIP, config.AppConfig.Network.ServerIP)
		serverEndpoint = fmt.Sprintf("%s:%d", serverIP, wgServer.WgPort)
	}

	// 客户端 AllowedIPs：优先取 network.client_allowed_ips 配置，
	// 未配置时按 peer 所在网段推导（不再默认放行全部流量）
	allowedIPs := clientAllowedIPs(wgServer.WgAddress, peer.PeerAddress)

	// 基础配置内容
	configContent := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = %s
`,
		peer.PrivateKey,
		peer.PeerAddress+"/32",
		dns,
	)

	// 启用转发时注入客户端侧的 NAT 规则。
	// 这段脚本在【客户端设备】上执行，因此网卡指的是该设备自己的物理网卡；
	// 未指定时使用取反匹配 `! -o %i`（%i 由 wg-quick 展开为接口名），
	// 即"只要不是从隧道出去的流量就做 NAT"，从而无需知道对端网卡叫什么。
	// 注意 iptables 要求感叹号写在选项之前：`! -o wg0` 合法，`-o ! wg0` 会报错。
	if peer.EnableForwarding {
		match := "! -o %i"
		if iface := strings.TrimSpace(peer.ForwardInterface); iface != "" {
			match = "-o " + iface
		}

		postUp := fmt.Sprintf(`PostUp = iptables -t nat -A POSTROUTING %s -j MASQUERADE; iptables -A FORWARD -i %%i -j ACCEPT; iptables -A FORWARD -o %%i -j ACCEPT
PreDown = iptables -t nat -D POSTROUTING %s -j MASQUERADE; iptables -D FORWARD -i %%i -j ACCEPT; iptables -D FORWARD -o %%i -j ACCEPT
`,
			match,
			match,
		)
		configContent += postUp
	}

	// 添加 Peer 配置；启用预共享密钥时写入 [Peer] 段内
	presharedLine := ""
	if peer.PresharedKey != "" {
		presharedLine = fmt.Sprintf("PresharedKey = %s\n", peer.PresharedKey)
	}

	configContent += fmt.Sprintf(`
[Peer]
PublicKey = %s
%sEndpoint = %s
AllowedIPs = %s
PersistentKeepalive = %d
`,
		wgServer.WgPublicKey,
		presharedLine,
		serverEndpoint,
		allowedIPs,
		peer.PersistentKeepalive,
	)

	// 返回JSON格式的配置文本
	response.Success(c, "Config retrieved successfully", map[string]string{
		"config": configContent,
	})
}

// AdminDeleteWireguardServer 删除用户的 WireGuard 服务器（管理员）
func AdminDeleteWireguardServer(c *gin.Context) {
	serverIDStr := c.Param("id")
	serverID, err := strconv.ParseUint(serverIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid server ID", nil)
		return
	}

	var server models.WireguardServer
	if err := database.DB.Preload("User").First(&server, serverID).Error; err != nil {
		response.NotFound(c, "Server not found")
		return
	}

	// 使用事务确保数据一致性
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 先删除所有关联的 peers
		if err := tx.Where("server_id = ?", serverID).Delete(&models.WireguardPeer{}).Error; err != nil {
			return fmt.Errorf("failed to delete peers: %v", err)
		}

		// 2. 删除服务器记录
		if err := tx.Delete(&server).Error; err != nil {
			return fmt.Errorf("failed to delete server: %v", err)
		}

		// 3. 清理网络资源（命名空间、veth、iptables规则等）
		// 使用 UserNetworkService 清理网络环境
		networkService := services.NewUserNetworkServiceFromRuntime()

		// 清理网络环境（即使失败也继续，因为数据库记录已删除）
		if err := networkService.DestroyUserNetwork(&server, server.User.UserUID); err != nil {
			// 记录错误但不回滚事务
			log.Printf("Warning: Failed to cleanup network resources for server %d: %v", serverID, err)
		}

		return nil
	})

	if err != nil {
		response.InternalError(c, fmt.Sprintf("Failed to delete server: %v", err))
		return
	}

	response.Success(c, "Server and all associated peers deleted successfully", nil)
}

// AdminToggleWireguardServer 启用/禁用用户的 WireGuard 服务器（管理员）
func AdminToggleWireguardServer(c *gin.Context) {
	serverIDStr := c.Param("id")
	serverID, err := strconv.ParseUint(serverIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid server ID", nil)
		return
	}

	// 使用指针接收，避免漏传字段时被当成 "enabled=false" 误禁用
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}
	if req.Enabled == nil {
		response.BadRequest(c, "enabled is required", nil)
		return
	}

	var server models.WireguardServer
	if err := database.DB.First(&server, serverID).Error; err != nil {
		response.NotFound(c, "Server not found")
		return
	}

	// 更新状态
	if err := database.DB.Model(&server).Update("enabled", *req.Enabled).Error; err != nil {
		response.InternalError(c, "Failed to update server status")
		return
	}

	// TODO: 实现启用/禁用命名空间网络的逻辑
	// 可以通过 iptables 规则来实现禁用功能

	message := "Server enabled successfully"
	if !*req.Enabled {
		message = "Server disabled successfully"
	}

	response.Success(c, message, nil)
}

// maxRateLimitMbps 速率限制上限（Mbps）
const maxRateLimitMbps = 100000

// AdminSetRateLimit 设置用户 WireGuard 服务器的速率限制（管理员）
func AdminSetRateLimit(c *gin.Context) {
	serverIDStr := c.Param("id")
	serverID, err := strconv.ParseUint(serverIDStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid server ID", nil)
		return
	}

	var req struct {
		DownloadRate int `json:"download_rate"` // Mbps
		UploadRate   int `json:"upload_rate"`   // Mbps
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	if req.DownloadRate < 0 || req.DownloadRate > maxRateLimitMbps {
		response.BadRequest(c, fmt.Sprintf("download_rate must be within 0..%d Mbps", maxRateLimitMbps), nil)
		return
	}
	if req.UploadRate < 0 || req.UploadRate > maxRateLimitMbps {
		response.BadRequest(c, fmt.Sprintf("upload_rate must be within 0..%d Mbps", maxRateLimitMbps), nil)
		return
	}

	var server models.WireguardServer
	if err := database.DB.First(&server, serverID).Error; err != nil {
		response.NotFound(c, "Server not found")
		return
	}

	// 更新速率限制
	updates := map[string]interface{}{
		"download_rate": req.DownloadRate,
		"upload_rate":   req.UploadRate,
	}
	if err := database.DB.Model(&server).Updates(updates).Error; err != nil {
		response.InternalError(c, "Failed to update rate limit")
		return
	}

	// TODO: 实现 tc (traffic control) 命令来设置实际的速率限制
	// 需要在命名空间中执行 tc 命令

	response.Success(c, "Rate limit set successfully", nil)
}
