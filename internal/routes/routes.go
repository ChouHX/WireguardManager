package routes

import (
	"time"

	"cloud-platform/internal/config"
	"cloud-platform/internal/handlers"
	"cloud-platform/internal/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// Public routes
	//
	// 这两个接口无需认证，是外部唯一能直接触及的入口，必须限速：
	// login 不限速即可被无限次猜密码，register 每成功一次都会创建命名空间与隧道，
	// 而隧道网段只有 254 个，刷满之后正常用户就注册不进来了。
	api.POST("/register", middleware.RateLimitByIP(registerLimit()), handlers.Register)
	api.POST("/login", middleware.RateLimitByIP(loginLimit()), handlers.Login)

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

		// 可用网络接口（用于转发出口选择，默认值为探测到的出口接口）
		wg.GET("/interfaces", handlers.GetNetworkInterfaces)

		// 设备实时在线状态（服务端 TCP 探测，客户端无需 Agent）
		wg.GET("/liveness", handlers.GetLiveness)
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
		admin.GET("/wireguard/liveness", handlers.GetAdminLiveness)                // 管理端：各服务器在线设备统计
		
		// 管理员管理 WireGuard 服务器
		admin.DELETE("/wireguard/servers/:id", handlers.AdminDeleteWireguardServer) // 删除服务器
		admin.POST("/wireguard/users/:id/server", handlers.AdminRecreateWireguardServer) // 为账号重新分配隧道（误删后的补救）
		admin.PATCH("/wireguard/servers/:id/toggle", handlers.AdminToggleWireguardServer) // 启用/禁用服务器
		admin.PATCH("/wireguard/servers/:id/ratelimit", handlers.AdminSetRateLimit) // 设置速率限制
		
		// 运行时配置（管理界面可调）
		admin.GET("/settings", handlers.GetRuntimeSettings)
		admin.PATCH("/settings", handlers.UpdateRuntimeSettings)

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

// loginLimit 登录接口的限速配置。
func loginLimit() middleware.RateLimitConfig {
	cfg := config.AppConfig.RateLimit
	if !cfg.Enabled {
		return middleware.RateLimitConfig{Limit: 0}
	}
	return middleware.RateLimitConfig{
		Limit:  cfg.LoginPerMinute,
		Window: time.Minute,
	}
}

// registerLimit 注册接口的限速配置，窗口取一小时。
func registerLimit() middleware.RateLimitConfig {
	cfg := config.AppConfig.RateLimit
	if !cfg.Enabled {
		return middleware.RateLimitConfig{Limit: 0}
	}
	return middleware.RateLimitConfig{
		Limit:  cfg.RegisterPerHour,
		Window: time.Hour,
	}
}
