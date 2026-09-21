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

// 判定参数由服务端内部决定，不对外暴露为配置项。
//
// 取值依据：
//   - 探测间隔 2s：满足"秒级感知"，同时不会给对端与自身带来可观开销；
//   - 单次超时 1s：小于探测间隔，保证一轮探测不会拖到下一轮；
//   - 离线确认 2 次：约 4 秒完成判定，规避单次丢包造成的误报；
//   - 流量窗口 30s：略大于客户端默认保活间隔（PersistentKeepalive=25s），
//     用于在探测被对端防火墙拦截时，靠对端发来的保活流量继续维持在线
//     （只统计接收方向的流量，避免本机探测包自身造成的误判）；
//   - 并发上限 16：限制同一时刻 fork 的探测数量。
const (
	livenessInterval      = 2 * time.Second
	livenessProbeTimeout  = time.Second
	livenessOfflineAfter  = 2
	livenessTrafficStale  = 30 * time.Second
	livenessMaxConcurrent = 16
)

// LivenessState 设备在线状态
type LivenessState string

const (
	// LivenessUnknown 尚无结论（刚启动，或判定次数未达阈值）
	LivenessUnknown LivenessState = "unknown"
	LivenessOnline  LivenessState = "online"
	LivenessOffline LivenessState = "offline"
)

// 判定依据，便于排查与前端展示
const (
	ReasonProbe     = "probe"     // 主动探测有响应
	ReasonTraffic   = "traffic"   // 隧道内有流量
	ReasonRecent    = "recent"    // 近期有流量（保护窗口内）
	ReasonHandshake = "handshake" // 握手仍在时效内
	ReasonTimeout   = "timeout"   // 探测无响应且无流量
	ReasonNoStats   = "no_stats"  // 无法读取接口统计
)

// LivenessResult 单个设备的在线判定结论
type LivenessResult struct {
	State LivenessState `json:"state"`
	// LastHandshakeAt 最近一次握手时间
	LastHandshakeAt *time.Time `json:"last_handshake_at,omitempty"`
	// HandshakeAgeSeconds 距最近一次握手的秒数；从未握手为 -1
	HandshakeAgeSeconds int64      `json:"handshake_age_seconds"`
	LastOnlineAt        *time.Time `json:"last_online_at,omitempty"`
	CheckedAt           time.Time  `json:"checked_at"`
	// LatencyMS 最近一次主动探测的往返耗时
	LatencyMS int64 `json:"latency_ms"`
	// Reachable 最近一次主动探测是否有响应
	Reachable bool `json:"reachable"`
	// TrafficActive 最近一轮隧道内是否有流量
	TrafficActive bool `json:"traffic_active"`
	// Reason 本次状态的判定依据
	Reason string `json:"reason,omitempty"`
	// Failures 当前连续判为「无响应且无流量」的次数
	Failures int `json:"failures"`
	// NoStats 连续无法读取接口统计的次数
	NoStats int   `json:"no_stats"`
	Checks  int64 `json:"checks"`
}

// trafficSample 上一次采样到的累计流量。
//
// 只使用 rx（本机从该 peer 收到的字节数）判断对端是否活跃：
// tx 是本机发给对端的字节数，会被主动探测本身抬高——每轮探测都会发出
// TCP SYN，tx 因此持续增长，若一并采信则对端离线也会被判为"有流量"。
type trafficSample struct {
	rx int64
	tx int64
}

// LivenessMonitor 周期性地探测各设备：在设备所属命名空间内主动发起 TCP 探测为主，
// 隧道流量与握手状态为辅，实现秒级的在线/离线感知。
type LivenessMonitor struct {
	db *gorm.DB
	wg *WireguardService

	probePort int

	mu              sync.RWMutex
	results         map[string]*LivenessResult
	traffic         map[string]trafficSample
	lastTrafficSeen map[string]time.Time

	ctx    context.Context
	cancel context.CancelFunc
	done   sync.WaitGroup
}

// NewLivenessMonitor 创建在线判定监控器。
func NewLivenessMonitor(db *gorm.DB, cfg config.LivenessConfig) *LivenessMonitor {
	return &LivenessMonitor{
		db:              db,
		wg:              NewWireguardService(config.AppConfig.Network.ConfigDir),
		probePort:       cfg.ProbePort,
		results:         make(map[string]*LivenessResult),
		traffic:         make(map[string]trafficSample),
		lastTrafficSeen: make(map[string]time.Time),
	}
}

// Start 启动判定循环。
func (m *LivenessMonitor) Start(parent context.Context) {
	m.ctx, m.cancel = context.WithCancel(parent)

	m.done.Add(1)
	go func() {
		defer m.done.Done()

		ticker := time.NewTicker(livenessInterval)
		defer ticker.Stop()

		m.checkAll()

		for {
			select {
			case <-ticker.C:
				m.checkAll()
			case <-m.ctx.Done():
				return
			}
		}
	}()

	log.Printf("Liveness monitor started: interval=%v probe_timeout=%v probe_port=%d offline_after=%d traffic_stale=%v",
		livenessInterval, livenessProbeTimeout, m.probePort, livenessOfflineAfter, livenessTrafficStale)
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

// currentProbePort 探测端口是唯一可配置项（管理界面可改）。
func (m *LivenessMonitor) currentProbePort() int {
	port := GetSettings().Int(SettingLivenessProbePort, m.probePort)
	if port <= 0 || port > 65535 {
		return 49151
	}
	return port
}

// checkAll 遍历所有账号，探测设备并更新状态。
func (m *LivenessMonitor) checkAll() {
	var servers []models.WireguardServer
	if err := m.db.Find(&servers).Error; err != nil {
		log.Printf("liveness: failed to list wireguard servers: %v", err)
		return
	}

	now := time.Now()
	timeout := livenessProbeTimeout
	sem := make(chan struct{}, livenessMaxConcurrent)
	var wg sync.WaitGroup

	for _, server := range servers {
		var peers []models.WireguardPeer
		if err := m.db.Select("public_key", "peer_address").
			Where("server_id = ?", server.ID).Find(&peers).Error; err != nil {
			log.Printf("liveness: failed to list peers of server %d: %v", server.ID, err)
			continue
		}
		if len(peers) == 0 {
			continue
		}

		handshakes := make(map[string]time.Time, len(peers))
		transfers := make(map[string]trafficSample, len(peers))
		statsOK := false

		stats, err := m.wg.GetDetailedStats(server.Namespace, server.WgInterface)
		if err != nil {
			// 接口不可用（账号被禁用、网络未就绪）：本轮不出结论
			log.Printf("liveness: stats unavailable for %s/%s: %v", server.Namespace, server.WgInterface, err)
		} else {
			statsOK = true
			for _, peerStats := range stats.Peers {
				handshakes[peerStats.PublicKey] = peerStats.LatestHandshake
				transfers[peerStats.PublicKey] = trafficSample{rx: peerStats.TransferRx, tx: peerStats.TransferTx}
			}
		}

		for _, peer := range peers {
			peerCopy := peer
			sample := transfers[peerCopy.PublicKey]

			wg.Add(1)
			go func() {
				defer wg.Done()

				sem <- struct{}{}
				defer func() { <-sem }()

				// 主动探测：进入该账号的命名空间，向设备隧道地址的高位端口发 TCP SYN
				reachable, rtt := false, time.Duration(0)
				if statsOK {
					target := ProbeTarget(peerCopy.PeerAddress, m.currentProbePort())
					reachable, rtt, _ = ProbeTCPInNamespace(server.Namespace, target, timeout)
				}

				trafficActive := m.recordTraffic(peerCopy.PublicKey, sample, now)
				m.evaluate(peerCopy.PublicKey, handshakes[peerCopy.PublicKey],
					reachable, rtt, trafficActive, statsOK, now)
			}()
		}
	}

	wg.Wait()
}

// recordTraffic 记录累计流量并判断本轮是否有增长。
func (m *LivenessMonitor) recordTraffic(key string, sample trafficSample, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	previous, seen := m.traffic[key]
	m.traffic[key] = sample

	if !seen {
		return false
	}

	// 仅以 rx 增长作为对端活跃的判据（见 trafficSample 的说明）。
	// tx 增长可能只是本机主动探测所致，不能证明对端在线。
	if sample.rx > previous.rx {
		m.lastTrafficSeen[key] = now
		return true
	}
	return false
}

// evaluate 综合主动探测、隧道流量与握手状态得出在线结论。
//
// 在线（立即）：探测有响应，或本轮隧道有流量；
// 保持在线：保护窗口内仍有流量，或握手仍在时效内（应对 ICMP 被拦截的设备）；
// 离线：以上都不满足并连续确认若干次，通常在数秒内完成。
func (m *LivenessMonitor) evaluate(
	key string,
	handshake time.Time,
	reachable bool,
	rtt time.Duration,
	trafficActive bool,
	statsOK bool,
	now time.Time,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[key]
	if !ok {
		result = &LivenessResult{State: LivenessUnknown}
		m.results[key] = result
	}

	result.CheckedAt = now
	result.Checks++
	result.Reachable = reachable
	result.TrafficActive = trafficActive
	if reachable {
		result.LatencyMS = rtt.Milliseconds()
	}

	if handshake.IsZero() {
		result.LastHandshakeAt = nil
		result.HandshakeAgeSeconds = -1
	} else {
		handshakeTime := handshake
		result.LastHandshakeAt = &handshakeTime
		result.HandshakeAgeSeconds = int64(now.Sub(handshake).Seconds())
	}

	if !statsOK {
		result.Reason = ReasonNoStats
		// 统计持续读取失败（账号禁用、网络未就绪）达阈值后标记为未知，
		// 避免界面长期停留在过期结论上。
		result.NoStats++
		if result.NoStats >= livenessOfflineAfter*3 {
			result.State = LivenessUnknown
		}
		return
	}
	result.NoStats = 0

	switch {
	case reachable:
		result.Reason = ReasonProbe

	case trafficActive:
		result.Reason = ReasonTraffic

	default:
		// 保护窗口：最近这段时间内隧道仍有数据往来（保活包即可）。
		// 刻意不使用握手时间：客户端断开后 last handshake 只是停住而不会清空，
		// 拿它当依据会让离线判定滞后一个重协商周期（最长 180 秒）。
		if lastSeen, ok := m.lastTrafficSeen[key]; ok && now.Sub(lastSeen) <= livenessTrafficStale {
			result.Reason = ReasonRecent
		} else {
			result.Failures++
			result.Reason = ReasonTimeout
			if result.Failures >= livenessOfflineAfter {
				result.State = LivenessOffline
			}
			return
		}
	}

	result.State = LivenessOnline
	result.Failures = 0
	result.LastOnlineAt = &now
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
