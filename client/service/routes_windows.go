//go:build windows

package service

import (
	"errors"
	"fmt"
	"net/netip"
	"runtime"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
)

// SetRoutes operates only on this session's adapter. On an OS/driver error the
// manager closes the session, so partially applied routes cannot remain active.
func (s *windowsSession) SetRoutes(routes []netip.Prefix) error {
	if s.adapter == nil {
		return errors.New("隧道已关闭")
	}
	for _, route := range routes {
		if route.Contains(s.endpointIP) {
			return errors.New("目标路由覆盖云端 Endpoint，会形成路由环路")
		}
	}
	old, next := map[netip.Prefix]bool{}, map[netip.Prefix]bool{}
	for _, route := range s.routes {
		old[route] = true
	}
	for _, route := range routes {
		next[route] = true
	}
	for _, route := range s.routes {
		if next[route] {
			continue
		}
		err := s.luid.DeleteRoute(route, netip.IPv4Unspecified())
		if err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("删除本机路由 %s 失败：%w", route, err)
		}
	}
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{PeerCount: 1})
	builder.AppendPeer(&driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerUpdateOnly | driver.PeerReplaceAllowedIPs, PublicKey: s.peerKey, AllowedIPsCount: uint32(len(routes))})
	for _, route := range routes {
		allowed := driver.AllowedIP{AddressFamily: windows.AF_INET, Cidr: uint8(route.Bits())}
		ip := route.Addr().As4()
		copy(allowed.Address[:], ip[:])
		builder.AppendAllowedIP(&allowed)
	}
	config, size := builder.Interface()
	err := s.adapter.SetConfiguration(config, size)
	runtime.KeepAlive(builder)
	if err != nil {
		return fmt.Errorf("更新 WireGuard 允许访问地址失败：%w", err)
	}
	for _, route := range routes {
		if old[route] {
			if _, err := s.luid.Route(route, netip.IPv4Unspecified()); err == nil {
				continue
			}
		}
		if err := s.luid.AddRoute(route, netip.IPv4Unspecified(), 0); err != nil {
			return fmt.Errorf("添加本机路由 %s 失败：%w", route, err)
		}
	}
	if err := s.verifyRoutes(routes); err != nil {
		return err
	}
	s.routes = append([]netip.Prefix(nil), routes...)
	return nil
}

// Read back both layers: a Windows route alone is insufficient if WireGuard's
// cryptokey routing does not allow the destination and the returning source.
func (s *windowsSession) verifyRoutes(routes []netip.Prefix) error {
	config, err := s.adapter.Configuration()
	if err != nil {
		return fmt.Errorf("读取 WireGuard 允许访问地址失败：%w", err)
	}
	defer runtime.KeepAlive(config)
	if config.PeerCount != 1 {
		return errors.New("路由核验失败：WireGuard Peer 数量不一致")
	}
	peer := config.FirstPeer()
	if peer.PublicKey != s.peerKey || peer.AllowedIPsCount != uint32(len(routes)) {
		return errors.New("路由核验失败：WireGuard 允许访问地址不一致")
	}
	allowed := map[netip.Prefix]bool{}
	ip := peer.FirstAllowedIP()
	for i := uint32(0); i < peer.AllowedIPsCount; i++ {
		if ip.AddressFamily != windows.AF_INET {
			return errors.New("路由核验失败：非 IPv4 允许访问地址")
		}
		allowed[netip.PrefixFrom(netip.AddrFrom4([4]byte(ip.Address[:4])), int(ip.Cidr))] = true
		ip = ip.NextAllowedIP()
	}
	for _, route := range routes {
		if !allowed[route] {
			return fmt.Errorf("WireGuard 缺少允许访问地址 %s", route)
		}
		row, err := s.luid.Route(route, netip.IPv4Unspecified())
		if err != nil {
			return fmt.Errorf("读取本机路由 %s 失败：%w", route, err)
		}
		if row.InterfaceLUID != s.luid {
			return fmt.Errorf("本机路由 %s 未指向 WireGuard 网卡", route)
		}
	}
	return nil
}
