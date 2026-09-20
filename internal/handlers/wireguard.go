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

		// 4. 创建peer记录
		peer := models.WireguardPeer{
			ServerID:            wgServer.ID,
			PublicKey:           publicKey,
			PrivateKey:          privateKey,
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

	// 5. 添加到WireGuard配置（使用peer的IP地址作为allowed-ips）
	if err := wgService.AddPeer(namespace, wgInterface, peer.PublicKey, peerAllowedIPs, ""); err != nil {
		return fmt.Errorf("Failed to add peer to WireGuard: %w", err)
	}

	// 6. 同步路由规则：确保命名空间知道如何访问peer指定的网段
	// 注意：如果allowedIPs就是peer自己的IP，不需要额外的路由规则（WireGuard已经处理）
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

	if len(updates) == 0 {
		response.BadRequest(c, "No valid fields to update", nil)
		return
	}

	// 如果需要更新WireGuard配置（AllowedIPs变化）
	// 注意：AllowedIPs 是 peer 可以访问的网段，不影响 WireGuard 配置中的 allowed-ips
	// WireGuard 配置中的 allowed-ips 始终是 peer 的 IP 地址
	if needWgUpdate {
		netnsService := services.NewNetnsService()

		// 1. 清理旧的路由和iptables规则
		clearPeerRoutes(netnsService, wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)

		// 2. 添加新的路由和iptables规则
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

	// 客户端 DNS 取自配置（默认 "1.1.1.1, 8.8.8.8"）
	dns := strings.TrimSpace(config.AppConfig.Network.DNS)
	if dns == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}

	// 生成服务器端点地址：优先使用服务器记录的 endpoint，缺失时回退到 配置的 ServerIP:端口
	serverEndpoint := strings.TrimSpace(wgServer.ServerEndpoint)
	if serverEndpoint == "" {
		serverEndpoint = fmt.Sprintf("%s:%d", config.AppConfig.Network.ServerIP, wgServer.WgPort)
	}

	// 默认 AllowedIPs 为所有流量（全局代理）
	allowedIPs := "0.0.0.0/0, ::/0"

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

	// 如果启用转发，添加 PostUp 和 PreDown 脚本
	if peer.EnableForwarding && peer.ForwardInterface != "" {
		postUp := fmt.Sprintf(`PostUp = iptables -t nat -A POSTROUTING -o %s -j MASQUERADE; iptables -A FORWARD -i %%i -j ACCEPT; iptables -A FORWARD -o %%i -j ACCEPT
PreDown = iptables -t nat -D POSTROUTING -o %s -j MASQUERADE; iptables -D FORWARD -i %%i -j ACCEPT; iptables -D FORWARD -o %%i -j ACCEPT
`,
			peer.ForwardInterface,
			peer.ForwardInterface,
		)
		configContent += postUp
	}

	// 添加 Peer 配置
	configContent += fmt.Sprintf(`
[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = %s
PersistentKeepalive = %d
`,
		wgServer.WgPublicKey,
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
		networkService := services.NewUserNetworkService(
			config.AppConfig.Network.ConfigDir,
			config.AppConfig.Network.BaseSubnet,
			config.AppConfig.Network.BasePort,
			config.AppConfig.Network.OutInterface,
		)

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
