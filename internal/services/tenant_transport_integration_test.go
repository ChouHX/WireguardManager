package services

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"cloud-platform/internal/models"
)

func TestTenantUDPRejectedFixture(t *testing.T) {
	target := os.Getenv("WGM_UDP_REJECTED_TARGET")
	if target == "" {
		t.Skip("remote UDP isolation helper")
	}
	conn, err := net.DialTimeout("udp4", target, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("isolation-probe")); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Read(make([]byte, 64))
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("tunnel UDP reached host listener instead of INPUT rejection: %v", err)
	}
}

// Unlike the overlapping-LAN fixture, the remote UDP socket is born on a
// separate machine behind stateful NAT. Encrypted traffic must cross the WAN,
// host firewall and router in both directions; loopback cannot hide failures.
func TestTenantNATTransportIntegration(t *testing.T) {
	if os.Getenv("WGM_ISOLATED_NETWORK_TEST") != "1" {
		t.Skip("requires disposable network test container")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// Preserve the other fixtures' rules, inside the disposable container only.
	rules := run("iptables-save")
	t.Cleanup(func() {
		cmd := exec.Command("iptables-restore")
		cmd.Stdin = strings.NewReader(rules + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restore fixture firewall: %v: %s", err, out)
		}
	})
	for _, ns := range []string{"fixture-nat", "fixture-nat-client"} {
		run("ip", "netns", "add", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
		run("ip", "-n", ns, "link", "set", "lo", "up")
	}
	nat := func(name string, args ...string) string {
		return run("ip", append([]string{"netns", "exec", "fixture-nat", name}, args...)...)
	}
	client := func(name string, args ...string) string {
		return run("ip", append([]string{"netns", "exec", "fixture-nat-client", name}, args...)...)
	}
	run("ip", "link", "add", "fixture-wan", "type", "veth", "peer", "name", "wan", "netns", "fixture-nat")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "fixture-wan").Run() })
	run("ip", "addr", "add", "198.18.0.1/24", "dev", "fixture-wan")
	run("ip", "link", "set", "fixture-wan", "up")
	run("ip", "route", "add", "default", "via", "198.18.0.2", "dev", "fixture-wan")
	nat("ip", "addr", "add", "198.18.0.2/24", "dev", "wan")
	nat("ip", "link", "set", "wan", "up")
	nat("ip", "link", "add", "lan", "type", "veth", "peer", "name", "eth0", "netns", "fixture-nat-client")
	nat("ip", "addr", "add", "192.0.2.1/24", "dev", "lan")
	nat("ip", "link", "set", "lan", "up")
	nat("sysctl", "-w", "net.ipv4.ip_forward=1")
	nat("iptables", "-P", "FORWARD", "DROP")
	nat("iptables", "-A", "FORWARD", "-i", "lan", "-o", "wan", "-j", "ACCEPT")
	nat("iptables", "-A", "FORWARD", "-i", "wan", "-o", "lan", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
	nat("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", "wan", "-j", "MASQUERADE")
	client("ip", "addr", "add", "192.0.2.2/24", "dev", "eth0")
	client("ip", "link", "set", "eth0", "up")
	client("ip", "route", "add", "default", "via", "192.0.2.1")
	// Model 1Panel without manually opening the new tenant's port. Provisioning
	// must admit its UDP listener ahead of this drop, without opening other ports.
	run("iptables", "-P", "FORWARD", "DROP")
	run("iptables", "-A", "INPUT", "-i", "fixture-wan", "-p", "udp", "-j", "DROP")
	run("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "MASQUERADE")
	run("sysctl", "-w", "net.ipv4.conf.fixture-wan.rp_filter=1")

	service := NewUserNetworkService(t.TempDir(), "10.200", 51826, "fixture-wan", 1380)
	priv, pub, err := service.wireguardService.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := &models.WireguardServer{WgInterface: "wgm6", WgAddress: tunnelAddress(6), WgPort: 51826, WgPrivateKey: priv, WgPublicKey: pub, NetworkMode: NetworkModeMultiInterface, Enabled: true}
	if err := service.createNetwork(server, "nat-transport"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.interfaceService.Destroy(server.WgInterface, server.WgAddress) })
	priv, pub, err = service.wireguardService.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "client-key")
	if err := os.WriteFile(key, []byte(priv), 0600); err != nil {
		t.Fatal(err)
	}
	client("ip", "link", "add", "vpn", "type", "wireguard")
	client("wg", "set", "vpn", "private-key", key, "peer", server.WgPublicKey, "allowed-ips", "10.100.6.0/24", "endpoint", "198.18.0.1:51826", "persistent-keepalive", "1")
	client("ip", "addr", "add", "10.100.6.2/32", "dev", "vpn")
	client("ip", "addr", "add", "192.168.9.100/32", "dev", "lo")
	client("iptables", "-A", "INPUT", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
	client("iptables", "-A", "INPUT", "-i", "vpn", "-s", "10.100.6.0/24", "-j", "ACCEPT")
	client("iptables", "-A", "INPUT", "-i", "lo", "-j", "ACCEPT")
	client("iptables", "-P", "INPUT", "DROP")
	const allowed = "10.100.6.2/32,192.168.9.0/24"
	if err := service.wireguardService.AddPeer("wgm6", pub, allowed, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.interfaceService.AddRouteForPeer("wgm6", allowed); err != nil {
		t.Fatal(err)
	}
	psk, err := service.wireguardService.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	pskPath := filepath.Join(t.TempDir(), "psk")
	if err := os.WriteFile(pskPath, []byte(psk), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.wireguardService.SetPeerPresharedKey("wgm6", pub, psk); err != nil {
		t.Fatal(err)
	}
	client("wg", "set", "vpn", "peer", server.WgPublicKey, "preshared-key", pskPath)
	// Provision the server before the client's first keepalive/handshake. A
	// failed first attempt otherwise waits for WireGuard's five-second retry.
	client("ip", "link", "set", "vpn", "up")
	client("ip", "route", "add", "10.100.6.0/24", "dev", "vpn")
	client("ping", "-c", "3", "-W", "2", "10.100.6.1")
	client("ping", "-I", "192.168.9.100", "-c", "1", "-W", "2", "10.100.6.1")
	run("ping", "-c", "3", "-W", "2", "10.100.6.2")
	run("ping", "-I", "wgm6", "-c", "1", "-W", "2", "192.168.9.100")
	// Admitting encrypted UDP must never admit the same port from inside a tenant.
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("ip", "netns", "exec", "fixture-nat-client", binary, "-test.run=^TestTenantUDPRejectedFixture$")
	probe.Env = append(os.Environ(), "WGM_UDP_REJECTED_TARGET=10.100.6.1:51826")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("UDP admission bypassed tenant isolation: %v %s", err, out)
	}
	if endpoint := run("wg", "show", "wgm6", "endpoints"); !strings.Contains(endpoint, "198.18.0.2:") {
		t.Fatalf("peer did not use translated WAN endpoint: %s", endpoint)
	}
	stats, err := service.wireguardService.GetDetailedStats("wgm6")
	if err != nil || stats.PublicKey != server.WgPublicKey || stats.ListenPort != server.WgPort {
		t.Fatalf("live interface statistics disagree with provisioned configuration: %v", err)
	}
	for _, transfers := range []string{run("wg", "show", "wgm6", "transfer"), client("wg", "show", "vpn", "transfer")} {
		var key string
		var rx, tx uint64
		if _, err := fmt.Sscan(transfers, &key, &rx, &tx); err != nil || rx == 0 || tx == 0 {
			t.Fatalf("missing bidirectional transport: %q", transfers)
		}
	}
	if script := os.Getenv("WGM_NETWORK_DIAGNOSTICS_SCRIPT"); script != "" {
		out, err := exec.Command("sh", script, "wgm6", "10.100.6.2", "0").CombinedOutput()
		for _, secret := range []string{priv, server.WgPrivateKey, psk} {
			if strings.Contains(string(out), secret) {
				t.Fatal("diagnostic report exposed private key material")
			}
		}
		if err != nil || !strings.Contains(string(out), pub+" enabled") || !strings.Contains(string(out), "198.18.0.2:") {
			t.Fatalf("diagnostic report missing safe transport details: %v", err)
		}
	}
}
