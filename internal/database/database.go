package database

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/models"
	"cloud-platform/internal/services"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 说明：数据库为嵌入式 SQLite（glebarez/sqlite，纯 Go 实现，无需 CGO）。
// 连接数、busy_timeout、WAL 等参数来自 config 的 database 段，
// 默认 max_open_conns=1（串行访问，从根上规避 "database is locked"）。

var DB *gorm.DB

// Close 释放数据库连接。
//
// SQLite 在 WAL 模式下，已提交事务的持久性由 WAL 保证，进程被强杀也不会丢数据
// （下次打开会自动重放）。但正常退出时显式关闭会让 WAL 合并回主库，退出后的
// 数据目录更干净——这对「直接复制数据文件做备份」的场景尤其重要，否则副本必须
// 连同 -wal / -shm 一起带走才算完整。
func Close() error {
	if DB == nil {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("failed to access underlying sql.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close sqlite database: %w", err)
	}
	return nil
}

func InitDB() error {
	cfg := config.AppConfig
	dsn := cfg.GetDSN()

	// 首次启动时数据库文件的父目录通常还不存在，先创建
	if dir := filepath.Dir(cfg.DatabasePath()); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create database directory %s: %w", dir, err)
		}
	}

	// SQLite 下只用 Warn 级别日志，避免把常规查询刷进日志
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("failed to open sqlite database %s: %w", cfg.DatabasePath(), err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to access underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxOpenConns)
	// 文件型数据库无需回收"可能失效"的网络连接，保持零值即不限制存活时间

	// 显式 Ping 一次，让文件不可写（权限/磁盘）等问题在启动阶段暴露
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	DB = db

	// Auto migrate the schema
	err = db.AutoMigrate(
		&models.User{},
		&models.WireguardServer{},
		&models.WireguardPeer{},
		&models.MonitoringRecord{},
		&models.Setting{},
	)
	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Create default platform admin if not exists
	if err := createDefaultPlatformAdmin(); err != nil {
		return fmt.Errorf("failed to create default platform admin: %w", err)
	}

	return nil
}

// createDefaultPlatformAdmin 在没有任何管理员时创建默认管理员账号。
// 账号信息取自配置的 default 段，密码在运行时用 bcrypt 生成（不再硬编码 hash）。
// 网络 provisioning 失败不会阻止管理员创建：账号仍可登录，只是没有独立的 WireGuard 网络。
func createDefaultPlatformAdmin() error {
	var count int64
	if err := DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count existing platform admins: %w", err)
	}
	if count > 0 {
		return nil
	}

	adminCfg := config.AppConfig.Default

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(adminCfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash default admin password: %w", err)
	}

	defaultAdmin := models.User{
		Email:        adminCfg.AdminEmail,
		PasswordHash: string(hashedPassword),
		Name:         adminCfg.AdminName,
		Role:         models.RoleAdmin,
		UserUID:      "admin001", // 固定的管理员UserUID
	}

	if err := DB.Create(&defaultAdmin).Error; err != nil {
		return fmt.Errorf("failed to create default admin %s: %w", adminCfg.AdminEmail, err)
	}

	// 为管理员配置网络环境
	networkService := services.NewUserNetworkServiceFromRuntime()

	var existingServers []models.WireguardServer
	if err := DB.Find(&existingServers).Error; err != nil {
		log.Printf("Warning: default admin %s was created, but reading existing network allocations failed: %v",
			adminCfg.AdminEmail, err)
		return nil
	}

	wgServer, err := networkService.ProvisionUserNetwork(&defaultAdmin, services.AllocationsFromServers(existingServers))
	if err != nil {
		// 保留管理员账号：网络配置失败时管理员仍可登录并后续修复网络。
		log.Printf("Warning: default admin %s was created, but network provisioning failed "+
			"(namespace/wireguard setup error): %v", adminCfg.AdminEmail, err)
		return nil
	}

	// 保存管理员的网络配置信息到数据库
	if err := DB.Create(wgServer).Error; err != nil {
		log.Printf("Warning: default admin %s was created, but saving its network record failed; "+
			"rolling back the provisioned network resources: %v", adminCfg.AdminEmail, err)
		if destroyErr := networkService.DestroyUserNetwork(wgServer, defaultAdmin.UserUID); destroyErr != nil {
			log.Printf("Warning: failed to roll back network resources for default admin %s: %v",
				adminCfg.AdminEmail, destroyErr)
		}
		return nil
	}

	log.Printf("Default platform admin created: %s (network provisioned)", adminCfg.AdminEmail)
	return nil
}
