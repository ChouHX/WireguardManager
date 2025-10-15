package routes

import (
	"cloud-platform/internal/handlers"
	"cloud-platform/internal/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// Public routes
	api.POST("/register", handlers.Register)
	api.POST("/login", handlers.Login)

	// Protected routes
	protected := api.Group("")
	protected.Use(middleware.RequireAuth())

	// User routes
	protected.GET("/me", handlers.GetMe)
	protected.PATCH("/me", handlers.UpdateProfile)

	// WireGuard routes
	wg := protected.Group("/wireguard")
	{
		// 流量统计
		wg.GET("/traffic", handlers.GetMyTrafficSummary) // 用户流量摘要（用于轮询）
		
		// Peer管理
		wg.GET("/peers", handlers.GetMyPeers)
		wg.POST("/peers", handlers.AddPeer)
		wg.PATCH("/peers/:id", handlers.UpdatePeer)
		wg.DELETE("/peers/:id", handlers.DeletePeer)
		wg.GET("/peers/:id/config", handlers.GetPeerConfig)
	}

	// Admin routes
	admin := protected.Group("/admin")
	admin.Use(middleware.RequireAdmin())
	{
		admin.GET("/users", handlers.GetAllUsers)
		admin.DELETE("/users/:id", handlers.DeleteUser)
		admin.PATCH("/users/:id", handlers.UpdateUser)
		
		// 管理员查看所有用户流量
		admin.GET("/wireguard/traffic", handlers.GetAdminTrafficStats)              // 管理员流量统计（精简版）
		admin.GET("/wireguard/traffic/:id", handlers.GetUserTrafficStats)          // 查看单个用户详情
		
		// 管理员管理 WireGuard 服务器
		admin.DELETE("/wireguard/servers/:id", handlers.AdminDeleteWireguardServer) // 删除服务器
		admin.PATCH("/wireguard/servers/:id/toggle", handlers.AdminToggleWireguardServer) // 启用/禁用服务器
		admin.PATCH("/wireguard/servers/:id/ratelimit", handlers.AdminSetRateLimit) // 设置速率限制
		
		// 系统监控
		admin.GET("/monitoring/system", handlers.GetSystemStats)        // 获取系统整体统计
		admin.GET("/monitoring/cpu", handlers.GetCPUStats)              // 获取CPU统计
		admin.GET("/monitoring/memory", handlers.GetMemoryStats)        // 获取内存统计
		admin.GET("/monitoring/disk", handlers.GetDiskStats)            // 获取磁盘统计
		admin.GET("/monitoring/network", handlers.GetNetworkStats)      // 获取网络统计
		admin.GET("/monitoring/chart", handlers.GetMonitoringChart)     // 获取图表数据（简化版）
		admin.GET("/monitoring/history", handlers.GetMonitoringHistory) // 获取历史监控记录（完整版）
		admin.GET("/monitoring/stats", handlers.GetMonitoringStats)     // 获取聚合统计数据（完整版）
	}
}
