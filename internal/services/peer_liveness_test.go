package services

import (
	"testing"
	"time"

	"cloud-platform/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 判定参数已是内部常量，夹具只需初始化容器字段。
func newTestMonitor() *LivenessMonitor {
	return &LivenessMonitor{
		probePort:       49151,
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

// 关键回归：本机主动探测会抬高 tx，不能因此把离线对端判成在线。
// 只有 rx（本机从对端收到的字节）增长才证明对端真的在发包。
func TestTrafficIgnoresOutboundOnly(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-tx"

	// 建立基线
	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)

	// 对端离线：本机不停发探测包，tx 持续增长，rx 不变
	if active := monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 9000}, now.Add(2*time.Second)); active {
		t.Fatal("仅 tx 增长不得判定为活跃（那是本机探测包自身造成的）")
	}

	// 连续多轮 tx 增长同样不得累积成在线
	for i := 0; i < 5; i++ {
		if active := monitor.recordTraffic(key, trafficSample{rx: 1000, tx: int64(9000 + i*1000)}, now.Add(time.Duration(i+3)*time.Second)); active {
			t.Fatalf("第 %d 轮仅 tx 增长仍被误判为活跃", i+1)
		}
	}

	// 端到端：探测无响应 + 只有 tx 增长 → 应判离线
	expired := now.Add(31 * time.Second)
	for i := 0; i < 2; i++ {
		active := monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 50000}, expired)
		monitor.evaluate(key, expired.Add(-10*time.Minute), false, 0, active, true, expired)
	}
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("对端离线（仅本机发包）时应判离线，实际 %q", got)
	}
}

// 对端在发包（rx 增长）时仍应维持在线——保活流量是有效的在线证据。
func TestTrafficCountsInboundOnly(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-rx"

	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)

	// 对端发来保活包：rx 增长
	if active := monitor.recordTraffic(key, trafficSample{rx: 1200, tx: 2000}, now.Add(25*time.Second)); !active {
		t.Fatal("rx 增长应判定为活跃")
	}

	// 探测可能被对端防火墙拦截，但保活流量足以维持在线
	monitor.evaluate(key, now.Add(-10*time.Minute), false, 0, true, true, now.Add(25*time.Second))
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("有对端来向流量时应维持在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonTraffic {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTraffic, got)
	}
}

// 核心场景：客户端断开后不得被"残留的握手时间"拖住。
// 握手时间戳在断开后只是停住，若拿它当依据会滞后一个重协商周期。
func TestLivenessOfflineDespiteFreshHandshake(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-d"

	// 30 秒前刚握过手（远小于旧版 180 秒窗口），但此刻探测无响应、无流量
	freshHandshake := now.Add(-30 * time.Second)

	for i := 0; i < 2; i++ {
		monitor.evaluate(key, freshHandshake, false, 0, false, true, now)
	}

	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("探测无响应且无流量时应判离线，不应被握手时间拖住，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonTimeout {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTimeout, got)
	}
}

// 流量停止后经过保护窗口即判离线。
func TestLivenessOfflineAfterTrafficStops(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-e"

	// 窗口内仍有流量：维持在线
	monitor.recordTraffic(key, trafficSample{rx: 100, tx: 200}, now)
	monitor.recordTraffic(key, trafficSample{rx: 300, tx: 400}, now)
	monitor.evaluate(key, now.Add(-10*time.Minute), false, 0, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("保护窗口内应维持在线，实际 %q", got)
	}

	// 窗口过期后：连续两次无响应即离线
	expired := now.Add(31 * time.Second)
	for i := 0; i < 2; i++ {
		monitor.evaluate(key, expired.Add(-10*time.Minute), false, 0, false, true, expired)
	}
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("保护窗口过期后应判离线，实际 %q", got)
	}
}

// 统计短时不可用时保留既有结论；持续不可用则转为未知，避免停在过期结论上。
func TestLivenessNoStatsBehaviour(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-f"

	monitor.evaluate(key, now, true, time.Millisecond, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("前置条件：应为在线，实际 %q", got)
	}

	// 短时不可用：保持在线
	monitor.evaluate(key, time.Time{}, false, 0, false, false, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("短时统计不可用时应保持在线，实际 %q", got)
	}

	// 持续不可用：转为未知
	for i := 0; i < 10; i++ {
		monitor.evaluate(key, time.Time{}, false, 0, false, false, now)
	}
	if got := stateOf(t, monitor, key); got != LivenessUnknown {
		t.Fatalf("统计持续不可用时应转为 unknown，实际 %q", got)
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

// 探测函数：参数缺失或不存在的命名空间应快速失败，不阻塞判定循环。
func TestProbeTCPInNamespaceFailurePaths(t *testing.T) {
	if ok, _, _ := ProbeTCPInNamespace("", "10.0.0.1:49151", time.Second); ok {
		t.Fatal("命名空间为空时不应判定可达")
	}
	if ok, _, _ := ProbeTCPInNamespace("wg_missing", "", time.Second); ok {
		t.Fatal("目标为空时不应判定可达")
	}

	start := time.Now()
	ok, _, detail := ProbeTCPInNamespace("wg_nonexistent_netns", "10.99.99.99:49151", time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("不存在的命名空间不应判定可达")
	}
	if detail != probeDetailSetupFailed {
		t.Fatalf("应标记为 %s，实际 %q", probeDetailSetupFailed, detail)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("失败路径不应长时间阻塞，实际耗时 %v", elapsed)
	}
}

func TestProbeTarget(t *testing.T) {
	cases := []struct {
		address string
		port    int
		want    string
	}{
		{"10.100.0.2", 49151, "10.100.0.2:49151"},
		{"10.100.0.2", 0, "10.100.0.2:49151"},     // 端口非法时回退默认值
		{"10.100.0.2", 70000, "10.100.0.2:49151"}, // 超范围同样回退
		{"", 49151, ""}, // 地址缺失
	}

	for _, tc := range cases {
		if got := ProbeTarget(tc.address, tc.port); got != tc.want {
			t.Fatalf("ProbeTarget(%q, %d) = %q, want %q", tc.address, tc.port, got, tc.want)
		}
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
