package services

import (
	"cloud-platform/internal/models"
	"context"
	"log"
	"time"

	"gorm.io/gorm"
)

// MonitoringService handles background monitoring data collection
type MonitoringService struct {
	db       *gorm.DB
	interval time.Duration
	cancel   context.CancelFunc
}

// NewMonitoringService creates a new monitoring service
func NewMonitoringService(db *gorm.DB, interval time.Duration) *MonitoringService {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &MonitoringService{
		db:       db,
		interval: interval,
	}
}

// Start 启动周期性落库循环，ctx 取消后退出。
func (s *MonitoringService) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	s.cancel = cancel
	defer cancel()

	log.Printf("Starting monitoring service with interval: %v", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Collect initial data immediately
	s.collectAndSave()

	for {
		select {
		case <-ticker.C:
			// 采样间隔支持运行时调整：每轮检查一次，变化时重建 ticker
			if interval := runtimeMonitoringInterval(s.interval); interval != s.interval {
				s.interval = interval
				ticker.Reset(interval)
				log.Printf("Monitoring service interval updated to %v", interval)
			}
			s.collectAndSave()
		case <-ctx.Done():
			log.Println("Monitoring service stopped")
			return
		}
	}
}

// Stop stops the monitoring service
func (s *MonitoringService) Stop() {
	log.Println("Stopping monitoring service...")
	if s.cancel != nil {
		s.cancel()
	}
}

// RunCleanupLoop 周期清理过期监控记录，直到 ctx 取消。
func (s *MonitoringService) RunCleanupLoop(ctx context.Context, interval, retention time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if retention <= 0 {
		retention = 168 * time.Hour
	}

	log.Printf("Monitoring cleanup loop started (every %v, retaining %v)", interval, retention)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Monitoring cleanup loop stopped")
			return
		case <-ticker.C:
			if err := s.CleanupOldRecords(runtimeMonitoringRetention(retention)); err != nil {
				log.Printf("Failed to clean up old monitoring records: %v", err)
			}
		}
	}
}

// collectAndSave 复用采集器快照落库，不再重复调用整套 gopsutil（避免阻塞与重复开销）。
func (s *MonitoringService) collectAndSave() {
	snapshot := GetMetricsCollector().Snapshot()

	record := &models.MonitoringRecord{
		CreatedAt: time.Now(),

		CPUUsagePercent: snapshot.CPU.UsagePercent,
		CPUCores:        snapshot.CPU.Cores,

		MemoryTotal:       snapshot.Memory.Total,
		MemoryUsed:        snapshot.Memory.Used,
		MemoryAvailable:   snapshot.Memory.Available,
		MemoryUsedPercent: snapshot.Memory.UsedPercent,

		DiskTotal:       snapshot.Disk.Total,
		DiskUsed:        snapshot.Disk.Used,
		DiskFree:        snapshot.Disk.Free,
		DiskUsedPercent: snapshot.Disk.UsedPercent,

		NetworkBytesSent:   snapshot.Network.BytesSent,
		NetworkBytesRecv:   snapshot.Network.BytesRecv,
		NetworkPacketsSent: snapshot.Network.PacketsSent,
		NetworkPacketsRecv: snapshot.Network.PacketsRecv,
		NetworkSpeedSent:   snapshot.Network.SpeedSent,
		NetworkSpeedRecv:   snapshot.Network.SpeedRecv,

		Hostname: snapshot.Host.Hostname,
		Uptime:   snapshot.Host.Uptime,
	}

	if err := s.db.Create(record).Error; err != nil {
		log.Printf("Failed to save monitoring record: %v", err)
		return
	}

	log.Printf("Monitoring record saved: CPU=%.2f%%, Memory=%.2f%%, Disk=%.2f%%",
		record.CPUUsagePercent, record.MemoryUsedPercent, record.DiskUsedPercent)
}

// runtimeMonitoringInterval 取运行时配置中的采样间隔。
func runtimeMonitoringInterval(fallback time.Duration) time.Duration {
	seconds := GetSettings().Int(SettingMonitoringInterval, int(fallback/time.Second))
	if seconds < 1 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

// runtimeMonitoringRetention 取运行时配置中的记录保留时长。
func runtimeMonitoringRetention(fallback time.Duration) time.Duration {
	hours := GetSettings().Int(SettingMonitoringRetention, int(fallback/time.Hour))
	if hours < 1 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}

// GetRecentRecords retrieves recent monitoring records
func (s *MonitoringService) GetRecentRecords(limit int, since time.Time) ([]models.MonitoringRecord, error) {
	var records []models.MonitoringRecord
	query := s.db.Order("created_at DESC")

	if !since.IsZero() {
		query = query.Where("created_at >= ?", since)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&records).Error
	return records, err
}

// CleanupOldRecords removes monitoring records older than the specified duration
func (s *MonitoringService) CleanupOldRecords(olderThan time.Duration) error {
	cutoffTime := time.Now().Add(-olderThan)
	result := s.db.Where("created_at < ?", cutoffTime).Delete(&models.MonitoringRecord{})

	if result.Error != nil {
		return result.Error
	}

	log.Printf("Cleaned up %d old monitoring records (older than %v)", result.RowsAffected, olderThan)
	return nil
}
