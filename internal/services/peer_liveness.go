package services

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"cloud-platform/internal/config"
	"cloud-platform/internal/models"

	"gorm.io/gorm"
)

// 判定参数由服务端内部决定，不对外暴露为配置项。
//
// 取值依据：
//   - 探测间隔 1s：配合"连续 2 次确认"，断开后约 2 秒即可判定，真正的秒级；
//   - 单次超时 600ms：必须明显小于探测间隔，否则对端不可达时单轮会耗掉整个
//     间隔（实测 1s 超时会让判定从 2 秒拖到 5 秒）。隧道内往返实测亚毫秒级，
//     600ms 给了移动网络充足的余量；
//   - 离线确认 2 次：约 4 秒完成判定，规避单次丢包造成的误报；
//   - 并发上限 16：限制同一时刻 fork 的探测数量。
//
// 判定不设"流量保护窗口"：那会引入一个必须大于保活间隔的等待期，直接拖慢判定，
// 而保活本就不可靠（见 wireguard.go 中 serverKeepaliveSeconds 的说明）。
// 现在以主动探测为唯一主判据，接收方向的流量只作为辅助证据。
const (
	livenessInterval = time.Second
	// livenessProbeTimeout 单次探测超时。
	//
	// 必须能容忍质量较差的链路：移动网络下设备往返常达数百毫秒，个别情况
	// 超过 1 秒。超时若小于链路 RTT，在线设备会被误判为无响应。
	// 这里取 3 秒（约 3 倍最坏 RTT），同时配合"连续 2 次确认"与并发探测，
	// 使真正断开的设备仍能在数秒内被判离线。
	livenessProbeTimeout = 3 * time.Second
	// livenessOfflineAfter 连续多少次无响应判离线。
	// 慢链路下单次探测本身可能耗时接近超时，因此保持 2 次即可，
	// 不靠增加次数来过滤抖动（那会显著拖慢判定）。
	livenessOfflineAfter  = 2
	livenessMaxConcurrent = 16
	// livenessTrafficFresh 流量新鲜度窗口。
	// 必须大于 stats 缓存 TTL（1 秒）与探测间隔，确保"缓存导致的重复快照"
	// 不会被误判为对端停止发包；同时要足够小，使真的断开能被及时反映。
	// 判定最终仍由主动探测主导，这里只作为探测失败时的辅助证据。
	livenessTrafficFresh = 6 * time.Second
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
	// LatencyMS 最近一次主动探测的往返耗时（毫秒，亚毫秒会截断为 0）
	LatencyMS int64 `json:"latency_ms"`
	// LatencyUS 同一耗时的微秒表示，保留亚毫秒精度
	LatencyUS int64 `json:"latency_us"`
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

	// checking 防止上一轮尚未结束就启动下一轮。
	// 超时已放宽到 3 秒以容忍慢链路，若不加保护，1 秒的触发间隔会让
	// 轮次不断重叠、goroutine 持续堆积。
	checking atomic.Bool
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
				if !m.checking.CompareAndSwap(false, true) {
					continue // 上一轮尚未结束（有慢设备仍在探测），跳过本轮
				}
				m.checkAll()
				m.checking.Store(false)
			case <-m.ctx.Done():
				return
			}
		}
	}()

	log.Printf("Liveness monitor started: interval=%v probe_timeout=%v probe_port=%d offline_after=%d",
		livenessInterval, livenessProbeTimeout, m.probePort, livenessOfflineAfter)
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

// recordTraffic 记录累计流量并判断"自上次采样以来"是否有增长。
//
// 仅以 rx 增长作为对端活跃的判据（见 trafficSample 的说明）：tx 增长可能只是
// 本机主动探测所致，不能证明对端在线。
//
// 关键细节：stats 读取有 1 秒缓存，而探测间隔也是 1 秒，因此相邻两轮很可能拿到
// 完全相同的快照。此时不能把"没变化"当作"对端不活跃"——那会让判定在
// 在线/离线之间反复跳变（用户可见的状态抖动）。这里用「最近一次流量增长的时间」
// 配合判定窗口，使重复快照只影响新鲜度、不直接翻转状态。
func (m *LivenessMonitor) recordTraffic(key string, sample trafficSample, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	previous, seen := m.traffic[key]
	m.traffic[key] = sample

	if !seen {
		// 首个样本只建立基线。不标记为活跃：否则刚启动时离线设备会在
		// 新鲜度窗口内被误判为在线。
		return false
	}

	if sample.rx > previous.rx {
		m.lastTrafficSeen[key] = now
		return true
	}

	// 快照未变化：可能是 stats 缓存导致的重复快照，也可能是对端真的安静了。
	// 只有在此前确实观察到过增长的前提下，才用新鲜度窗口维持"活跃"，
	// 否则（从未有过来向流量）直接判为不活跃。
	last, everSeen := m.lastTrafficSeen[key]
	if !everSeen {
		return false
	}
	return now.Sub(last) <= livenessTrafficFresh
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
		// 保留亚毫秒精度：Milliseconds() 会把 100~900µs 截断为 0，
		// 前端只能显示成"<1ms"，无法反映真实往返。这里以微秒精度记录，
		// 供前端按需格式化为 µs 或 ms。
		result.LatencyUS = rtt.Microseconds()
		result.LatencyMS = rtt.Milliseconds()
	} else {
		// 本轮探测没有得到响应：必须清零，否则会保留上一次成功探测的旧值。
		// 那样的数据自相矛盾（reachable=false 却带着延迟），会让用户误以为
		// 当前链路就是那个耗时。
		result.LatencyUS = 0
		result.LatencyMS = 0
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
		// 统计读取失败（wg show 在命名空间上偶发失败、账号禁用、网络未就绪）
		// 时完全保留既有结论：只记录计数与依据，绝不改动 State。
		//
		// 这里曾把连续失败翻译成 unknown，但 wg show 的失败是间歇性的：
		// 几次失败转 unknown、一次成功又回到 online，界面表现为状态反复跳变。
		// 判定应由探测结果驱动，而不是由"本轮能否读到统计"决定。
		result.Reason = ReasonNoStats
		result.NoStats++
		return
	}
	result.NoStats = 0

	switch {
	case reachable:
		result.Reason = ReasonProbe

	case trafficActive:
		result.Reason = ReasonTraffic

	default:
		result.Failures++
		result.Reason = ReasonTimeout
		if result.Failures >= livenessOfflineAfter {
			result.State = LivenessOffline
		}
		return
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
