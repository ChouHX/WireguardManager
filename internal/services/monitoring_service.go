package services

import (
	"cloud-platform/internal/models"
	"context"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"gorm.io/gorm"
)

// MonitoringService handles background monitoring data collection
type MonitoringService struct {
	db            *gorm.DB
	interval      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	prevNetStats  *net.IOCountersStat
	prevStatsTime time.Time
}

// NewMonitoringService creates a new monitoring service
func NewMonitoringService(db *gorm.DB, interval time.Duration) *MonitoringService {
	ctx, cancel := context.WithCancel(context.Background())
	return &MonitoringService{
		db:       db,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the monitoring data collection loop
func (s *MonitoringService) Start() {
	log.Printf("Starting monitoring service with interval: %v", s.interval)
	
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Collect initial data immediately
	s.collectAndSave()

	for {
		select {
		case <-ticker.C:
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
	s.cancel()
}

// collectAndSave collects system metrics and saves them to database
func (s *MonitoringService) collectAndSave() {
	record := &models.MonitoringRecord{
		CreatedAt: time.Now(),
	}

	// Collect CPU stats
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		record.CPUUsagePercent = cpuPercent[0]
	}

	cpuCounts, err := cpu.Counts(true)
	if err == nil {
		record.CPUCores = cpuCounts
	}

	// Collect memory stats
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		record.MemoryTotal = memInfo.Total
		record.MemoryUsed = memInfo.Used
		record.MemoryAvailable = memInfo.Available
		record.MemoryUsedPercent = memInfo.UsedPercent
	}

	// Collect disk stats
	diskInfo, err := disk.Usage("/")
	if err == nil {
		record.DiskTotal = diskInfo.Total
		record.DiskUsed = diskInfo.Used
		record.DiskFree = diskInfo.Free
		record.DiskUsedPercent = diskInfo.UsedPercent
	}

	// Collect network stats
	netInfo, err := net.IOCounters(false)
	if err == nil && len(netInfo) > 0 {
		currentStats := &netInfo[0]
		record.NetworkBytesSent = currentStats.BytesSent
		record.NetworkBytesRecv = currentStats.BytesRecv
		record.NetworkPacketsSent = currentStats.PacketsSent
		record.NetworkPacketsRecv = currentStats.PacketsRecv

		// Calculate network speed
		now := time.Now()
		if s.prevNetStats != nil && !s.prevStatsTime.IsZero() {
			timeDiff := now.Sub(s.prevStatsTime).Seconds()
			if timeDiff > 0 {
				record.NetworkSpeedSent = float64(currentStats.BytesSent-s.prevNetStats.BytesSent) / timeDiff
				record.NetworkSpeedRecv = float64(currentStats.BytesRecv-s.prevNetStats.BytesRecv) / timeDiff
			}
		}

		// Update previous stats
		s.prevNetStats = currentStats
		s.prevStatsTime = now
	}

	// Collect host info
	hostInfo, err := host.Info()
	if err == nil {
		record.Hostname = hostInfo.Hostname
		record.Uptime = hostInfo.Uptime
	}

	// Save to database
	if err := s.db.Create(record).Error; err != nil {
		log.Printf("Failed to save monitoring record: %v", err)
		return
	}

	log.Printf("Monitoring record saved: CPU=%.2f%%, Memory=%.2f%%, Disk=%.2f%%",
		record.CPUUsagePercent, record.MemoryUsedPercent, record.DiskUsedPercent)
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
