package handlers

import (
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// SystemStats represents the overall system statistics
type SystemStats struct {
	CPU     CPUStats     `json:"cpu"`
	Memory  MemoryStats  `json:"memory"`
	Disk    DiskStats    `json:"disk"`
	Network NetworkStats `json:"network"`
	Host    HostStats    `json:"host"`
}

// CPUStats represents CPU usage statistics
type CPUStats struct {
	UsagePercent float64   `json:"usage_percent"`
	Cores        int       `json:"cores"`
	PerCore      []float64 `json:"per_core"`
}

// MemoryStats represents memory usage statistics
type MemoryStats struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsedPercent float64 `json:"used_percent"`
}

// DiskStats represents disk usage statistics
type DiskStats struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

// NetworkStats represents network statistics
type NetworkStats struct {
	BytesSent   uint64  `json:"bytes_sent"`
	BytesRecv   uint64  `json:"bytes_recv"`
	PacketsSent uint64  `json:"packets_sent"`
	PacketsRecv uint64  `json:"packets_recv"`
	SpeedSent   float64 `json:"speed_sent"`   // bytes per second
	SpeedRecv   float64 `json:"speed_recv"`   // bytes per second
}

// HostStats represents host information
type HostStats struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	Uptime          uint64 `json:"uptime"`
	BootTime        uint64 `json:"boot_time"`
}

// Store previous network stats for speed calculation
var (
	prevNetStats     *net.IOCountersStat
	prevNetStatsTime time.Time
)

// GetSystemStats returns current system statistics
func GetSystemStats(c *gin.Context) {
	stats := SystemStats{}

	// Get CPU stats
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.CPU.UsagePercent = cpuPercent[0]
	}

	cpuCounts, err := cpu.Counts(true)
	if err == nil {
		stats.CPU.Cores = cpuCounts
	}

	cpuPerCore, err := cpu.Percent(time.Second, true)
	if err == nil {
		stats.CPU.PerCore = cpuPerCore
	}

	// Get memory stats
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		stats.Memory.Total = memInfo.Total
		stats.Memory.Used = memInfo.Used
		stats.Memory.Available = memInfo.Available
		stats.Memory.UsedPercent = memInfo.UsedPercent
	}

	// Get disk stats (root partition)
	diskInfo, err := disk.Usage("/")
	if err == nil {
		stats.Disk.Total = diskInfo.Total
		stats.Disk.Used = diskInfo.Used
		stats.Disk.Free = diskInfo.Free
		stats.Disk.UsedPercent = diskInfo.UsedPercent
	}

	// Get network stats
	netInfo, err := net.IOCounters(false)
	if err == nil && len(netInfo) > 0 {
		currentStats := &netInfo[0]
		stats.Network.BytesSent = currentStats.BytesSent
		stats.Network.BytesRecv = currentStats.BytesRecv
		stats.Network.PacketsSent = currentStats.PacketsSent
		stats.Network.PacketsRecv = currentStats.PacketsRecv

		// Calculate network speed
		now := time.Now()
		if prevNetStats != nil && !prevNetStatsTime.IsZero() {
			timeDiff := now.Sub(prevNetStatsTime).Seconds()
			if timeDiff > 0 {
				stats.Network.SpeedSent = float64(currentStats.BytesSent-prevNetStats.BytesSent) / timeDiff
				stats.Network.SpeedRecv = float64(currentStats.BytesRecv-prevNetStats.BytesRecv) / timeDiff
			}
		}

		// Update previous stats
		prevNetStats = currentStats
		prevNetStatsTime = now
	}

	// Get host info
	hostInfo, err := host.Info()
	if err == nil {
		stats.Host.Hostname = hostInfo.Hostname
		stats.Host.OS = hostInfo.OS
		stats.Host.Platform = hostInfo.Platform
		stats.Host.PlatformVersion = hostInfo.PlatformVersion
		stats.Host.Uptime = hostInfo.Uptime
		stats.Host.BootTime = hostInfo.BootTime
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "System statistics retrieved successfully",
		"data":    stats,
	})
}

// GetCPUStats returns detailed CPU statistics
func GetCPUStats(c *gin.Context) {
	stats := CPUStats{}

	cpuPercent, err := cpu.Percent(time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.UsagePercent = cpuPercent[0]
	}

	cpuCounts, err := cpu.Counts(true)
	if err == nil {
		stats.Cores = cpuCounts
	}

	cpuPerCore, err := cpu.Percent(time.Second, true)
	if err == nil {
		stats.PerCore = cpuPerCore
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "CPU statistics retrieved successfully",
		"data":    stats,
	})
}

// GetMemoryStats returns memory statistics
func GetMemoryStats(c *gin.Context) {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve memory statistics",
			"error":   err.Error(),
		})
		return
	}

	stats := MemoryStats{
		Total:       memInfo.Total,
		Used:        memInfo.Used,
		Available:   memInfo.Available,
		UsedPercent: memInfo.UsedPercent,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Memory statistics retrieved successfully",
		"data":    stats,
	})
}

// GetDiskStats returns disk statistics
func GetDiskStats(c *gin.Context) {
	diskInfo, err := disk.Usage("/")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve disk statistics",
			"error":   err.Error(),
		})
		return
	}

	stats := DiskStats{
		Total:       diskInfo.Total,
		Used:        diskInfo.Used,
		Free:        diskInfo.Free,
		UsedPercent: diskInfo.UsedPercent,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Disk statistics retrieved successfully",
		"data":    stats,
	})
}

// GetNetworkStats returns network statistics
func GetNetworkStats(c *gin.Context) {
	netInfo, err := net.IOCounters(false)
	if err != nil || len(netInfo) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve network statistics",
			"error":   err.Error(),
		})
		return
	}

	currentStats := &netInfo[0]
	stats := NetworkStats{
		BytesSent:   currentStats.BytesSent,
		BytesRecv:   currentStats.BytesRecv,
		PacketsSent: currentStats.PacketsSent,
		PacketsRecv: currentStats.PacketsRecv,
	}

	// Calculate network speed
	now := time.Now()
	if prevNetStats != nil && !prevNetStatsTime.IsZero() {
		timeDiff := now.Sub(prevNetStatsTime).Seconds()
		if timeDiff > 0 {
			stats.SpeedSent = float64(currentStats.BytesSent-prevNetStats.BytesSent) / timeDiff
			stats.SpeedRecv = float64(currentStats.BytesRecv-prevNetStats.BytesRecv) / timeDiff
		}
	}

	// Update previous stats
	prevNetStats = currentStats
	prevNetStatsTime = now

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Network statistics retrieved successfully",
		"data":    stats,
	})
}

// GetMonitoringHistory returns historical monitoring records
func GetMonitoringHistory(c *gin.Context) {
	// Parse query parameters
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000 // Cap at 1000 records
	}

	// Parse time range
	var since time.Time
	sinceStr := c.Query("since")
	if sinceStr != "" {
		sinceUnix, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			since = time.Unix(sinceUnix, 0)
		}
	}

	// Default to last 24 hours if no since parameter
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}

	var records []models.MonitoringRecord
	query := database.DB.Order("created_at DESC").Limit(limit)
	
	if !since.IsZero() {
		query = query.Where("created_at >= ?", since)
	}
	
	if err := query.Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve monitoring history",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Monitoring history retrieved successfully",
		"data": gin.H{
			"records": records,
			"count":   len(records),
		},
	})
}

// ChartDataPoint represents a simplified data point for chart rendering
type ChartDataPoint struct {
	Timestamp        int64   `json:"timestamp"`         // Unix timestamp
	CPUPercent       float64 `json:"cpu_percent"`       // CPU usage percentage
	MemoryPercent    float64 `json:"memory_percent"`    // Memory usage percentage
	DiskPercent      float64 `json:"disk_percent"`      // Disk usage percentage
	NetworkSpeedSent float64 `json:"network_speed_sent"` // Network upload speed (bytes/s)
	NetworkSpeedRecv float64 `json:"network_speed_recv"` // Network download speed (bytes/s)
}

// GetMonitoringChart returns simplified monitoring data for chart rendering
func GetMonitoringChart(c *gin.Context) {
	// Parse time range
	hoursStr := c.DefaultQuery("hours", "0.5")
	hours, err := strconv.ParseFloat(hoursStr, 64)
	if err != nil || hours <= 0 {
		hours = 0.5
	}
	if hours > 168 { // Cap at 1 week
		hours = 168
	}

	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))

	var records []models.MonitoringRecord
	if err := database.DB.Where("created_at >= ?", since).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve monitoring data",
			"error":   err.Error(),
		})
		return
	}

	// Convert to chart data points
	chartData := make([]ChartDataPoint, len(records))
	for i, record := range records {
		chartData[i] = ChartDataPoint{
			Timestamp:        time.Time(record.CreatedAt).Unix(),
			CPUPercent:       record.CPUUsagePercent,
			MemoryPercent:    record.MemoryUsedPercent,
			DiskPercent:      record.DiskUsedPercent,
			NetworkSpeedSent: record.NetworkSpeedSent,
			NetworkSpeedRecv: record.NetworkSpeedRecv,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Chart data retrieved successfully",
		"data": gin.H{
			"points": chartData,
			"count":  len(chartData),
			"period": gin.H{
				"hours": hours,
				"from":  since.Unix(),
				"to":    time.Now().Unix(),
			},
		},
	})
}

// GetMonitoringStats returns aggregated monitoring statistics
func GetMonitoringStats(c *gin.Context) {
	// Parse time range
	hoursStr := c.DefaultQuery("hours", "1")
	hours, err := strconv.Atoi(hoursStr)
	if err != nil || hours <= 0 {
		hours = 1
	}
	if hours > 168 { // Cap at 1 week
		hours = 168
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	var records []models.MonitoringRecord
	if err := database.DB.Where("created_at >= ?", since).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to retrieve monitoring statistics",
			"error":   err.Error(),
		})
		return
	}

	if len(records) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "No monitoring data available for the specified time range",
			"data": gin.H{
				"records": []models.MonitoringRecord{},
				"stats": gin.H{
					"count": 0,
				},
			},
		})
		return
	}

	// Calculate statistics
	var (
		totalCPU    float64
		totalMemory float64
		totalDisk   float64
		maxCPU      float64
		maxMemory   float64
		maxDisk     float64
	)

	for _, record := range records {
		totalCPU += record.CPUUsagePercent
		totalMemory += record.MemoryUsedPercent
		totalDisk += record.DiskUsedPercent

		if record.CPUUsagePercent > maxCPU {
			maxCPU = record.CPUUsagePercent
		}
		if record.MemoryUsedPercent > maxMemory {
			maxMemory = record.MemoryUsedPercent
		}
		if record.DiskUsedPercent > maxDisk {
			maxDisk = record.DiskUsedPercent
		}
	}

	count := float64(len(records))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Monitoring statistics retrieved successfully",
		"data": gin.H{
			"records": records,
			"stats": gin.H{
				"count": len(records),
				"period": gin.H{
					"hours": hours,
					"from":  since.Unix(),
					"to":    time.Now().Unix(),
				},
				"cpu": gin.H{
					"average": totalCPU / count,
					"max":     maxCPU,
				},
				"memory": gin.H{
					"average": totalMemory / count,
					"max":     maxMemory,
				},
				"disk": gin.H{
					"average": totalDisk / count,
					"max":     maxDisk,
				},
			},
		},
	})
}
