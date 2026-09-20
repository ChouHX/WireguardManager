package services

import (
	"context"
	"net"
	"testing"
	"time"

	"cloud-platform/internal/config"
	"cloud-platform/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestProbeTCPHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	outcome := ProbeTCP(context.Background(), listener.Addr().String(), time.Second)
	if !outcome.Alive {
		t.Fatalf("对监听中的端口探测应判定存活，实际: %+v", outcome)
	}
	if outcome.Reason != "handshake" {
		t.Fatalf("判定依据应为 handshake，实际 %q", outcome.Reason)
	}
}

// 关键场景：对端存活但端口未开放时，内核回 RST（connection refused），
// 这必须被判定为"在线"。
func TestProbeTCPRefusedCountsAsAlive(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	outcome := ProbeTCP(context.Background(), addr, time.Second)
	if !outcome.Alive {
		t.Fatalf("连接被拒绝（RST）应判定存活，实际: %+v", outcome)
	}
	if outcome.Reason != "refused" {
		t.Fatalf("判定依据应为 refused，实际 %q", outcome.Reason)
	}
}

func TestProbeTCPTimeoutCountsAsUnresponsive(t *testing.T) {
	// 192.0.2.0/24 是 TEST-NET-1，保留用于文档示例，不会出现在公网路由中
	outcome := ProbeTCP(context.Background(), "192.0.2.1:49151", 150*time.Millisecond)
	if outcome.Alive {
		t.Fatalf("不可达地址不应判定存活，实际: %+v", outcome)
	}
	if outcome.Reason == "" {
		t.Fatalf("超时/不可达应带有判定依据")
	}
}

func TestLivenessStateMachine(t *testing.T) {
	monitor := &LivenessMonitor{
		timeout:        time.Second,
		offlineAfter:   2,
		probePort:      49151,
		maxConcurrency: 4,
		targets:        map[string]string{},
		results:        map[string]*LivenessResult{},
	}

	const key = "peer-public-key"
	const target = "10.100.0.2:49151"

	stateOf := func() LivenessState {
		result, ok := monitor.Result(key)
		if !ok {
			t.Fatal("期望存在探测结果")
		}
		return result.State
	}

	// 首次失败：未达阈值，保持未知而非直接判离线
	monitor.apply(key, target, ProbeOutcome{Alive: false, Reason: "timeout"})
	if got := stateOf(); got != LivenessUnknown {
		t.Fatalf("单次失败后状态应为 unknown，实际 %q", got)
	}

	// 连续第二次失败：判定离线
	monitor.apply(key, target, ProbeOutcome{Alive: false, Reason: "timeout"})
	if got := stateOf(); got != LivenessOffline {
		t.Fatalf("连续两次失败后状态应为 offline，实际 %q", got)
	}

	// 一次成功：立即回到在线
	monitor.apply(key, target, ProbeOutcome{Alive: true, Latency: 3 * time.Millisecond, Reason: "refused"})
	if got := stateOf(); got != LivenessOnline {
		t.Fatalf("成功探测后状态应为 online，实际 %q", got)
	}
	if result, _ := monitor.Result(key); result.LatencyMS != 3 || result.Failures != 0 {
		t.Fatalf("在线结果应记录时延并清零失败计数，实际: %+v", result)
	}

	// 在线状态下单次失败：保持在线（防抖）
	monitor.apply(key, target, ProbeOutcome{Alive: false, Reason: "timeout"})
	if got := stateOf(); got != LivenessOnline {
		t.Fatalf("单次丢包不应立即判离线，实际 %q", got)
	}

	// 再次失败：达到阈值，判定离线
	monitor.apply(key, target, ProbeOutcome{Alive: false, Reason: "timeout"})
	if got := stateOf(); got != LivenessOffline {
		t.Fatalf("连续两次失败后应判离线，实际 %q", got)
	}

	online, offline, unknown := monitor.Counts()
	if online != 0 || offline != 1 || unknown != 0 {
		t.Fatalf("统计不正确: online=%d offline=%d unknown=%d", online, offline, unknown)
	}
}

// 端到端：从数据库读取 peer，对 127.0.0.1 上未监听的端口做真实探测，
// 预期因 RST 而判定在线。
func TestLivenessMonitorAgainstLocalhost(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:liveness_e2e?mode=memory&cache=shared"),
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
		UserID: user.ID, Namespace: "wg_" + user.UserUID, WgInterface: "wg0", WgPort: 51821,
		WgPublicKey: "KEY=", WgPrivateKey: "KEY=", WgAddress: "10.100.0.1/24",
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("create server: %v", err)
	}
	peer := models.WireguardPeer{
		ServerID: server.ID, PublicKey: "PEERKEY=", PrivateKey: "KEY=",
		PeerAddress: "127.0.0.1", AllowedIPs: "127.0.0.1/32", PersistentKeepalive: 25,
	}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatalf("create peer: %v", err)
	}

	monitor := NewLivenessMonitor(db, config.LivenessConfig{
		Enabled: true, IntervalSeconds: 1, TimeoutMS: 300,
		OfflineThreshold: 2, ProbePort: 49151, MaxConcurrency: 4,
	})
	monitor.Start(context.Background())
	defer monitor.Stop()

	deadline := time.Now().Add(4 * time.Second)
	var state LivenessState
	for time.Now().Before(deadline) {
		if result, ok := monitor.Result(peer.PublicKey); ok {
			state = result.State
			if state == LivenessOnline {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if state != LivenessOnline {
		t.Fatalf("对本机未监听端口探测应判定在线，实际状态 %q", state)
	}
	if online, _, _ := monitor.Counts(); online != 1 {
		t.Fatalf("在线计数应为 1，实际 %d", online)
	}
}
