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

// 探测无响应但对端仍有来向流量：判在线（探测可能被其防火墙拦截）。
func TestLivenessOnlineOnInboundTraffic(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("peer-b", now.Add(-10*time.Minute), false, 0, true, true, now)

	if got := stateOf(t, monitor, "peer-b"); got != LivenessOnline {
		t.Fatalf("有来向流量应判定在线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, "peer-b"); got != ReasonTraffic {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTraffic, got)
	}
}

// 核心场景：探测失败即开始累计，连续两次（约 4 秒）判离线——这是"秒级感知"的来源。
func TestLivenessOfflineWithinTwoFailures(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-c"

	// 首次失败：尚未达阈值，维持原状态
	monitor.evaluate(key, now, false, 0, false, true, now)
	if got := stateOf(t, monitor, key); got == LivenessOffline {
		t.Fatal("单次失败不应立即判离线（防止丢包误报）")
	}

	// 第二次失败：判离线
	monitor.evaluate(key, now.Add(2*time.Second), false, 0, false, true, now.Add(2*time.Second))
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("连续两次无响应应判离线，实际 %q", got)
	}
	if got := reasonOf(t, monitor, key); got != ReasonTimeout {
		t.Fatalf("判定依据应为 %s，实际 %q", ReasonTimeout, got)
	}

	// 探测恢复：立即回到在线
	monitor.evaluate(key, now.Add(4*time.Second), true, 2*time.Millisecond, false, true, now.Add(4*time.Second))
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("探测恢复后应立即在线，实际 %q", got)
	}
}

// 客户端断开后握手时间戳只是停住、不会清空，不能作为在线依据。
func TestLivenessOfflineDespiteFreshHandshake(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-d"

	freshHandshake := now.Add(-30 * time.Second)
	for i := 0; i < 2; i++ {
		monitor.evaluate(key, freshHandshake, false, 0, false, true, now)
	}

	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("探测无响应时应判离线，不应被握手时间拖住，实际 %q", got)
	}
}

// 关键回归：stats 有 1 秒缓存、探测间隔 1 秒，相邻两轮常拿到同一份快照。
// 重复快照不得被当成"对端停止发包"而翻转状态——这正是状态抖动的来源。
func TestTrafficRepeatedSnapshotsDoNotFlip(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-jitter"

	// 首个样本只建立基线；随后出现一次真实增长（保活包）
	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)
	monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, now)

	// 连续多轮拿到完全相同的快照（缓存命中），应始终视为活跃
	for i := 1; i <= 5; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		if active := monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, at); !active {
			t.Fatalf("第 %d 轮重复快照被误判为不活跃", i)
		}
	}

	// 端到端：探测成功 + 重复快照 → 稳定在线，不出现震荡
	for i := 0; i <= 6; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		active := monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, at)
		monitor.evaluate(key, at, true, 300*time.Microsecond, active, true, at)
		if got := stateOf(t, monitor, key); got != LivenessOnline {
			t.Fatalf("第 %d 轮出现状态抖动：%q", i, got)
		}
	}
}

// 超出新鲜度窗口后，重复快照才被认定为不再活跃。
func TestTrafficStaleAfterWindow(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-stale"

	// 建立基线，并在窗口内出现一次真实增长
	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)
	within := now.Add(time.Second)
	monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, within)

	// 窗口内的重复快照：仍视为活跃
	if active := monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, within.Add(time.Second)); !active {
		t.Fatal("新鲜度窗口内的重复快照应视为活跃")
	}

	// 超出窗口后再无增长：判定为不活跃
	after := within.Add(livenessTrafficFresh + time.Second)
	if active := monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, after); active {
		t.Fatal("超出新鲜度窗口后应判定为不活跃")
	}
}

// 关键回归：本机主动探测会抬高 tx，不能因此把离线对端判成在线。
//
// 注意语义：tx 增长不刷新"最近来向流量时刻"，所以在新鲜度窗口过期后，
// 仅靠 tx 增长的连接会被判定为不活跃。
func TestTrafficIgnoresOutboundOnly(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-tx"

	// 建立基线，并出现一次真实的来向流量
	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)
	monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 2000}, now)

	// 对端随后离线：本机不停发探测包，tx 持续增长，rx 不变。
	// 新鲜度窗口过期后必须判定为不活跃。
	after := now.Add(livenessTrafficFresh + time.Second)
	if active := monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 90000}, after); active {
		t.Fatal("窗口过期后仅 tx 增长不得判定为活跃")
	}

	// 端到端：探测无响应 + 仅 tx 增长 → 应判离线
	for i := 0; i < 2; i++ {
		active := monitor.recordTraffic(key, trafficSample{rx: 1500, tx: 100000}, after)
		monitor.evaluate(key, after.Add(-10*time.Minute), false, 0, active, true, after)
	}
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("对端离线（仅本机发包）时应判离线，实际 %q", got)
	}
}

// rx 增长是对端发包的直接证据，应判定活跃并维持在线。
func TestTrafficCountsInboundOnly(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-rx"

	monitor.recordTraffic(key, trafficSample{rx: 1000, tx: 2000}, now)
	if active := monitor.recordTraffic(key, trafficSample{rx: 1200, tx: 2000}, now); !active {
		t.Fatal("rx 增长应判定为活跃")
	}

	monitor.evaluate(key, now.Add(-10*time.Minute), false, 0, true, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("有对端来向流量时应维持在线，实际 %q", got)
	}
}

// 关键回归：探测未响应时必须清零延迟，不能保留上一轮成功时的旧值。
// 否则会出现 reachable=false 却带着延迟的自相矛盾数据，用户看到的就是
// 一个与当前链路无关的陈旧耗时。
func TestLatencyClearedWhenProbeFails(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-lat"

	// 第一轮：探测成功，记录耗时
	monitor.evaluate(key, now, true, 236*time.Microsecond, false, true, now)
	result, _ := monitor.Result(key)
	if result.LatencyUS != 236 {
		t.Fatalf("探测成功时应记录 236µs，实际 %d", result.LatencyUS)
	}

	// 第二轮：探测失败，但靠流量维持在线
	later := now.Add(time.Second)
	monitor.evaluate(key, later, false, 0, true, true, later)

	result, _ = monitor.Result(key)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("有流量时应维持在线，实际 %q", got)
	}
	if result.LatencyUS != 0 || result.LatencyMS != 0 {
		t.Fatalf("探测失败后延迟应清零，实际 us=%d ms=%d", result.LatencyUS, result.LatencyMS)
	}
	if result.Reachable {
		t.Fatal("本轮探测未响应，reachable 应为 false")
	}

	// 第三轮：探测恢复，重新记录耗时
	back := later.Add(time.Second)
	monitor.evaluate(key, back, true, 550*time.Microsecond, false, true, back)
	result, _ = monitor.Result(key)
	if result.LatencyUS != 550 {
		t.Fatalf("探测恢复后应记录新值 550µs，实际 %d", result.LatencyUS)
	}
}

// 统计读取失败（wg show 偶发失败、账号禁用等）绝不能改动状态：
// 一旦把失败翻译成 unknown，间歇性失败会让界面在在线/非在线之间反复跳变。
func TestLivenessNoStatsKeepsState(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-f"

	// 先建立"在线"结论
	monitor.evaluate(key, now, true, time.Millisecond, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("前置条件：应为在线，实际 %q", got)
	}

	// 无论统计失败多少次，状态都必须保持不变
	for i := 0; i < 20; i++ {
		monitor.evaluate(key, time.Time{}, false, 0, false, false, now)
		if got := stateOf(t, monitor, key); got != LivenessOnline {
			t.Fatalf("第 %d 次统计失败后状态被改动为 %q", i+1, got)
		}
	}

	result, _ := monitor.Result(key)
	if result.Reason != ReasonNoStats {
		t.Fatalf("判定依据应标记为 %s，实际 %q", ReasonNoStats, result.Reason)
	}
	if result.NoStats < 20 {
		t.Fatalf("应累计统计失败次数，实际 %d", result.NoStats)
	}

	// 离线状态同样不应因统计失败而改变
	const offKey = "peer-off"
	monitor.evaluate(offKey, now, false, 0, false, true, now)
	monitor.evaluate(offKey, now, false, 0, false, true, now)
	if got := stateOf(t, monitor, offKey); got != LivenessOffline {
		t.Fatalf("前置条件：应为离线，实际 %q", got)
	}
	for i := 0; i < 10; i++ {
		monitor.evaluate(offKey, time.Time{}, false, 0, false, false, now)
	}
	if got := stateOf(t, monitor, offKey); got != LivenessOffline {
		t.Fatalf("离线状态在统计失败后被改动为 %q", got)
	}

	// 统计恢复后，正常判定继续生效
	monitor.evaluate(key, now, true, time.Millisecond, false, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("统计恢复后应正常判定，实际 %q", got)
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
		PeerAddress: "10.100.0.2", AllowedIPs: "10.100.0.2/32", PersistentKeepalive: 10,
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
