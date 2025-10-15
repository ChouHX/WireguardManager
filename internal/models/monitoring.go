package models

import (
	"time"

	"gorm.io/gorm"
)

// MonitoringRecord represents a system monitoring record saved to database
type MonitoringRecord struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`

	// CPU metrics
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	CPUCores        int     `json:"cpu_cores"`

	// Memory metrics
	MemoryTotal       uint64  `json:"memory_total"`
	MemoryUsed        uint64  `json:"memory_used"`
	MemoryAvailable   uint64  `json:"memory_available"`
	MemoryUsedPercent float64 `json:"memory_used_percent"`

	// Disk metrics
	DiskTotal       uint64  `json:"disk_total"`
	DiskUsed        uint64  `json:"disk_used"`
	DiskFree        uint64  `json:"disk_free"`
	DiskUsedPercent float64 `json:"disk_used_percent"`

	// Network metrics
	NetworkBytesSent   uint64  `json:"network_bytes_sent"`
	NetworkBytesRecv   uint64  `json:"network_bytes_recv"`
	NetworkPacketsSent uint64  `json:"network_packets_sent"`
	NetworkPacketsRecv uint64  `json:"network_packets_recv"`
	NetworkSpeedSent   float64 `json:"network_speed_sent"` // bytes per second
	NetworkSpeedRecv   float64 `json:"network_speed_recv"` // bytes per second

	// Host info
	Hostname string `json:"hostname"`
	Uptime   uint64 `json:"uptime"`
}

// BeforeCreate hook to set created_at if not set
func (m *MonitoringRecord) BeforeCreate(tx *gorm.DB) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return nil
}
