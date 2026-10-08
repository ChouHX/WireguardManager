package services

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// 单次探测的判定依据
const (
	probeDetailHandshake   = "handshake"   // 连接成功
	probeDetailRefused     = "refused"     // 对端内核回 RST
	probeDetailTimeout     = "timeout"     // 超时
	probeDetailUnreachable = "unreachable" // 主机不可达
	probeDetailSetupFailed = "setup_failed"
)

// ProbeTCPOnInterface binds the probe socket to the tenant interface, selecting
// its oif policy rule without changing the process or thread's network context.
func ProbeTCPOnInterface(link, target string, timeout time.Duration) (reachable bool, latency time.Duration, detail string) {
	if _, err := TenantTable(link); err != nil || strings.TrimSpace(target) == "" {
		return false, 0, probeDetailSetupFailed
	}
	setupFailed := false
	dialer := net.Dialer{Timeout: timeout, Control: func(network, address string, conn syscall.RawConn) error {
		var bindErr error
		err := conn.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, link)
		})
		if err != nil || bindErr != nil {
			setupFailed = true
		}
		return errors.Join(err, bindErr)
	}}
	start := time.Now()
	conn, err := dialer.Dial("tcp4", strings.TrimSpace(target))
	if setupFailed {
		return false, 0, probeDetailSetupFailed
	}
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
