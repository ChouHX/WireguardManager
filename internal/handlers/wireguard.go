package handlers

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetMyTrafficStats 获取当前用户的流量统计（完整信息）
func GetMyTrafficStats(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*models.User)

	// 查询用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	// 创建WireGuard服务
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 获取流量统计
	stats, err := wgService.GetDetailedStats(wgServer.Namespace, wgServer.WgInterface)
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
	user, _ := c.Get("user")
	u := user.(*models.User)

	// 查询用户的 WireGuard 服务器
	var wgServer models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&wgServer).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}

	// 创建WireGuard服务
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 获取流量统计
	stats, err := wgService.GetDetailedStats(wgServer.Namespace, wgServer.WgInterface)
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
	if err := database.DB.Preload("User").Find(&servers).Error; err != nil {
		response.InternalError(c, "Failed to fetch wireguard servers")
		return
	}

	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	var adminTraffic []models.AdminUserTraffic
	for _, server := range servers {
		stats, err := wgService.GetDetailedStats(server.Namespace, server.WgInterface)
		if err != nil {
			// 记录错误但继续处理其他用户
			continue
		}

		adminTraffic = append(adminTraffic, models.AdminUserTraffic{
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
		})
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

	stats, err := wgService.GetDetailedStats(wgServer.Namespace, wgServer.WgInterface)
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
	user, _ := c.Get("user")
	u := user.(*models.User)

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
	var peerResponses []models.WireguardPeerResponse
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
	EnableForwarding    bool   `json:"enable_forwarding"`    // 是否启用转发（作为网关）
	ForwardInterface    string `json:"forward_interface"`    // 转发接口名称（如 eth0）
}

// AddPeer 添加新的peer
func AddPeer(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*models.User)

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

	// 创建WireGuard服务实例
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	// 1. 自动生成peer的密钥对
	privateKey, publicKey, err := wgService.GenerateKeys()
	if err != nil {
		response.InternalError(c, "Failed to generate peer keys: "+err.Error())
		return
	}

	// 2. 自动分配peer IP地址（从服务器网段中分配）
	peerIP, err := allocatePeerIP(wgServer.ID, wgServer.WgAddress)
	if err != nil {
		response.InternalError(c, "Failed to allocate peer IP: "+err.Error())
		return
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
		response.InternalError(c, "Failed to create peer record")
		return
	}

	// 5. 添加到WireGuard配置（使用peer的IP地址作为allowed-ips）
	if err := wgService.AddPeer(wgServer.Namespace, wgServer.WgInterface, publicKey, peerIP+"/32", ""); err != nil {
		// 回滚数据库记录
		database.DB.Delete(&peer)
		response.InternalError(c, "Failed to add peer to WireGuard: "+err.Error())
		return
	}

	// 6. 同步路由规则：确保命名空间知道如何访问peer指定的网段
	// 注意：如果allowedIPs就是peer自己的IP，不需要额外的路由规则（WireGuard已经处理）
	netnsService := services.NewNetnsService()
	if allowedIPs != peerIP+"/32" && allowedIPs != "0.0.0.0/0" {
		if err := netnsService.AddRouteForPeer(wgServer.Namespace, wgServer.WgInterface, allowedIPs); err != nil {
			// 路由添加失败，回滚WireGuard配置和数据库
			wgService.RemovePeer(wgServer.Namespace, wgServer.WgInterface, publicKey)
			database.DB.Delete(&peer)
			response.InternalError(c, "Failed to add route for peer: "+err.Error())
			return
		}

		// 7. 同步iptables规则：允许转发peer网段的流量
		if err := netnsService.AddIptablesRuleForPeer(wgServer.Namespace, allowedIPs); err != nil {
			// iptables失败，回滚所有配置
			netnsService.DeleteRouteForPeer(wgServer.Namespace, wgServer.WgInterface, allowedIPs)
			wgService.RemovePeer(wgServer.Namespace, wgServer.WgInterface, publicKey)
			database.DB.Delete(&peer)
			response.InternalError(c, "Failed to add iptables rule for peer: "+err.Error())
			return
		}
	}

	response.Created(c, "Peer added successfully", peer.ToResponse())
}

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

// DeletePeer 删除peer
func DeletePeer(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*models.User)

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
	user, _ := c.Get("user")
	u := user.(*models.User)

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
		if peer.AllowedIPs != "" && peer.AllowedIPs != "0.0.0.0/0" {
			netnsService.DeleteIptablesRuleForPeer(wgServer.Namespace, peer.AllowedIPs)
			netnsService.DeleteRouteForPeer(wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)
		}
		
		// 2. 添加新的路由和iptables规则
		if req.AllowedIPs != "" && req.AllowedIPs != "0.0.0.0/0" {
			if err := netnsService.AddRouteForPeer(wgServer.Namespace, wgServer.WgInterface, req.AllowedIPs); err != nil {
				// 尝试恢复旧规则
				if peer.AllowedIPs != "" && peer.AllowedIPs != "0.0.0.0/0" {
					netnsService.AddRouteForPeer(wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)
					netnsService.AddIptablesRuleForPeer(wgServer.Namespace, peer.AllowedIPs)
				}
				response.InternalError(c, "Failed to add route for peer: "+err.Error())
				return
			}
			
			if err := netnsService.AddIptablesRuleForPeer(wgServer.Namespace, req.AllowedIPs); err != nil {
				// 回滚
				netnsService.DeleteRouteForPeer(wgServer.Namespace, wgServer.WgInterface, req.AllowedIPs)
				if peer.AllowedIPs != "" && peer.AllowedIPs != "0.0.0.0/0" {
					netnsService.AddRouteForPeer(wgServer.Namespace, wgServer.WgInterface, peer.AllowedIPs)
					netnsService.AddIptablesRuleForPeer(wgServer.Namespace, peer.AllowedIPs)
				}
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

// GetPeerConfig 获取peer的WireGuard配置（统一接口，返回JSON格式）
func GetPeerConfig(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*models.User)

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

	// 生成服务器端点地址（从配置文件获取服务器IP）
	serverEndpoint := fmt.Sprintf("%s:%d", config.AppConfig.Network.ServerIP, wgServer.WgPort)

	// 默认 AllowedIPs 为所有流量（全局代理）
	allowedIPs := "0.0.0.0/0, ::/0"
	
	// 基础配置内容
	configContent := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 1.1.1.1, 8.8.8.8
`,
		peer.PrivateKey,
		peer.PeerAddress+"/32",
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

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	var server models.WireguardServer
	if err := database.DB.First(&server, serverID).Error; err != nil {
		response.NotFound(c, "Server not found")
		return
	}

	// 更新状态
	if err := database.DB.Model(&server).Update("enabled", req.Enabled).Error; err != nil {
		response.InternalError(c, "Failed to update server status")
		return
	}

	// TODO: 实现启用/禁用命名空间网络的逻辑
	// 可以通过 iptables 规则来实现禁用功能

	message := "Server enabled successfully"
	if !req.Enabled {
		message = "Server disabled successfully"
	}

	response.Success(c, message, nil)
}

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
