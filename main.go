package main

import (
	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/routes"
	"cloud-platform/internal/services"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	if err := config.LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	if err := database.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Start monitoring service (collect every 10 seconds)
	monitoringService := services.NewMonitoringService(database.DB, 10*time.Second)
	go monitoringService.Start()

	// Setup Gin
	r := gin.Default()

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Setup routes
	routes.SetupRoutes(r)

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"success":   true,
			"message":   "Cloud Platform API is running",
			"timestamp": time.Now().Unix(),
			"data": gin.H{
				"status":  "healthy",
				"version": "1.0.0",
			},
		})
	})

	// Start server
	port := fmt.Sprintf(":%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %d", config.AppConfig.Server.Port)
	log.Printf("Default platform admin: admin@platform.com / password")

	if err := r.Run(port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
