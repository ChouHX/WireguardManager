package handlers

import (
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// 类型别名：对外类型名与 JSON 字段名保持不变（前端契约冻结），
// 数据来源改为 MetricsCollector 的内存快照，读路径不再阻塞。
type (
	SystemStats  = services.SystemSnapshot
	CPUStats     = services.CPUSnapshot
	MemoryStats  = services.MemorySnapshot
	DiskStats    = services.DiskSnapshot
	NetworkStats = services.NetworkSnapshot
	HostStats    = services.HostSnapshot
)

// GetSystemStats returns current system statistics
func GetSystemStats(c *gin.Context) {
	stats, ok := currentSnapshot(c)
	if !ok {
		return
	}

	response.Success(c, "System statistics retrieved successfully", stats)
}

// GetCPUStats returns detailed CPU statistics
func GetCPUStats(c *gin.Context) {
	stats, ok := currentSnapshot(c)
	if !ok {
		return
	}

	response.Success(c, "CPU statistics retrieved successfully", stats.CPU)
}

// GetMemoryStats returns memory statistics
func GetMemoryStats(c *gin.Context) {
	stats, ok := currentSnapshot(c)
	if !ok {
		return
	}

	response.Success(c, "Memory statistics retrieved successfully", stats.Memory)
}

// GetDiskStats returns disk statistics
func GetDiskStats(c *gin.Context) {
	stats, ok := currentSnapshot(c)
	if !ok {
		return
	}

	response.Success(c, "Disk statistics retrieved successfully", stats.Disk)
}

// GetNetworkStats returns network statistics
func GetNetworkStats(c *gin.Context) {
	stats, ok := currentSnapshot(c)
	if !ok {
		return
	}

	response.Success(c, "Network statistics retrieved successfully", stats.Network)
}

// currentSnapshot 读取采集器快照；采集器尚未就绪时返回 false 并已写出 503。
func currentSnapshot(c *gin.Context) (SystemStats, bool) {
	collector := services.GetMetricsCollector()
	if !collector.Ready() {
		response.ServiceUnavailable(c, "System metrics are not available yet, please retry shortly")
		return SystemStats{}, false
	}
	return collector.Snapshot(), true
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

	records := make([]models.MonitoringRecord, 0, limit)
	query := database.DB.Order("created_at DESC").Limit(limit).Where("created_at >= ?", since)

	if err := query.Find(&records).Error; err != nil {
		response.InternalError(c, "Failed to retrieve monitoring history: "+err.Error())
		return
	}

	response.Success(c, "Monitoring history retrieved successfully", gin.H{
		"records": records,
		"count":   len(records),
	})
}

// ChartDataPoint represents a simplified data point for chart rendering
type ChartDataPoint struct {
	Timestamp        int64   `json:"timestamp"`          // Unix timestamp
	CPUPercent       float64 `json:"cpu_percent"`        // CPU usage percentage
	MemoryPercent    float64 `json:"memory_percent"`     // Memory usage percentage
	DiskPercent      float64 `json:"disk_percent"`       // Disk usage percentage
	NetworkSpeedSent float64 `json:"network_speed_sent"` // Network upload speed (bytes/s)
	NetworkSpeedRecv float64 `json:"network_speed_recv"` // Network download speed (bytes/s)
}

// 图表返回的最大点数：按小时范围动态选桶，保证前端渲染量可控
const maxChartPoints = 720

// 候选时间桶（秒），从细到粗；挑选出的桶需让总点数不超过 maxChartPoints
var chartBucketSeconds = []int64{1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400}

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
		response.InternalError(c, "Failed to retrieve monitoring data: "+err.Error())
		return
	}

	points := downsampleRecords(records, hours)

	response.Success(c, "Chart data retrieved successfully", gin.H{
		"points": points,
		"count":  len(points),
		"period": gin.H{
			"hours": hours,
			"from":  since.Unix(),
			"to":    time.Now().Unix(),
		},
	})
}

// downsampleRecords 把原始记录按时间桶聚合成图表点：CPU/内存/磁盘/网络速率取桶内平均，
// 时间戳取桶起点，总点数不超过 maxChartPoints。
func downsampleRecords(records []models.MonitoringRecord, hours float64) []ChartDataPoint {
	if len(records) == 0 {
		return []ChartDataPoint{}
	}

	bucket := pickBucketSeconds(hours, records)

	type bucketAcc struct {
		ts                                      int64
		count                                   int64
		cpu, memory, disk, speedSent, speedRecv float64
	}

	// records 已按 created_at ASC 排序，桶键单调递增，用切片保序即可，无需 map 排序
	accumulators := make([]*bucketAcc, 0, len(records))
	indexByBucket := make(map[int64]int, len(records))

	for _, record := range records {
		ts := time.Time(record.CreatedAt).Unix()
		bucketTS := ts - ts%bucket

		idx, ok := indexByBucket[bucketTS]
		if !ok {
			accumulators = append(accumulators, &bucketAcc{ts: bucketTS})
			idx = len(accumulators) - 1
			indexByBucket[bucketTS] = idx
		}

		acc := accumulators[idx]
		acc.count++
		acc.cpu += record.CPUUsagePercent
		acc.memory += record.MemoryUsedPercent
		acc.disk += record.DiskUsedPercent
		acc.speedSent += record.NetworkSpeedSent
		acc.speedRecv += record.NetworkSpeedRecv
	}

	points := make([]ChartDataPoint, 0, len(accumulators))
	for _, acc := range accumulators {
		if acc.count == 0 {
			continue
		}
		divisor := float64(acc.count)
		points = append(points, ChartDataPoint{
			Timestamp:        acc.ts,
			CPUPercent:       acc.cpu / divisor,
			MemoryPercent:    acc.memory / divisor,
			DiskPercent:      acc.disk / divisor,
			NetworkSpeedSent: acc.speedSent / divisor,
			NetworkSpeedRecv: acc.speedRecv / divisor,
		})
	}

	return points
}

// pickBucketSeconds 依据查询范围与记录实际跨度选择时间桶大小，保证聚合后的点数不超过上限。
// 采样间隔可配置，仅按 hours 选桶在采样更密时会突破上限，因此同时参考记录的真实时间跨度。
func pickBucketSeconds(hours float64, records []models.MonitoringRecord) int64 {
	windowSeconds := int64(hours * 3600)
	if windowSeconds <= 0 {
		windowSeconds = 1
	}

	if len(records) > 1 {
		first := time.Time(records[0].CreatedAt).Unix()
		last := time.Time(records[len(records)-1].CreatedAt).Unix()
		if span := last - first; span > windowSeconds {
			windowSeconds = span
		}
	}

	for _, candidate := range chartBucketSeconds {
		if windowSeconds/candidate <= maxChartPoints {
			return candidate
		}
	}

	// 窗口超过候选表覆盖范围时，按窗口均分为 maxChartPoints 个桶
	return (windowSeconds + maxChartPoints - 1) / maxChartPoints
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
		response.InternalError(c, "Failed to retrieve monitoring statistics: "+err.Error())
		return
	}

	if len(records) == 0 {
		response.Success(c, "No monitoring data available for the specified time range", gin.H{
			"records": []models.MonitoringRecord{},
			"stats": gin.H{
				"count": 0,
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

	response.Success(c, "Monitoring statistics retrieved successfully", gin.H{
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
	})
}
