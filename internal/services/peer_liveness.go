package services

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cloud-platform/internal/config"
	"cloud-platform/internal/models"

	"gorm.io/gorm"
)

// LivenessState 设备在线状态
type LivenessState string

const (
	// LivenessUnknown 尚无结论（首次探测未达阈值，或探测功能刚启动）
	LivenessUnknown LivenessState = "unknown"
	LivenessOnline  LivenessState = "online"
	LivenessOffline LivenessState = "offline"
)

// LivenessResult 单个设备的探测结论快照
type LivenessResult struct {
	// Target 实际探测的地址（peer 隧道地址:端口）
	Target string `json:"target"`
	State  LivenessState `json:"state"`
	// LatencyMS 最近一次成功探测的往返耗时
	LatencyMS int64 `json:"latency_ms"`
	// Reason 最近一次探测的判定依据：refused / handshake / timeout / unreachable / error
	Reason       string     `json:"reason,omitempty"`
	LastProbeAt  time.Time  `json:"last_probe_at"`
	LastOnlineAt *time.Time `json:"last_online_at,omitempty"`
	// Failures 当前连续失败次数
	Failures int `json:"failures"`
	// Probes 累计探测次数
	Probes int64 `json:"probes"`
}

// ProbeOutcome 单次 TCP 探测的结论
type ProbeOutcome struct {
	Alive   bool
	Latency time.Duration
	Reason  string
}

// ProbeTCP 向 target 发起一次 TCP 连接探测。
//
// 判定依据（与内核协议栈行为一致，无需对端安装任何 Agent）：
//   - 握手成功             → 对端存活；
//   - connection refused   → 对端内核回送 RST，同样证明对端存活；
//   - 超时 / 主机不可达     → 本次判为未响应。
func ProbeTCP(ctx context.Context, target string, timeout time.Duration) ProbeOutcome {
	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	latency := time.Since(start)

	if err == nil {
		_ = conn.Close()
		return ProbeOutcome{Alive: true, Latency: latency, Reason: "handshake"}
	}

	// 连接被拒绝：数据已到达对端内核且内核正常回包
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ProbeOutcome{Alive: true, Latency: latency, Reason: "refused"}
	}

	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return ProbeOutcome{Alive: false, Latency: latency, Reason: "timeout"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ProbeOutcome{Alive: false, Latency: latency, Reason: "timeout"}
	}

	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return ProbeOutcome{Alive: false, Latency: latency, Reason: "unreachable"}
	}

	return ProbeOutcome{Alive: false, Latency: latency, Reason: "error"}
}

// LivenessMonitor 周期性地对所有 peer 的隧道地址做存活探测。
type LivenessMonitor struct {
	db *gorm.DB

	interval       time.Duration
	timeout        time.Duration
	offlineAfter   int
	probePort      int
	maxConcurrency int

	// refreshInterval 刷新 peer 列表的周期，远低于探测频率，避免频繁查库
	refreshInterval time.Duration

	mu      sync.RWMutex
	targets map[string]string // peer 公钥 -> "隧道地址:探测端口"
	results map[string]*LivenessResult

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewLivenessMonitor 创建探测监控器。
func NewLivenessMonitor(db *gorm.DB, cfg config.LivenessConfig) *LivenessMonitor {
	refresh := 5 * time.Second
	if cfg.Interval()*5 > refresh {
		refresh = cfg.Interval() * 5
	}

	return &LivenessMonitor{
		db:              db,
		interval:        cfg.Interval(),
		timeout:         cfg.Timeout(),
		offlineAfter:    cfg.OfflineThreshold,
		probePort:       cfg.ProbePort,
		maxConcurrency:  cfg.MaxConcurrency,
		refreshInterval: refresh,
		targets:         make(map[string]string),
		results:         make(map[string]*LivenessResult),
	}
}

// Start 启动探测循环（每秒探测、每 refreshInterval 刷新一次设备列表）。
func (m *LivenessMonitor) Start(parent context.Context) {
	m.ctx, m.cancel = context.WithCancel(parent)

	m.refreshTargets()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		probeTicker := time.NewTicker(m.interval)
		refreshTicker := time.NewTicker(m.refreshInterval)
		defer probeTicker.Stop()
		defer refreshTicker.Stop()

		m.probeAll()

		for {
			select {
			case <-probeTicker.C:
				m.probeAll()
			case <-refreshTicker.C:
				m.refreshTargets()
			case <-m.ctx.Done():
				return
			}
		}
	}()

	log.Printf("Liveness monitor started: interval=%v timeout=%v offline_after=%d port=%d",
		m.interval, m.timeout, m.offlineAfter, m.probePort)
}

// Stop 停止探测循环。
func (m *LivenessMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

// ProbeEnabled 探测是否处于运行状态。
func (m *LivenessMonitor) ProbeEnabled() bool {
	return m.ctx != nil
}

// refreshTargets 从数据库刷新待探测的设备列表（key 为 peer 公钥）。
func (m *LivenessMonitor) refreshTargets() {
	var peers []models.WireguardPeer
	if err := m.db.Select("public_key", "peer_address").Find(&peers).Error; err != nil {
		log.Printf("liveness: failed to refresh peer list: %v", err)
		return
	}

	targets := make(map[string]string, len(peers))
	for _, peer := range peers {
		address := strings.TrimSpace(peer.PeerAddress)
		if address == "" {
			continue
		}
		targets[peer.PublicKey] = net.JoinHostPort(address, strconv.Itoa(m.probePort))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.targets = targets
	// 清理已删除设备的残留结果
	for key := range m.results {
		if _, ok := targets[key]; !ok {
			delete(m.results, key)
		}
	}
}

// probeAll 对当前目标集合发起一轮并发探测。
func (m *LivenessMonitor) probeAll() {
	m.mu.RLock()
	targets := make(map[string]string, len(m.targets))
	for key, target := range m.targets {
		targets[key] = target
	}
	m.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	sem := make(chan struct{}, m.maxConcurrency)
	var wg sync.WaitGroup

	for key, target := range targets {
		wg.Add(1)
		go func(key, target string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			ctx := m.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			m.apply(key, target, ProbeTCP(ctx, target, m.timeout))
		}(key, target)
	}

	wg.Wait()
}

// apply 将单次探测结果并入状态机。
//
// 防抖规则：
//   - 上线：任意一次成功（RST 或握手）立即置为在线，毫秒级响应；
//   - 离线：必须连续失败达到阈值才置为离线，规避公网单次丢包造成的误报。
func (m *LivenessMonitor) apply(key, target string, outcome ProbeOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, ok := m.results[key]
	if !ok {
		result = &LivenessResult{Target: target, State: LivenessUnknown}
		m.results[key] = result
	}

	now := time.Now()
	result.Target = target
	result.LastProbeAt = now
	result.Probes++
	result.Reason = outcome.Reason

	if outcome.Alive {
		result.State = LivenessOnline
		result.LatencyMS = outcome.Latency.Milliseconds()
		result.Failures = 0
		result.LastOnlineAt = &now
		return
	}

	result.Failures++
	// 未达阈值时保持既有状态（unknown 或 online），避免抖动
	if result.Failures >= m.offlineAfter {
		result.State = LivenessOffline
	}
}

// Snapshot 返回全部设备的探测结果副本。
func (m *LivenessMonitor) Snapshot() map[string]LivenessResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]LivenessResult, len(m.results))
	for key, result := range m.results {
		out[key] = *result
	}
	return out
}

// Result 返回指定设备的探测结果。
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
