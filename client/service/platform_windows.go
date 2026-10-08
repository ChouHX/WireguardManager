//go:build windows

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const adapterName = "WGM-Desktop"

var adapterGUID = windows.GUID{Data1: 0xd1f7d63a, Data2: 0x766c, Data3: 0x4869, Data4: [8]byte{0xa9, 0xf2, 0x14, 0x3b, 0x6c, 0xaa, 0x52, 0x75}}

type DPAPI struct{}

func (DPAPI) Protect(data []byte) ([]byte, error)   { return cryptData(data, true) }
func (DPAPI) Unprotect(data []byte) ([]byte, error) { return cryptData(data, false) }
func cryptData(data []byte, encrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("空数据")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	result := append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...)
	clear(unsafe.Slice(out.Data, int(out.Size)))
	return result, nil
}

// The global mutex also prevents two Windows users from modifying forwarding concurrently.
func AcquireInstance() (func(), error) {
	name, _ := windows.UTF16PtrFromString(`Global\WireguardManager.Desktop.6af9212c`)
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return nil, errors.New("客户端已运行，或当前账户无权取得客户端锁")
	}
	return func() { windows.CloseHandle(handle) }, nil
}

type WindowsBackend struct{ journal string }
type forwardingJournal struct {
	Version    int
	AdapterID  string
	Forwarding bool
}

func NewWindowsBackend(dir string) (*WindowsBackend, error) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil, errors.New("请以管理员身份运行客户端")
	}
	if _, err := CheckDriver(); err != nil {
		return nil, err
	}
	b := &WindowsBackend{journal: filepath.Join(dir, "forwarding-recovery.json")}
	if err := b.recover(); err != nil {
		return nil, fmt.Errorf("恢复上次网卡状态失败：%w", err)
	}
	return b, nil
}

func adapterLUID(id string) (winipcfg.LUID, error) {
	guid, err := windows.GUIDFromString(id)
	if err != nil {
		return 0, err
	}
	return winipcfg.LUIDFromGUID(&guid)
}
func (b *WindowsBackend) recover() error {
	raw, err := os.ReadFile(b.journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal forwardingJournal
	if err = json.Unmarshal(raw, &journal); err != nil || journal.Version != 1 {
		return errors.New("恢复记录无效")
	}
	luid, err := adapterLUID(journal.AdapterID)
	if err != nil {
		return fmt.Errorf("找不到原局域网卡，请重新启用该网卡后再运行：%w", err)
	}
	row, err := luid.IPInterface(windows.AF_INET)
	if err != nil {
		return err
	}
	row.ForwardingEnabled = journal.Forwarding
	if err = row.Set(); err != nil {
		return err
	}
	return os.Remove(b.journal)
}

func (b *WindowsBackend) Adapters() ([]AdapterInfo, error) {
	adapters, err := winipcfg.GetAdaptersAddresses(windows.AF_INET, winipcfg.GAAFlagIncludePrefix)
	if err != nil {
		return nil, err
	}
	result := []AdapterInfo{}
	for _, a := range adapters {
		if a.OperStatus != winipcfg.IfOperStatusUp || a.FriendlyName() == adapterName || a.IfType == winipcfg.IfTypeSoftwareLoopback || a.IfType == winipcfg.IfTypeTunnel {
			continue
		}
		guid, err := a.LUID.GUID()
		if err != nil {
			continue
		}
		info := AdapterInfo{ID: guid.String(), Name: a.FriendlyName(), Addresses: []string{}}
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			ip, ok := netip.AddrFromSlice(u.Address.IP())
			if !ok || !ip.Unmap().Is4() {
				continue
			}
			info.Addresses = append(info.Addresses, netip.PrefixFrom(ip.Unmap(), int(u.OnLinkPrefixLength)).String())
		}
		if len(info.Addresses) > 0 {
			result = append(result, info)
		}
	}
	return result, nil
}

func (b *WindowsBackend) Open(ctx context.Context, p Profile) (Session, error) {
	routes, err := p.Routes()
	if err != nil {
		return nil, err
	}
	if err = b.recover(); err != nil {
		return nil, err
	}
	host, port, _ := net.SplitHostPort(p.Config.Endpoint)
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("无法解析云端 IPv4 地址")
	}
	endpointIP := ips[0].Unmap()
	for _, route := range routes {
		if route.Contains(endpointIP) {
			return nil, errors.New("目标路由覆盖云端 Endpoint，会形成路由环路")
		}
	}
	portValue, _ := strconv.ParseUint(port, 10, 16)
	adapters, err := b.Adapters()
	if err != nil {
		return nil, err
	}
	var selected *AdapterInfo
	for i := range adapters {
		if adapters[i].ID == p.AdapterID {
			selected = &adapters[i]
		}
		for _, raw := range adapters[i].Addresses {
			addr, _ := netip.ParsePrefix(raw)
			for _, route := range routes {
				if route.Contains(addr.Addr()) {
					return nil, fmt.Errorf("路由 %s 覆盖本机地址 %s，请使用更具体的远端 /32 目标", route, addr.Addr())
				}
			}
		}
	}
	if len(p.Config.DeviceLANs) > 0 {
		if selected == nil {
			return nil, errors.New("此设备声明了局域网，请选择本机的局域网网卡")
		}
		for _, lan := range p.Config.DeviceLANs {
			matched := false
			for _, raw := range selected.Addresses {
				addr, _ := netip.ParsePrefix(raw)
				if addr.Masked() == lan || lan.Contains(addr.Addr()) {
					matched = true
				}
			}
			if !matched {
				return nil, fmt.Errorf("网卡 %s 不在设备局域网 %s 内", selected.Name, lan)
			}
		}
	}
	// Only adapters created by this process are owned and removed. Never adopt an unknown interface.
	if old, openErr := driver.OpenAdapter(adapterName); openErr == nil {
		old.Close()
		return nil, errors.New("同名隧道仍存在，请关闭其他实例或重启 Windows 后重试")
	}
	adapter, err := driver.CreateAdapter(adapterName, "WireGuard", &adapterGUID)
	if err != nil {
		return nil, fmt.Errorf("创建 WireGuard 网卡失败：%w", err)
	}
	session := &windowsSession{adapter: adapter, luid: winipcfg.LUID(adapter.LUID()), backend: b, source: p.Config.Address.Addr(), probe: p.Config.BaseRoutes[0].Addr().Next()}
	fail := func(cause error) (Session, error) {
		if cleanup := session.Close(); cleanup != nil {
			return session, errors.Join(cause, cleanup)
		}
		return nil, cause
	}
	if err = adapter.SetLogging(driver.AdapterLogOff); err != nil {
		return fail(err)
	}
	peer := driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerHasPersistentKeepalive | driver.PeerHasEndpoint | driver.PeerReplaceAllowedIPs, PublicKey: p.Config.PublicKey, PersistentKeepalive: p.Config.Keepalive, AllowedIPsCount: uint32(len(routes))}
	if p.Config.PresharedKey != [32]byte{} {
		peer.Flags |= driver.PeerHasPresharedKey
		peer.PresharedKey = p.Config.PresharedKey
	}
	if err = peer.Endpoint.SetAddrPort(netip.AddrPortFrom(endpointIP, uint16(portValue))); err != nil {
		return fail(err)
	}
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{Flags: driver.InterfaceHasPrivateKey | driver.InterfaceReplacePeers, PrivateKey: p.Config.PrivateKey, PeerCount: 1})
	builder.AppendPeer(&peer)
	for _, r := range routes {
		allowed := driver.AllowedIP{AddressFamily: windows.AF_INET, Cidr: uint8(r.Bits())}
		ip := r.Addr().As4()
		copy(allowed.Address[:], ip[:])
		builder.AppendAllowedIP(&allowed)
	}
	config, size := builder.Interface()
	err = adapter.SetConfiguration(config, size)
	runtime.KeepAlive(builder)
	// The driver now owns the key material; clear the temporary native configuration buffer.
	clear(unsafe.Slice((*byte)(unsafe.Pointer(config)), int(size)))
	if err != nil {
		return fail(err)
	}
	if err = adapter.SetAdapterState(driver.AdapterStateUp); err != nil {
		return fail(err)
	}
	row, err := session.luid.IPInterface(windows.AF_INET)
	if err != nil {
		return fail(err)
	}
	row.ForwardingEnabled = true
	row.UseAutomaticMetric = false
	row.Metric = 1
	row.NLMTU = p.Config.MTU
	row.DadTransmits = 0
	if err = row.Set(); err != nil {
		return fail(err)
	}
	if err = session.luid.AddIPAddress(p.Config.Address); err != nil {
		return fail(err)
	}
	for _, route := range routes {
		if err = session.luid.AddRoute(route, netip.IPv4Unspecified(), 0); err != nil {
			return fail(fmt.Errorf("添加路由 %s 失败：%w", route, err))
		}
	}
	if selected != nil {
		lan, err := adapterLUID(selected.ID)
		if err != nil {
			return fail(err)
		}
		row, err := lan.IPInterface(windows.AF_INET)
		if err != nil {
			return fail(err)
		}
		data, _ := json.Marshal(forwardingJournal{1, selected.ID, row.ForwardingEnabled})
		// Journal before mutation: after a crash the next launch restores the original value.
		if err = AtomicWrite(b.journal, data); err != nil {
			return fail(err)
		}
		session.restore = true
		row.ForwardingEnabled = true
		if err = row.Set(); err != nil {
			return fail(err)
		}
	}
	return session, nil
}

type windowsSession struct {
	adapter       *driver.Adapter
	luid          winipcfg.LUID
	backend       *WindowsBackend
	restore       bool
	source, probe netip.Addr
}

func (s *windowsSession) Close() error {
	// WireGuardCloseAdapter is a VOID API and consumes the handle. Verify cleanup
	// through the OS route table instead of interpreting the wrapper's return register.
	if s.adapter != nil {
		_ = s.adapter.SetAdapterState(driver.AdapterStateDown)
		_ = s.adapter.Close()
		s.adapter = nil
	}
	var cleanupErr error
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		cleanupErr = err
	} else {
		for i := range rows {
			if rows[i].InterfaceLUID != s.luid {
				continue
			}
			if err := rows[i].Delete(); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}
	if s.restore {
		if err := s.backend.recover(); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			s.restore = false
		}
	}
	return cleanupErr
}
func (s *windowsSession) Sample(ctx context.Context) (Counters, error) {
	if s.adapter == nil {
		return Counters{}, errors.New("隧道已关闭")
	}
	config, err := s.adapter.Configuration()
	if err != nil {
		return Counters{}, err
	}
	if config.PeerCount != 1 {
		return Counters{}, errors.New("隧道 Peer 状态异常")
	}
	peer := config.FirstPeer()
	c := Counters{Rx: peer.RxBytes, Tx: peer.TxBytes, LatencyMS: -1}
	if peer.LastHandshake != 0 {
		// FILETIME is in 100 ns ticks from 1601-01-01.
		ticks := peer.LastHandshake - 116444736000000000
		c.Handshake = time.Unix(int64(ticks/10000000), int64(ticks%10000000)*100)
	}
	runtime.KeepAlive(config)
	// This probe is source-bound to the active tunnel. A timeout is displayed as no latency,
	// not zero and not a successful connection. The driver handshake determines connection state.
	if ctx.Err() == nil {
		c.LatencyMS = icmpLatency(s.source, s.probe)
	}
	return c, nil
}
