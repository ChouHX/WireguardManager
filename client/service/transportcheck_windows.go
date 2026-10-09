//go:build windows

package service

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
)

// CheckLoopbackTransport exercises the production Windows backend against a
// second native adapter. Only UDP loopback and a temporary split-tunnel prefix
// are used; no account credentials or remote servers participate in this check.
// Call with the application's single-instance lock held on an elevated runner.
func CheckLoopbackTransport(ctx context.Context) (result error) {
	dir, err := os.MkdirTemp("", "wgm-transport-check-")
	if err != nil {
		return err
	}
	// Preserve recovery records if cleanup fails, so they can be inspected.
	defer func() {
		if result == nil {
			_ = os.RemoveAll(dir)
		} else {
			result = fmt.Errorf("%w (recovery directory: %s)", result, dir)
		}
	}()
	backend, err := NewWindowsBackend(dir)
	if err != nil {
		return err
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		return err
	}
	server, err := driver.CreateAdapter("WGM-Transport-Check", "WireGuard", nil)
	if err != nil {
		return err
	}
	defer func() { _ = server.SetAdapterState(driver.AdapterStateDown); _ = server.Close() }()
	var builder driver.ConfigBuilder
	builder.AppendInterface(&driver.Interface{Flags: driver.InterfaceHasPrivateKey | driver.InterfaceHasListenPort | driver.InterfaceReplacePeers, PrivateKey: [32]byte(serverKey.Bytes()), PeerCount: 1})
	builder.AppendPeer(&driver.Peer{Flags: driver.PeerHasPublicKey | driver.PeerHasPresharedKey, PublicKey: [32]byte(clientKey.PublicKey().Bytes()), PresharedKey: psk})
	config, size := builder.Interface()
	err = server.SetConfiguration(config, size)
	runtime.KeepAlive(builder)
	clear(unsafe.Slice((*byte)(unsafe.Pointer(config)), int(size)))
	if err != nil {
		return err
	}
	if err := server.SetAdapterState(driver.AdapterStateUp); err != nil {
		return err
	}
	installed, err := server.Configuration()
	if err != nil {
		return err
	}
	if installed.ListenPort == 0 {
		return errors.New("test server did not allocate a UDP port")
	}
	profile := Profile{ID: "loopback-self-check", AccessOnly: true, Config: Config{
		PrivateKey: [32]byte(clientKey.Bytes()), PublicKey: [32]byte(serverKey.PublicKey().Bytes()), PresharedKey: psk,
		Address: netip.MustParsePrefix("10.254.253.2/32"), BaseRoutes: []netip.Prefix{netip.MustParsePrefix("10.254.253.0/24")},
		Endpoint: fmt.Sprintf("127.0.0.1:%d", installed.ListenPort), Keepalive: 1, MTU: 1420,
	}}
	session, err := backend.Open(ctx, profile)
	if session != nil {
		defer func() { result = errors.Join(result, session.Close()) }()
	}
	if err != nil {
		return err
	}
	// The same access mode as the desktop must not modify LAN forwarding/NAT.
	native := session.(*windowsSession)
	row, err := native.luid.IPInterface(windows.AF_INET)
	if err != nil {
		return err
	}
	if row.ForwardingEnabled || native.restore {
		return errors.New("access mode unexpectedly enabled forwarding or LAN recovery")
	}
	if _, err := os.Stat(backend.journal); !os.IsNotExist(err) {
		return errors.New("access mode unexpectedly created a LAN recovery journal")
	}
	// A cancelled probe context skips ICMP while still sampling native counters.
	// The responder has no IP assigned; its handshake is confirmed by a keepalive.
	sampleCtx, cancel := context.WithCancel(ctx)
	cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		counts, err := session.Sample(sampleCtx)
		if err != nil {
			return err
		}
		remote, err := server.Configuration()
		if err != nil {
			return err
		}
		confirmed := remote.PeerCount == 1 && remote.FirstPeer().LastHandshake != 0
		runtime.KeepAlive(remote)
		if !counts.Handshake.IsZero() && confirmed {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("native loopback handshake did not complete before timeout")
		case <-ticker.C:
		}
	}
}
