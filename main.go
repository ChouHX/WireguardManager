package main

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/handlers"
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

	monitoringCfg := config.AppConfig.Monitoring

	// 后台指标采集：所有监控接口读内存快照，不再在请求内阻塞采样
	collector := services.InitMetricsCollector(monitoringCfg.Interval())
	go collector.Start(ctx)

	// 监控记录落库 + 过期清理
	monitoringService := services.NewMonitoringService(database.DB, monitoringCfg.Interval())
	go monitoringService.Start(ctx)
	go monitoringService.RunCleanupLoop(ctx, monitoringCfg.CleanupInterval(), monitoringCfg.Retention())

	// 设备实时存活探测（TCP SYN/RST，秒级感知；客户端无需 Agent）
	var livenessMonitor *services.LivenessMonitor
	if config.AppConfig.Liveness.Enabled {
		livenessMonitor = services.NewLivenessMonitor(database.DB, config.AppConfig.Liveness)
		livenessMonitor.Start(ctx)
		handlers.SetLivenessMonitor(livenessMonitor)
	} else {
		log.Println("Liveness probing is disabled by configuration")
	}

	// Setup Gin
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		// 探针请求不写访问日志，避免日志噪声
		SkipPaths: []string{"/health", "/ready"},
	}))
	r.Use(gin.Recovery())
	r.Use(corsMiddleware(config.AppConfig.Server.CorsOrigins))

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Graceful shutdown failed, forcing close: %v", err)
		if closeErr := srv.Close(); closeErr != nil {
			log.Printf("Failed to close server: %v", closeErr)
		}
	}

	log.Println("Server stopped")
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
