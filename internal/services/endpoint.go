package services

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// NormalizeWireGuardHost validates the UDP relay host independently of the HTTP
// management address. Never infer this from a request Host / forwarded header:
// the web app may use a CDN which does not carry WireGuard UDP traffic.
func NormalizeWireGuardHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", errors.New("请在系统设置中填写 WireGuard 中继的直连公网 IP / 域名（network.server_ip），不能使用普通 CDN 代理地址")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" || ip == netip.MustParseAddr("255.255.255.255") {
			return "", errors.New("WireGuard 公网地址必须是可连接的单播地址")
		}
		return ip.String(), nil
	}
	name := strings.TrimSuffix(host, ".")
	if len(name) > 253 || name == "" {
		return "", errors.New("WireGuard 公网域名格式无效")
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("WireGuard 公网域名格式无效")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", errors.New("WireGuard 公网地址应填写 IP 或域名，不要填写 http/https、端口或路径")
			}
		}
	}
	return strings.ToLower(host), nil
}

func ClientEndpoint(override, publicHost string, listenPort int) (string, error) {
	host, port := publicHost, listenPort
	if strings.TrimSpace(override) != "" {
		var portText string
		var err error
		host, portText, err = net.SplitHostPort(strings.TrimSpace(override))
		if err != nil {
			return "", errors.New("WireGuard 服务器 Endpoint 格式无效，应为公网 IP / 域名和 UDP 端口")
		}
		port, err = strconv.Atoi(portText)
		if err != nil {
			return "", errors.New("WireGuard 中继 UDP 端口无效")
		}
		// Recover historical port-only overrides once the administrator supplies
		// the missing global host, while preserving any explicit forwarded port.
		if host == "" {
			host = publicHost
		}
	}
	host, err := NormalizeWireGuardHost(host)
	if err != nil {
		return "", err
	}
	if port < 1 || port > 65535 {
		return "", errors.New("WireGuard 中继 UDP 端口必须在 1–65535 之间")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
