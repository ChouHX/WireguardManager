package services

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// netnsMountDir 网络命名空间挂载点（容器内挂载宿主的 /var/run/netns）
const netnsMountDir = "/var/run/netns"

// 单次探测的判定依据
const (
	probeDetailHandshake   = "handshake"   // 连接成功
	probeDetailRefused     = "refused"     // 对端内核回 RST
	probeDetailTimeout     = "timeout"     // 超时
	probeDetailUnreachable = "unreachable" // 主机不可达
	probeDetailSetupFailed = "setup_failed"
)

// ProbeTCPInNamespace 在指定网络命名空间内向 target 发起一次 TCP 连接探测。
//
// 判定与内核协议栈行为一致，无需对端安装任何 Agent：
//   - 连接成功           → 对端存活；
//   - connection refused → 对端内核回送 RST，同样说明对端存活；
//   - 超时 / 主机不可达   → 本次未响应。
//
// 为什么必须在目标命名空间内执行：每台设备的隧道地址（如 10.100.0.2）只在
// 它所属账号的 netns 内可达，宿主机路由表里并没有该网段——从宿主命名空间
// 发包会落到默认路由上，无论对端是否在线都只会得到超时。
func ProbeTCPInNamespace(nsName, target string, timeout time.Duration) (reachable bool, latency time.Duration, detail string) {
	nsName = strings.TrimSpace(nsName)
	target = strings.TrimSpace(target)
	if nsName == "" || target == "" {
		return false, 0, probeDetailSetupFailed
	}

	// 切换命名空间会影响整个线程，必须把当前 goroutine 锁定到独立线程，
	// 并在返回前切回原命名空间，否则该线程会一直留在目标 netns 里。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	originalNamespace, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return false, 0, probeDetailSetupFailed
	}
	defer originalNamespace.Close()

	targetNamespace, err := os.Open(filepath.Join(netnsMountDir, nsName))
	if err != nil {
		return false, 0, probeDetailSetupFailed
	}
	defer targetNamespace.Close()

	if err := unix.Setns(int(targetNamespace.Fd()), unix.CLONE_NEWNET); err != nil {
		return false, 0, probeDetailSetupFailed
	}
	defer func() {
		_ = unix.Setns(int(originalNamespace.Fd()), unix.CLONE_NEWNET)
	}()

	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, timeout)
	latency = time.Since(start)

	if err == nil {
		_ = conn.Close()
		return true, latency, probeDetailHandshake
	}

	// 连接被拒绝：数据已到达对端内核且内核正常回包
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true, latency, probeDetailRefused
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false, latency, probeDetailTimeout
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return false, latency, probeDetailUnreachable
	}

	return false, latency, probeDetailTimeout
}

// ProbeTarget 拼出探测目标：设备隧道地址 + 探测端口。
func ProbeTarget(peerAddress string, port int) string {
	address := strings.TrimSpace(peerAddress)
	if address == "" {
		return ""
	}
	if port <= 0 || port > 65535 {
		port = 49151
	}
	return net.JoinHostPort(address, strconv.Itoa(port))
}
