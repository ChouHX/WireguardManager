package services

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// 快照结构（JSON 字段名与前端契约保持一致：cpu/memory/disk/network/host）
type CPUSnapshot struct {
	UsagePercent float64   `json:"usage_percent"`
	Cores        int       `json:"cores"`
	PerCore      []float64 `json:"per_core"`
}

type MemorySnapshot struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskSnapshot struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

type NetworkSnapshot struct {
	BytesSent   uint64  `json:"bytes_sent"`
	BytesRecv   uint64  `json:"bytes_recv"`
	PacketsSent uint64  `json:"packets_sent"`
	PacketsRecv uint64  `json:"packets_recv"`
	SpeedSent   float64 `json:"speed_sent"` // bytes per second
	SpeedRecv   float64 `json:"speed_recv"` // bytes per second
}

type HostSnapshot struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	Uptime          uint64 `json:"uptime"`
	BootTime        uint64 `json:"boot_time"`
}

// SystemSnapshot 是某一时刻的完整系统指标快照
type SystemSnapshot struct {
	CPU     CPUSnapshot     `json:"cpu"`
	Memory  MemorySnapshot  `json:"memory"`
	Disk    DiskSnapshot    `json:"disk"`
	Network NetworkSnapshot `json:"network"`
	Host    HostSnapshot    `json:"host"`
}

// MetricsCollector 在后台按固定间隔采样系统指标并缓存到内存。
// 读路径（Snapshot/Ready）只做一次加锁拷贝，零阻塞、零 gopsutil 调用。
type MetricsCollector struct {
	interval time.Duration
	cores    int

	mu       sync.RWMutex
	snapshot SystemSnapshot
	ready    bool

	// 以下字段仅由采样 goroutine 读写
	prevNetBytesSent uint64
	prevNetBytesRecv uint64
	prevNetAt        time.Time
	netBaseline      bool

	cancel   context.CancelFunc
	stopOnce sync.Once
}

// NewMetricsCollector 创建采集器，并在返回前同步完成第一次采集，
// 使得调用方在 Start 之前就有一个可用的快照。
func NewMetricsCollector(interval time.Duration) *MetricsCollector {
	if interval <= 0 {
		interval = 10 * time.Second
	}

	collector := &MetricsCollector{
		interval: interval,
		cores:    detectCPUCores(),
	}
	collector.collect()

	return collector
}

// Start 启动后台采样循环，ctx 取消后退出。
func (m *MetricsCollector) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	log.Printf("Metrics collector started with interval %v", m.interval)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			log.Println("Metrics collector stopped")
			return
		case <-ticker.C:
			m.collect()
		}
	}
}

// Stop 停止采样循环，可重复调用。
func (m *MetricsCollector) Stop() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		cancel := m.cancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
}

// Snapshot 返回当前快照的副本（含 per-core 切片的深拷贝，调用方可安全改写）。
func (m *MetricsCollector) Snapshot() SystemSnapshot {
	m.mu.RLock()
	snap := m.snapshot
	m.mu.RUnlock()

	snap.CPU.PerCore = append([]float64(nil), snap.CPU.PerCore...)
	return snap
}

// Ready 表示至少完成过一次采集。
func (m *MetricsCollector) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready
}

// collect 采样一次并覆盖快照。任一子系统失败时沿用上一次的值，避免前端看到跳变的 0。
func (m *MetricsCollector) collect() {
	m.mu.RLock()
	snap := m.snapshot
	m.mu.RUnlock()
	snap.CPU.PerCore = append([]float64(nil), snap.CPU.PerCore...)

	// 只要有一个子系统采样成功就认为采集可用（避免单个指标不可用导致整体长期不就绪）
	sampled := false

	// CPU：Percent(0) 返回"自上次调用以来"的使用率，仅在本采样 goroutine 中按固定间隔调用，
	// 因此差值语义正确且完全不阻塞（不会 sleep 一个采样周期）。
	// percpu=false 与 percpu=true 在 gopsutil 内部使用各自的缓存变量，互不干扰。
	if percents, err := cpu.Percent(0, false); err != nil {
		log.Printf("Warning: failed to sample CPU usage: %v", err)
	} else if len(percents) > 0 {
		snap.CPU.UsagePercent = percents[0]
		sampled = true
	}
	if perCore, err := cpu.Percent(0, true); err != nil {
		log.Printf("Warning: failed to sample per-core CPU usage: %v", err)
	} else if len(perCore) > 0 {
		snap.CPU.PerCore = perCore
		sampled = true
	}
	snap.CPU.Cores = m.cores

	if memInfo, err := mem.VirtualMemory(); err != nil {
		log.Printf("Warning: failed to sample memory usage: %v", err)
	} else {
		snap.Memory = MemorySnapshot{
			Total:       memInfo.Total,
			Used:        memInfo.Used,
			Available:   memInfo.Available,
			UsedPercent: memInfo.UsedPercent,
		}
		sampled = true
	}

	if diskInfo, err := disk.Usage("/"); err != nil {
		log.Printf("Warning: failed to sample disk usage: %v", err)
	} else {
		snap.Disk = DiskSnapshot{
			Total:       diskInfo.Total,
			Used:        diskInfo.Used,
			Free:        diskInfo.Free,
			UsedPercent: diskInfo.UsedPercent,
		}
		sampled = true
	}

	if netInfo, err := net.IOCounters(false); err != nil {
		log.Printf("Warning: failed to sample network counters: %v", err)
	} else if len(netInfo) > 0 {
		current := netInfo[0]
		now := time.Now()

		// 速率取两次采样的差值，第一次采样只建立基线
		if m.netBaseline {
			if elapsed := now.Sub(m.prevNetAt).Seconds(); elapsed > 0 {
				snap.Network.SpeedSent = byteRate(current.BytesSent, m.prevNetBytesSent, elapsed)
				snap.Network.SpeedRecv = byteRate(current.BytesRecv, m.prevNetBytesRecv, elapsed)
			}
		}

		m.prevNetBytesSent = current.BytesSent
		m.prevNetBytesRecv = current.BytesRecv
		m.prevNetAt = now
		m.netBaseline = true

		snap.Network.BytesSent = current.BytesSent
		snap.Network.BytesRecv = current.BytesRecv
		snap.Network.PacketsSent = current.PacketsSent
		snap.Network.PacketsRecv = current.PacketsRecv
		sampled = true
	}

	if hostInfo, err := host.Info(); err != nil {
		log.Printf("Warning: failed to sample host info: %v", err)
	} else {
		snap.Host = HostSnapshot{
			Hostname:        hostInfo.Hostname,
			OS:              hostInfo.OS,
			Platform:        hostInfo.Platform,
			PlatformVersion: hostInfo.PlatformVersion,
			Uptime:          hostInfo.Uptime,
			BootTime:        hostInfo.BootTime,
		}
		sampled = true
	}

	m.mu.Lock()
	m.snapshot = snap
	if sampled {
		m.ready = true
	}
	m.mu.Unlock()
}

// byteRate 计算字节速率；计数器回绕（当前值变小）时返回 0 而不是巨大的负数。
func byteRate(current, previous uint64, elapsedSeconds float64) float64 {
	if current < previous {
		return 0
	}
	return float64(current-previous) / elapsedSeconds
}

func detectCPUCores() int {
	if cores, err := cpu.Counts(true); err == nil && cores > 0 {
		return cores
	}
	return runtime.NumCPU()
}

// ---- 单例 ----

var (
	collectorMu      sync.Mutex
	defaultCollector *MetricsCollector
)

// InitMetricsCollector 创建并注册进程级采集器单例（在 main 中调用一次）。
func InitMetricsCollector(interval time.Duration) *MetricsCollector {
	collectorMu.Lock()
	defer collectorMu.Unlock()

	defaultCollector = NewMetricsCollector(interval)
	return defaultCollector
}

// GetMetricsCollector 返回进程级采集器单例，未初始化时按默认 10 秒间隔惰性创建。
func GetMetricsCollector() *MetricsCollector {
	collectorMu.Lock()
	defer collectorMu.Unlock()

	if defaultCollector == nil {
		defaultCollector = NewMetricsCollector(10 * time.Second)
	}
	return defaultCollector
}
