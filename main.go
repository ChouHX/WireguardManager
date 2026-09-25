package main

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/handlers"
	"cloud-platform/internal/middleware"
	"cloud-platform/internal/reconcile"
	"cloud-platform/internal/response"
	"cloud-platform/internal/routes"
	"cloud-platform/internal/services"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// shutdownTimeout 优雅关闭的最长等待时间
const shutdownTimeout = 10 * time.Second

// reconcileWaitTimeout 关闭时等待启动期收敛收尾的上限。
// 大账号量场景下收敛可能较久，超过这个时间就放弃等待（收敛本身幂等，下次启动会继续）。
const reconcileWaitTimeout = 20 * time.Second

// maxRequestBodyBytes 请求体大小上限。
// 所有接口都只接收 JSON，1 MiB 对任何合法请求都绰绰有余。
const maxRequestBodyBytes = 1 << 20

func main() {
	// Load configuration
	if err := config.LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := config.AppConfig.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	gin.SetMode(config.AppConfig.GinMode())

	// Initialize database
	if err := database.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 运行时配置：config.yaml 只作为初始默认值，之后以管理界面中的设置为准
	if _, err := services.InitSettings(database.DB, settingsDefaults()); err != nil {
		log.Fatalf("Failed to initialize settings: %v", err)
	}

	// SIGINT/SIGTERM 触发优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 隧道 MTU 自检：这类不匹配完全静默（小包能过、满长包被丢），
	// 只有在启动时主动比对一次，才可能提前发现。
	warnIfTunnelMTUMismatch()

	monitoringCfg := config.AppConfig.Monitoring

	// 后台指标采集：所有监控接口读内存快照，不再在请求内阻塞采样
	collector := services.InitMetricsCollector(ctx, monitoringCfg.Interval())
	collector.StartBackground()

	// 监控记录落库 + 过期清理
	monitoringService := services.NewMonitoringService(ctx, database.DB, monitoringCfg.Interval())
	monitoringService.StartBackground(monitoringCfg.CleanupInterval(), monitoringCfg.Retention())

	// 设备实时存活探测（TCP SYN/RST，秒级感知；客户端无需 Agent）
	var livenessMonitor *services.LivenessMonitor
	if config.AppConfig.Liveness.Enabled {
		livenessMonitor = services.NewLivenessMonitor(database.DB, config.AppConfig.Liveness)
		livenessMonitor.Start(ctx)
		handlers.SetLivenessMonitor(livenessMonitor)
	} else {
		log.Println("Liveness probing is disabled by configuration")
	}

	// 启动期收敛：把历史账号迁移到「原生跨命名空间」形态，并修复缺失的命名空间/接口。
	// 放后台执行以免拖慢服务就绪；沿用数据库中已记录的端口与地址，客户端无需重新导入配置。
	//
	// 用 done 通道持有它，退出时才知道它是否已经收尾——否则可能停在「命名空间建了一半」
	// 的位置就被进程终止（虽然下次启动会重建，但留下半成品没有意义）。
	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		reconcile.Networks()
	}()

	// Setup Gin
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		// 探针请求不写访问日志，避免日志噪声
		SkipPaths: []string{"/health", "/ready"},
	}))
	r.Use(gin.Recovery())
	r.Use(corsMiddleware(config.AppConfig.Server.CorsOrigins))

	// 所有接口都只收 JSON（没有文件上传），因此请求体上限可以取得很小
	r.Use(middleware.BodySizeLimit(maxRequestBodyBytes))

	// 收窄可信代理：gin 默认信任所有来源，任何客户端都能用 X-Forwarded-For
	// 伪造自己的地址。这既让访问日志失真，也直接废掉按 IP 的限速。
	if err := setupTrustedProxies(r); err != nil {
		log.Fatalf("Invalid trusted proxy configuration: %v", err)
	}

	// Setup routes
	routes.SetupRoutes(r)

	// 单进程/单容器模式：设置 WEB_ROOT 后由后端直接托管前端构建产物
	if webRoot := strings.TrimSpace(os.Getenv("WEB_ROOT")); webRoot != "" {
		if err := routes.MountFrontend(r, webRoot); err != nil {
			log.Fatalf("Failed to serve frontend from %s: %v", webRoot, err)
		}
		log.Printf("Serving frontend from %s", webRoot)
	}

	// Health check endpoint（存活探针）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success":   true,
			"message":   "Cloud Platform API is running",
			"timestamp": time.Now().Unix(),
			"data": gin.H{
				"status":  "healthy",
				"version": "1.0.0",
			},
		})
	})

	// Readiness endpoint（就绪探针：数据库可达 + 指标采集已就绪）
	r.GET("/ready", func(c *gin.Context) {
		sqlDB, err := database.DB.DB()
		if err != nil {
			response.ServiceUnavailable(c, "Database handle unavailable: "+err.Error())
			return
		}

		pingCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := sqlDB.PingContext(pingCtx); err != nil {
			response.ServiceUnavailable(c, "Database is not reachable: "+err.Error())
			return
		}

		metricsStatus := "ready"
		if !collector.Ready() {
			metricsStatus = "warming_up"
		}

		response.Success(c, "Service is ready", gin.H{
			"status":   "ready",
			"version":  "1.0.0",
			"database": "ok",
			"metrics":  metricsStatus,
		})
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.AppConfig.Server.Port),
		Handler: r,
	}

	serverErr := make(chan error, 1)

	go func() {
		log.Printf("Server listening on port %d (gin mode: %s)", config.AppConfig.Server.Port, gin.Mode())
		log.Printf("Default platform admin: %s", config.AppConfig.Default.AdminEmail)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Printf("Server error: %v", err)
	case <-ctx.Done():
		log.Println("Shutdown signal received, stopping background services...")
	}

	// 先停后台任务，再停 HTTP 服务
	if livenessMonitor != nil {
		livenessMonitor.Stop()
	}
	monitoringService.Stop()
	collector.Stop()

	// 等启动期收敛收尾，避免它正处在「创建命名空间/移动接口」的中间步骤时进程退出。
	// 收敛本身是幂等的，故超时后继续退出是安全的，只是留个记录便于排查。
	select {
	case <-reconcileDone:
	case <-time.After(reconcileWaitTimeout):
		log.Println("Startup reconcile is still running; proceeding with shutdown")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Graceful shutdown failed, forcing close: %v", err)
		if closeErr := srv.Close(); closeErr != nil {
			log.Printf("Failed to close server: %v", closeErr)
		}
	}

	// 显式关闭数据库：让 SQLite 把 WAL 合并回主库，退出后的数据目录处于干净状态
	if err := database.Close(); err != nil {
		log.Printf("Failed to close database: %v", err)
	}

	log.Println("Server stopped")
}

// warnIfTunnelMTUMismatch 在启动时比对出口链路 MTU 与隧道 MTU，不匹配则告警。
//
// 这类故障的症状是「隧道能建立，却传不动数据」，而且不会产生任何错误日志：
// 握手与探测都是小包，能顺利通过；只有满长的数据包被底层静默丢弃。
// 正因为静默，它常被误判成服务端问题——这里主动比对一次，把它摆到日志里。
func warnIfTunnelMTUMismatch() {
	settings := services.GetSettings()
	if settings == nil {
		return
	}

	outInterface := settings.String(services.SettingNetworkOutInterface, config.AppConfig.Network.OutInterface)
	tunnelMTU := settings.Int(services.SettingNetworkMTU, config.AppConfig.Network.MTU)

	if advice := services.CheckTunnelMTUFit(outInterface, tunnelMTU); advice != "" {
		log.Printf("WARNING: %s", advice)
	}
}

// setupTrustedProxies 配置 Gin 的可信反向代理。
//
// 未配置时传 nil，表示不信任任何来源，ClientIP() 会回落到 TCP 对端地址。
// 这比 Gin 的默认值（信任 0.0.0.0/0）安全得多：默认值下任何客户端都能用
// X-Forwarded-For 冒充任意地址，按 IP 限速与访问日志都会失真。
func setupTrustedProxies(r *gin.Engine) error {
	proxies := config.AppConfig.Server.TrustedProxies
	if len(proxies) == 0 {
		return r.SetTrustedProxies(nil)
	}
	log.Printf("Trusting %d proxy entr(ies) for client IP resolution: %v", len(proxies), proxies)
	return r.SetTrustedProxies(proxies)
}

// corsMiddleware 按配置的允许来源集合设置 CORS 头，并直接放行 OPTIONS 预检。
func corsMiddleware(origins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(origins))

	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAll = true
		}
		allowed[origin] = struct{}{}
	}
	if len(allowed) == 0 {
		allowAll = true
	}

	// 放行全部来源时明确告警：这不是错误（单容器模式下前端与接口同源，本就不需要 CORS），
	// 但在对外暴露的场景下应当收窄到确切的来源，否则任意站点都能带着凭据调用接口。
	if allowAll {
		log.Println("CORS: allowing every origin (server.cors_origins is empty or contains \"*\")")
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		switch {
		case allowAll:
			if origin != "" {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			} else {
				c.Header("Access-Control-Allow-Origin", "*")
			}
		default:
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Request-ID")

		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Max-Age", "86400")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// settingsDefaults 把 config.yaml 中的可管理项整理成初始默认值。
// 仅在首次启动（数据库中尚无该键）时写入，之后管理界面的修改优先生效。
func settingsDefaults() map[string]string {
	cfg := config.AppConfig

	return map[string]string{
		services.SettingNetworkServerIP:         cfg.Network.ServerIP,
		services.SettingNetworkOutInterface:     cfg.Network.OutInterface,
		services.SettingNetworkBaseSubnet:       cfg.Network.BaseSubnet,
		services.SettingNetworkBasePort:         strconv.Itoa(cfg.Network.BasePort),
		services.SettingNetworkDNS:              cfg.Network.DNS,
		services.SettingNetworkClientAllowedIPs: cfg.Network.ClientAllowedIPs,

		services.SettingMonitoringInterval:  strconv.Itoa(cfg.Monitoring.IntervalSeconds),
		services.SettingMonitoringRetention: strconv.Itoa(cfg.Monitoring.RetentionHours),

		services.SettingLivenessProbePort: strconv.Itoa(cfg.Liveness.ProbePort),

		services.SettingJWTExpireHours: strconv.Itoa(cfg.JWT.ExpireHours),

		services.SettingPeerDefaultPSK: "false",
	}
}
