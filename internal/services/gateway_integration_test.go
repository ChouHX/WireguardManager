package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cloud-platform/gateway"
	"cloud-platform/internal/models"
)

// All namespaces AND filesystem mutations live inside the disposable container.
// The LAN device has no route to WireGuard: replies prove the bootstrap's NAT.
func TestGatewayBootstrapIntegration(t *testing.T) {
	if os.Getenv("WGM_ISOLATED_NETWORK_TEST") != "1" {
		t.Skip("requires disposable container")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v: %s", name, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for _, ns := range []string{"fixture-gateway", "fixture-access", "fixture-plc"} {
		run("ip", "netns", "add", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
		run("ip", "-n", ns, "link", "set", "lo", "up")
	}
	in := func(ns, name string, args ...string) string {
		return run("ip", append([]string{"netns", "exec", ns, name}, args...)...)
	}
	gw := func(name string, args ...string) string { return in("fixture-gateway", name, args...) }
	access := func(name string, args ...string) string { return in("fixture-access", name, args...) }
	plc := func(name string, args ...string) string { return in("fixture-plc", name, args...) }
	for i, ns := range []string{"fixture-access", "fixture-gateway"} {
		link := fmt.Sprintf("fixture-u%d", i)
		run("ip", "link", "add", link, "type", "veth", "peer", "name", "wan", "netns", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "link", "del", link).Run() })
		run("ip", "addr", "add", fmt.Sprintf("198.18.%d.1/24", 8+i), "dev", link)
		run("ip", "link", "set", link, "up")
		in(ns, "ip", "addr", "add", fmt.Sprintf("198.18.%d.2/24", 8+i), "dev", "wan")
		in(ns, "ip", "link", "set", "wan", "up")
	}
	gw("ip", "link", "add", "lan", "type", "veth", "peer", "name", "eth0", "netns", "fixture-plc")
	gw("ip", "link", "set", "lan", "up")
	plc("ip", "link", "set", "eth0", "up")
	for _, n := range []string{"44", "45"} {
		gw("ip", "addr", "add", "192.168."+n+".1/24", "dev", "lan")
		plc("ip", "addr", "add", "192.168."+n+".100/24", "dev", "eth0")
	}
	gw("iptables", "-P", "FORWARD", "DROP")
	gw("iptables", "-N", "EXISTING-APP")
	gw("iptables", "-A", "FORWARD", "-j", "EXISTING-APP")
	gw("iptables", "-t", "nat", "-N", "EXISTING-NAT")
	gw("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "EXISTING-NAT")
	beforeRules := gw("iptables-save")
	beforeRoutes := gw("ip", "-4", "route", "show", "table", "main")
	svc := NewUserNetworkService(t.TempDir(), "10.200", 51827, "fixture-u0", 1380)
	priv, pub, err := svc.wireguardService.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := &models.WireguardServer{WgInterface: "wgm7", WgAddress: tunnelAddress(7), WgPort: 51827, WgPrivateKey: priv, WgPublicKey: pub, NetworkMode: NetworkModeMultiInterface, Enabled: true}
	if err = svc.createNetwork(server, "bootstrap-test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.interfaceService.Destroy(server.WgInterface, server.WgAddress) })
	dir := t.TempDir()
	var gatewayPub string
	for i, ns := range []string{"fixture-gateway", "fixture-access"} {
		key, public, err := svc.wireguardService.GenerateKeys()
		if err != nil {
			t.Fatal(err)
		}
		address := fmt.Sprintf("10.100.7.%d", i+2)
		if err = svc.wireguardService.AddPeer("wgm7", public, address+"/32", ""); err != nil {
			t.Fatal(err)
		}
		if err = svc.interfaceService.AddRouteForPeer("wgm7", address+"/32"); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			gatewayPub = public
			cfg := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\n\n[Peer]\nPublicKey = %s\nEndpoint = 198.18.9.1:51827\nAllowedIPs = 10.100.7.0/24\nPersistentKeepalive = 1\n", key, address, pub)
			path := filepath.Join(dir, "setup.sh")
			if err = os.WriteFile(path, []byte(gateway.SetupScript(cfg)), 0600); err != nil {
				t.Fatal(err)
			}
			gw("sh", path)
			// Running the same enrollment again must not install duplicate rules.
			gw("sh", path)
		} else {
			path := filepath.Join(dir, "access.key")
			if err = os.WriteFile(path, []byte(key), 0600); err != nil {
				t.Fatal(err)
			}
			access("ip", "link", "add", "vpn", "type", "wireguard")
			access("wg", "set", "vpn", "private-key", path, "peer", pub, "endpoint", "198.18.8.1:51827", "allowed-ips", "10.100.7.0/24,192.168.44.0/24,192.168.45.0/24", "persistent-keepalive", "1")
			access("ip", "addr", "add", address+"/32", "dev", "vpn")
			access("ip", "link", "set", "vpn", "up")
			for _, prefix := range []string{"10.100.7.0/24", "192.168.44.0/24", "192.168.45.0/24"} {
				access("ip", "route", "add", prefix, "dev", "vpn")
			}
		}
		_ = ns
	}
	access("ping", "-c", "2", "-W", "3", "10.100.7.1")
	configBefore, err := os.ReadFile("/etc/wireguard-manager/wireguard.conf")
	if err != nil {
		t.Fatal(err)
	}
	routesBefore := gw("ip", "-4", "route", "show", "table", "main")
	tryPing := func(target string) error {
		return exec.Command("ip", "netns", "exec", "fixture-access", "ping", "-c", "1", "-W", "1", target).Run()
	}
	if tryPing("192.168.44.100") == nil {
		t.Fatal("unassigned LAN was reachable")
	}
	old := "10.100.7.2/32"
	for _, prefix := range []string{"192.168.44.100/32", "192.168.45.0/24"} {
		next := "10.100.7.2/32," + prefix
		if err = svc.wireguardService.SetPeerAllowedIPs("wgm7", gatewayPub, next); err != nil {
			t.Fatal(err)
		}
		if err = svc.interfaceService.ChangePeerRoutes("wgm7", old, next); err != nil {
			t.Fatal(err)
		}
		target := "192.168.44.100"
		if strings.Contains(prefix, "45") {
			target = "192.168.45.100"
		}
		access("ping", "-c", "2", "-W", "3", target)
		old = next
	}
	if tryPing("192.168.44.100") == nil {
		t.Fatal("removed cloud target still reachable")
	}
	configAfter, err := os.ReadFile("/etc/wireguard-manager/wireguard.conf")
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(configBefore) != sha256.Sum256(configAfter) || routesBefore != gw("ip", "-4", "route", "show", "table", "main") {
		t.Fatal("target change modified gateway")
	}
	if !strings.Contains(gw("iptables", "-t", "nat", "-v", "-n", "-L", "WGM-GATEWAY-NAT"), "MASQUERADE") {
		t.Fatal("missing return NAT")
	}
	gw("/usr/libexec/wgm-gateway", "down")
	afterRules := gw("iptables-save")
	// Ignore changing dump timestamps, all rules and counters must otherwise match.
	trim := func(s string) string {
		var lines []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.HasPrefix(l, "#") {
				lines = append(lines, l)
			}
		}
		return regexp.MustCompile(`\[\d+:\d+\]`).ReplaceAllString(strings.Join(lines, "\n"), "[counters]")
	}
	if trim(beforeRules) != trim(afterRules) || beforeRoutes != gw("ip", "-4", "route", "show", "table", "main") {
		t.Fatalf("gateway cleanup changed unrelated networking: rules before=%s after=%s routes before=%s after=%s", trim(beforeRules), trim(afterRules), beforeRoutes, gw("ip", "-4", "route", "show", "table", "main"))
	}
	gw("/usr/libexec/wgm-gateway", "uninstall")
	if _, err = os.Stat("/etc/wireguard-manager"); !os.IsNotExist(err) {
		t.Fatal("enrollment files remained")
	}
	t.Log("cloud-only target add/change/removal, routed LAN replies through NAT, stable gateway config, and unrelated network preservation passed")
}
