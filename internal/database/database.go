package database

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/models"
	"cloud-platform/internal/services"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() error {
	dsn := config.AppConfig.GetDSN()

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	DB = db

	// Auto migrate the schema
	err = db.AutoMigrate(
		&models.User{},
		&models.WireguardServer{},
		&models.WireguardPeer{},
		&models.MonitoringRecord{},
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

func createDefaultPlatformAdmin() error {
	var count int64
	DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&count)

	if count == 0 {
		// Create default admin
		defaultAdmin := models.User{
			Email:        "admin@platform.com",
			PasswordHash: "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi", // password: password
			Name:         "admin",
			Role:         models.RoleAdmin,
			UserUID:      "admin001", // 固定的管理员UserUID
		}

		if err := DB.Create(&defaultAdmin).Error; err != nil {
			return err
		}

		// 为管理员配置网络环境
		DB.First(&defaultAdmin, defaultAdmin.ID)
		
		networkService := services.NewUserNetworkService(
			config.AppConfig.Network.ConfigDir,
			config.AppConfig.Network.BaseSubnet,
			config.AppConfig.Network.BasePort,
			config.AppConfig.Network.OutInterface,
		)

		wgServer, err := networkService.ProvisionUserNetwork(&defaultAdmin)
		if err != nil {
			fmt.Printf("Warning: Failed to provision admin network: %v\n", err)
			fmt.Println("Default admin created: admin@platform.com / password (without network)")
		} else {
			// 保存管理员的网络配置信息到数据库
			if err := DB.Create(wgServer).Error; err != nil {
				fmt.Printf("Warning: Failed to save admin network info: %v\n", err)
				networkService.DestroyUserNetwork(wgServer, defaultAdmin.UserUID)
			} else {
				fmt.Println("Default admin created: admin@platform.com / password (with network)")
			}
		}
	}

	return nil
}
