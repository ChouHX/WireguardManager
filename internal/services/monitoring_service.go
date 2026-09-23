package services

import (
	"cloud-platform/internal/models"
	"context"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

// MonitoringService handles background monitoring data collection
type MonitoringService struct {
	db       *gorm.DB
	interval time.Duration

	// ctx 是服务级上下文，由 Stop 取消；两个后台循环都 select 它。
	// 在构造时同步创建，避免「Start 在协程里赋值 cancel、Stop 同时读取」的竞争，
	// 也保证 Stop 时 cancel 必定非 nil —— 否则 wg.Wait() 会永久阻塞、卡死关闭流程。
	ctx    context.Context
	cancel context.CancelFunc

	// wg 跟踪 StartBackground 启动的两个后台循环，供 Stop 等待收尾
	wg sync.WaitGroup
}

// NewMonitoringService creates a new monitoring service
func NewMonitoringService(ctx context.Context, db *gorm.DB, interval time.Duration) *MonitoringService {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	return &MonitoringService{
		db:       db,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// StartBackground 启动两个后台循环并登记等待计数。
//
// 计数必须在这里、也就是协程启动之前同步完成。若把 wg.Add 放进循环体内，
// Stop 有可能先执行到 wg.Wait 并因计数为 0 立即返回，而循环随后才真正开始运行——
// 结果是关闭流程结束后仍有协程在写数据库。
func (s *MonitoringService) StartBackground(cleanupInterval, retention time.Duration) {
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.startLoop()
	}()
	go func() {
		defer s.wg.Done()
		s.runCleanupLoop(cleanupInterval, retention)
	}()
}

// startLoop 启动周期性落库循环，服务上下文取消后退出。
func (s *MonitoringService) startLoop() {
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
		case <-s.ctx.Done():
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
	// 必须等采样与清理循环真正退出后再返回：它们的落库动作会写数据库，
	// 若只是取消 context 就返回，调用方可能在协程仍在写库时关闭数据库连接。
	s.wg.Wait()
}

// runCleanupLoop 周期清理过期监控记录，直到服务上下文取消。
func (s *MonitoringService) runCleanupLoop(interval, retention time.Duration) {
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
		case <-s.ctx.Done():
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
