package services

import (
	"testing"
	"time"

	"cloud-platform/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestMonitor() *LivenessMonitor {
	return &LivenessMonitor{
		interval:         3 * time.Second,
		handshakeTimeout: 180 * time.Second,
		offlineAfter:     2,
		results:          make(map[string]*LivenessResult),
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

// 核心场景：握手新鲜即在线，上线是立即生效的。
func TestLivenessOnlineOnFreshHandshake(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("peer-a", now.Add(-3*time.Second), true, now)

	if got := stateOf(t, monitor, "peer-a"); got != LivenessOnline {
		t.Fatalf("握手 3 秒前应判定在线，实际 %q", got)
	}

	result, _ := monitor.Result("peer-a")
	if result.HandshakeAgeSeconds != 3 {
		t.Fatalf("应记录握手年龄 3 秒，实际 %d", result.HandshakeAgeSeconds)
	}
	if result.LastHandshakeAt == nil {
		t.Fatal("应记录最近握手时间")
	}
}

// 握手过期需要连续确认，避免临界抖动直接翻成离线。
func TestLivenessOfflineNeedsConsecutiveTimeouts(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-b"

	monitor.evaluate(key, now.Add(-time.Second), true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("前置条件：应为在线，实际 %q", got)
	}

	stale := now.Add(-10 * time.Minute)

	// 第一次过期：保持在线
	monitor.evaluate(key, stale, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("单次过期不应立即判离线，实际 %q", got)
	}

	// 第二次过期：判定离线
	monitor.evaluate(key, stale, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOffline {
		t.Fatalf("连续两次过期应判离线，实际 %q", got)
	}

	// 重新握手：立即回到在线
	monitor.evaluate(key, now, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("重新握手后应立刻在线，实际 %q", got)
	}
}

// 从未握手的设备直接判离线，不需要等待阈值。
func TestLivenessNeverHandshakedIsOffline(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("peer-never", time.Time{}, true, now)

	if got := stateOf(t, monitor, "peer-never"); got != LivenessOffline {
		t.Fatalf("从未握手应判离线，实际 %q", got)
	}
	result, _ := monitor.Result("peer-never")
	if result.HandshakeAgeSeconds != -1 {
		t.Fatalf("未握手时握手年龄应为 -1，实际 %d", result.HandshakeAgeSeconds)
	}
}

// 接口读取失败时保留原结论，避免账号被禁用期间状态来回跳变。
func TestLivenessKeepsStateWhenStatsUnavailable(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()
	const key = "peer-c"

	monitor.evaluate(key, now, true, now)
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("前置条件：应为在线，实际 %q", got)
	}

	// 连续多次统计不可用，状态不应改变
	for i := 0; i < 5; i++ {
		monitor.evaluate(key, time.Time{}, false, now)
	}
	if got := stateOf(t, monitor, key); got != LivenessOnline {
		t.Fatalf("统计不可用时应保持在线，实际 %q", got)
	}
}

func TestLivenessCounts(t *testing.T) {
	monitor := newTestMonitor()
	now := time.Now()

	monitor.evaluate("online-1", now, true, now)
	monitor.evaluate("online-2", now.Add(-10*time.Second), true, now)
	monitor.evaluate("offline-1", time.Time{}, true, now)
	monitor.evaluate("unknown-1", now, false, now)

	online, offline, unknown := monitor.Counts()
	if online != 2 || offline != 1 || unknown != 1 {
		t.Fatalf("统计不正确: online=%d offline=%d unknown=%d", online, offline, unknown)
	}
}

// checkAll 在接口不可用时不应把设备误判为离线。
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

	// 命名空间不存在 → wg show 失败 → 不应得出「离线」结论
	monitor.checkAll()

	result, ok := monitor.Result(peer.PublicKey)
	if !ok {
		t.Fatal("应为该设备建立占位结论（unknown）")
	}
	if result.State != LivenessUnknown {
		t.Fatalf("统计不可用时应保持 unknown，实际 %q", result.State)
	}
}
