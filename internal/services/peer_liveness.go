package services

import (
	"context"
	"log"
	"sync"
	"time"

	"cloud-platform/internal/config"
	"cloud-platform/internal/models"

	"gorm.io/gorm"
)

// LivenessState 设备在线状态
type LivenessState string

const (
	// LivenessUnknown 尚无结论（刚启动，或判定次数未达阈值）
	LivenessUnknown LivenessState = "unknown"
	LivenessOnline  LivenessState = "online"
	LivenessOffline LivenessState = "offline"
)

// LivenessResult 单个设备的在线判定结论
type LivenessResult struct {
	State LivenessState `json:"state"`
	// LastHandshakeAt 最近一次握手时间，直接取自 WireGuard 自身状态
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
	// HandshakeAgeSeconds 距最近一次握手的秒数；从未握手为 -1
	HandshakeAgeSeconds int64      `json:"handshake_age_seconds"`
	LastOnlineAt        *time.Time `json:"last_online_at,omitempty"`
	CheckedAt           time.Time  `json:"checked_at"`
	// Failures 当前连续判定为「握手过期」的次数
	Failures int   `json:"failures"`
	Checks   int64 `json:"checks"`
}

// LivenessMonitor 周期性读取各账号的 WireGuard 握手状态，判定设备是否在线。
//
// 判据说明：peer 的 last handshake 由 WireGuard 自身维护，只要隧道活跃就会
// 持续更新（默认约 120 秒重协商，配合 keepalive 更频繁）。相比主动发包探测，
// 它不受客户端防火墙影响，也不依赖后端进程能路由到隧道网段。
type LivenessMonitor struct {
	db *gorm.DB
	wg *WireguardService

	interval         time.Duration
	handshakeTimeout time.Duration
	offlineAfter     int

	mu      sync.RWMutex
	results map[string]*LivenessResult

	ctx    context.Context
	cancel context.CancelFunc
	done   sync.WaitGroup
}

// NewLivenessMonitor 创建在线判定监控器。
func NewLivenessMonitor(db *gorm.DB, cfg config.LivenessConfig) *LivenessMonitor {
	return &LivenessMonitor{
		db:               db,
		wg:               NewWireguardService(config.AppConfig.Network.ConfigDir),
		interval:         cfg.Interval(),
		handshakeTimeout: cfg.HandshakeTimeout(),
		offlineAfter:     cfg.OfflineThreshold,
		results:          make(map[string]*LivenessResult),
	}
}

// Start 启动判定循环。
func (m *LivenessMonitor) Start(parent context.Context) {
	m.ctx, m.cancel = context.WithCancel(parent)

	m.done.Add(1)
	go func() {
		defer m.done.Done()

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		m.checkAll()

		for {
			select {
			case <-ticker.C:
				// 判定参数支持运行时调整：每轮复核前重新读取
				if interval := m.currentInterval(); interval != m.interval {
					m.interval = interval
					ticker.Reset(interval)
					log.Printf("Liveness monitor interval updated to %v", interval)
				}
				m.checkAll()
			case <-m.ctx.Done():
				return
			}
		}
	}()

	log.Printf("Liveness monitor started: interval=%v handshake_timeout=%v offline_after=%d",
		m.interval, m.handshakeTimeout, m.offlineAfter)
}

// Stop 停止判定循环。
func (m *LivenessMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.done.Wait()
}

// ProbeEnabled 判定功能是否处于运行状态。
func (m *LivenessMonitor) ProbeEnabled() bool {
	return m.ctx != nil
}

// currentInterval 取运行时配置中的复核间隔（未配置时用启动时的值）。
func (m *LivenessMonitor) currentInterval() time.Duration {
	seconds := GetSettings().Int(SettingLivenessInterval, int(m.interval/time.Second))
	if seconds < 1 {
		seconds = 3
	}
	return time.Duration(seconds) * time.Second
}

// handshakeWindow 取运行时配置中的握手时效阈值。
func (m *LivenessMonitor) handshakeWindow() time.Duration {
	seconds := GetSettings().Int(SettingLivenessHandshakeTimeout, int(m.handshakeTimeout/time.Second))
	if seconds < 1 {
		seconds = 180
	}
	return time.Duration(seconds) * time.Second
}

// offlineConfirmations 取运行时配置中的离线确认次数。
func (m *LivenessMonitor) offlineConfirmations() int {
	count := GetSettings().Int(SettingLivenessOfflineThreshold, m.offlineAfter)
	if count < 1 {
		count = 2
	}
	return count
}

// checkAll 遍历所有账号，读取握手状态并更新判定结果。
func (m *LivenessMonitor) checkAll() {
	var servers []models.WireguardServer
	if err := m.db.Find(&servers).Error; err != nil {
		log.Printf("liveness: failed to list wireguard servers: %v", err)
		return
	}

	now := time.Now()

	for _, server := range servers {
		var peers []models.WireguardPeer
		if err := m.db.Select("public_key").
			Where("server_id = ?", server.ID).Find(&peers).Error; err != nil {
			log.Printf("liveness: failed to list peers of server %d: %v", server.ID, err)
			continue
		}
		if len(peers) == 0 {
			continue
		}

		handshakes := make(map[string]time.Time, len(peers))
		statsOK := false

		stats, err := m.wg.GetDetailedStats(server.Namespace, server.WgInterface)
		if err != nil {
			// 接口不可用（账号被禁用、网络未就绪）：本轮不出结论，保留上一状态
			log.Printf("liveness: stats unavailable for %s/%s: %v", server.Namespace, server.WgInterface, err)
		} else {
			statsOK = true
			for _, peerStats := range stats.Peers {
				handshakes[peerStats.PublicKey] = peerStats.LatestHandshake
			}
		}

		for _, peer := range peers {
			m.evaluate(peer.PublicKey, handshakes[peer.PublicKey], statsOK, now)
		}
	}
}

// evaluate 依据握手时间更新单个设备的状态。
//
// 上线：握手时间在阈值内，立即置为在线（秒级）。
// 离线：从未握手直接判离线；握手过期则需连续多次确认，规避临界抖动。
func (m *LivenessMonitor) evaluate(key string, handshake time.Time, statsOK bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[key]
	if !ok {
		result = &LivenessResult{State: LivenessUnknown}
		m.results[key] = result
	}

	result.CheckedAt = now
	result.Checks++

	if handshake.IsZero() {
		result.LastHandshakeAt = nil
		result.HandshakeAgeSeconds = -1
	} else {
		handshakeTime := handshake
		result.LastHandshakeAt = &handshakeTime
		result.HandshakeAgeSeconds = int64(now.Sub(handshake).Seconds())
	}

	// 接口读取失败时不改变既有结论，避免账号禁用期间状态跳变
	if !statsOK {
		return
	}

	if !handshake.IsZero() && now.Sub(handshake) <= m.handshakeWindow() {
		result.State = LivenessOnline
		result.Failures = 0
		result.LastOnlineAt = &now
		return
	}

	// 从未握手：设备从未接入，直接判离线
	if handshake.IsZero() {
		result.State = LivenessOffline
		result.Failures++
		return
	}

	result.Failures++
	if result.Failures >= m.offlineConfirmations() {
		result.State = LivenessOffline
	}
}

// Snapshot 返回全部设备的判定结果副本。
func (m *LivenessMonitor) Snapshot() map[string]LivenessResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]LivenessResult, len(m.results))
	for key, result := range m.results {
		out[key] = *result
	}
	return out
}

// Result 返回指定设备的判定结果。
func (m *LivenessMonitor) Result(key string) (LivenessResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result, ok := m.results[key]
	if !ok {
		return LivenessResult{}, false
	}
	return *result, true
}

// Counts 返回 (在线数, 离线数, 未知数)。
func (m *LivenessMonitor) Counts() (online, offline, unknown int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, result := range m.results {
		switch result.State {
		case LivenessOnline:
			online++
		case LivenessOffline:
			offline++
		default:
			unknown++
		}
	}
	return online, offline, unknown
}
