package services

import (
	"context"
	"testing"
	"time"

	"cloud-platform/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestMonitor() *LivenessMonitor {
	return &LivenessMonitor{
		interval:        2 * time.Second,
		probeTimeout:    time.Second,
		offlineAfter:    2,
		handshakeWindow: 180 * time.Second,
		trafficStale:    40 * time.Second,
		maxConcurrency:  4,
		results:         make(map[string]*LivenessResult),
		traffic:         make(map[string]trafficSample),
		lastTrafficSeen: make(map[string]time.Time),
	}
}

func stateOf(t *testing.T, monitor *LivenessMonitor, key string) LivenessState {
	t.Helper()
	result, ok := monitor.Result(key)
	if !ok {
		t.Fatalf("期望存在 %s 的判定结果", key)
	}
	return result.State
}

func reasonOf(t *testing.T, monitor *LivenessMonitor, key string) string {
	t.Helper()
	result, _ := monitor.Result(key)
	return result.Reason
}

// 主动探测有响应：立即在线，并记录往返耗时。
func TestLivenessOnlineOnProbe(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("peer-a", now.Add(-10*time.Minute), true, 3*time.Millisecond, false, true, now)

	if got := stateOf(t, monitor, "peer-a"); got != LivenessOnline {
		t.Fatalf("探测有响应应判定在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, "peer-a"); got != ReasonProbe {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonProbe, got)
	}
	result, _ := monitor.Result("peer-a")
	if result.LatencyMS != 3 {
		t.Fatalf("应记录探测时延 3ms，实际 %d", result.LatencyMS)
	}
}

// 探测无响应但隧道仍有流量：判在线（保活流量即可维持）。
func TestLivenessOnlineOnTraffic(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("peer-b", now.Add(-10*time.Minute), false, 0, true, true, now)

	if got := stateOf(t, monitor, "peer-b"); got != LivenessOnline {
		t.Fatalf("有流量应判定在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, "peer-b"); got != ReasonTraffic {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTraffic, got)
	}
}

// 探测无响应、本轮无流量，但保护窗口内仍有流量：维持在线（避免误判）。
func TestLivenessKeepsOnlineWithinTrafficWindow(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	// 先制造一次流量，随后不再有流量
	monitor.recordTraffic("peer-c", trafficSample{rx: 100, tx: 200}, now)
	monitor.recordTraffic("peer-c", trafficSample{rx: 300, tx: 400}, now)

	// 10 秒后探测失败：保护窗口 40 秒内应维持在线
	later := now.Add(10 * time.Second)
	monitor.evaluate("peer-c", later.Add(-10*time.Minute), false, 0, false, true, later)

	if got := stateOf(t, monitor, "peer-c"); got != LivenessOnline {
		t.Fatalf("保护窗口内应维持在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, "peer-c"); got != ReasonRecent {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonRecent, got)
	}
}

// 断开后的实时判定：探测失败 + 流量停止 + 保护窗口过期 → 连续两次即离线。
func TestLivenessOfflineQuicklyAfterTrafficStops(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-d"

	// 历史上曾有流量（已超出保护窗口）
	monitor.recordTraffic(key, trafficSample{rx: 100, tx: 200}, now.Add(-5*time.Minute))
	monitor.recordTraffic(key, trafficSample{rx: 300, tx: 400}, now.Add(-5*time.Minute))

	// 第一轮：探测失败、无新流量，但握手仍在时效内 → 作为弱信号维持在线
	monitor.evaluate(key, now.Add(-30*time.Second), false, 0, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("握手仍在时效内应维持在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonHandshake {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonHandshake, got)
	}

	// 握手过期后：连续两次无响应即判离线
	stale := now.Add(-10 * time.Minute)
	monitor.evaluate(key, stale, false, 0, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("首次超时不应立即判离线，实际 %q", got)
	}

	monitor.evaluate(key, stale, false, 0, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("连续两次无响应应判离线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonTimeout {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTimeout, got)
	}

	// 重新探测成功：立即恢复在线
	monitor.evaluate(key, now, true, 2*time.Millisecond, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("探测恢复后应立即在线，实际 %q", got)
	}
}

// 统计不可用（账号禁用/网络未就绪）时不改变既有结论。
func TestLivenessKeepsStateWhenStatsUnavailable(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-e"

	monitor.evaluate(key, now, true, time.Millisecond, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("前置条件：应为在线，实际 %q", got)
	}

	for i := 0; i < 5; i++ {
		monitor.evaluate(key, time.Time{}, false, 0, false, false, now)
	}

	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("统计不可用时应保持在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonNoStats {
		t.Fatalf("判定依据应标记为 %s，实际 %q", ReasonNoStats, got)
	}
}

func TestLivenessCounts(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("online-1", now, true, 0, false, true, now)
	monitor.evaluate("online-2", now, false, 0, true, true, now)
	monitor.evaluate("unknown-1", now, false, 0, false, false, now)

	online, offline, unknown := monitor.Counts()
	if online != 2 || offline != 0 || unknown != 1 {
		t.Fatalf("统计不正确: online=%d offline=%d unknown=%d", online, offline, unknown)
	}
}

// 探测函数：目标为空或命名空间不存在时应快速返回失败，不阻塞判定循环。
func TestProbePeerReachableFailurePaths(t *testing.T) {
	ctx := context.Background()

	if ok, _ := ProbePeerReachable(ctx, "", "10.0.0.1", time.Second); ok {
		t.Fatal("命名空间为空时不应判定可达")
	}
	if ok, _ := ProbePeerReachable(ctx, "wg_missing", "", time.Second); ok {
		t.Fatal("目标为空时不应判定可达")
	}

	start := time.Now()
	ok, _ := ProbePeerReachable(ctx, "wg_nonexistent_netns", "10.99.99.99", time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("不存在的命名空间不应判定可达")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("失败路径不应长时间阻塞，实际耗时 %v", elapsed)
	}
}

// checkAll 在接口不可用时不产生判定结论。
func TestCheckAllSkipsWhenStatsUnavailable(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:liveness_checkall?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.WireguardServer{}, &models.WireguardPeer{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{Email: "live@example.com", Name: "live", Role: models.RoleNormalUser}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	server := models.WireguardServer{
		UserID: user.ID, Namespace: "wg_does_not_exist", WgInterface: "wg0", WgPort: 51821,
		WgPublicKey: "KEY=", WgPrivateKey: "KEY=", WgAddress: "10.100.0.1/24",
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("create server: %v", err)
	}
	peer := models.WireguardPeer{
		ServerID: server.ID, PublicKey: "PEERKEY=", PrivateKey: "KEY=",
		PeerAddress: "10.100.0.2", AllowedIPs: "10.100.0.2/32", PersistentKeepalive: 25,
	}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatalf("create peer: %v", err)
	}

	monitor := newTestMonitor()
	monitor.db = db
	monitor.wg = NewWireguardService("/tmp")

	monitor.checkAll()

	result, ok := monitor.Result(peer.PublicKey)
	if !ok {
		t.Fatal("应为该设备建立占位结论（unknown）")
	}
	if result.State != LivenessUnknown {
		t.Fatalf("统计不可用时应保持 unknown，实际 %q", result.State)
	}
}
