package services

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud-platform/internal/models"
)

// Both remote clients use the same source IP and port, producing identical TCP
// tuples in separate tenants. CT zones must keep their connections independent.
func TestTenantClientFixture(t *testing.T) {
	expected := os.Getenv("WGM_CLIENT_FIXTURE")
	if expected == "" {
		t.Skip("remote client helper")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 43123}}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", addr)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("http://192.168.0.100:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != expected {
		t.Fatalf("wrong tenant: %q, %v", body, err)
	}
}

// Run only inside the disposable --network none container documented in scripts.
// Namespaces below represent remote customer machines, never cloud-side tenants.
func TestTenantHTTPFixture(t *testing.T) {
	marker := os.Getenv("WGM_HTTP_FIXTURE")
	if marker == "" {
		t.Skip("remote endpoint helper")
	}
	if err := http.ListenAndServe("192.168.0.100:8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, marker)
	})); err != nil {
		t.Fatal(err)
	}
}

func TestMultiInterfaceIntegration(t *testing.T) {
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
	// Ensure an existing permissive host firewall cannot bypass our guard.
	run("iptables", "-A", "FORWARD", "-j", "ACCEPT")
	mainBefore := run("ip", "-4", "route", "show", "table", "main")
	service := NewUserNetworkService(t.TempDir(), "10.200", 51820, "eth0", 1380)
	wg := service.wireguardService
	network := service.interfaceService
	var servers []*models.WireguardServer
	tenantPeers := map[int][]models.WireguardPeer{}
	for id := 1; id <= 2; id++ {
		priv, pub, err := wg.GenerateKeys()
		if err != nil {
			t.Fatal(err)
		}
		server := &models.WireguardServer{WgInterface: TenantInterface(id), WgAddress: tunnelAddress(id), WgPort: 51819 + id, WgPrivateKey: priv, WgPublicKey: pub, NetworkMode: NetworkModeMultiInterface, Enabled: true}
		if err := service.createNetwork(server, fmt.Sprintf("tenant%d", id)); err != nil {
			t.Fatal(err)
		}
		servers = append(servers, server)
		t.Cleanup(func() { _ = network.Destroy(server.WgInterface, server.WgAddress) })
		for n, role := range []string{"gateway", "client"} {
			ns := fmt.Sprintf("fixture-%s-%d", role, id)
			link := fmt.Sprintf("remote%d%d", id, n)
			addr := fmt.Sprintf("10.100.%d.%d", id, n+2)
			priv, pub, err := wg.GenerateKeys()
			if err != nil {
				t.Fatal(err)
			}
			keyPath := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(keyPath, []byte(priv), 0600); err != nil {
				t.Fatal(err)
			}
			run("ip", "netns", "add", ns)
			t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
			run("ip", "link", "add", link, "type", "wireguard")
			run("ip", "link", "set", link, "netns", ns)
			run("ip", "netns", "exec", ns, "wg", "set", link, "private-key", keyPath, "peer", server.WgPublicKey, "allowed-ips", "0.0.0.0/0", "endpoint", fmt.Sprintf("127.0.0.1:%d", server.WgPort), "persistent-keepalive", "1")
			run("ip", "-n", ns, "addr", "add", addr+"/32", "dev", link)
			run("ip", "-n", ns, "link", "set", "lo", "up")
			run("ip", "-n", ns, "link", "set", link, "up")
			run("ip", "-n", ns, "route", "add", "default", "dev", link)
			allowed := addr + "/32"
			if role == "gateway" {
				allowed += ",192.168.0.0/24"
				run("ip", "-n", ns, "addr", "add", "192.168.0.100/32", "dev", "lo")
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("ip", "netns", "exec", ns, binary, "-test.run=^TestTenantHTTPFixture$")
				cmd.Env = append(os.Environ(), fmt.Sprintf("WGM_HTTP_FIXTURE=tenant%d", id))
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			} else {
				allowed += ",192.168.1.0/24"
				run("ip", "-n", ns, "addr", "add", "192.168.1.100/32", "dev", "lo")
			}
			tenantPeers[id] = append(tenantPeers[id], models.WireguardPeer{PublicKey: pub, PeerAddress: addr, AllowedIPs: allowed})
			if err := wg.AddPeer(server.WgInterface, pub, allowed, ""); err != nil {
				t.Fatal(err)
			}
			if err := network.AddRouteForPeer(server.WgInterface, allowed); err != nil {
				t.Fatal(err)
			}
		}
	}
	fetch := func(id int) string {
		t.Helper()
		var out []byte
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			out, err = exec.Command("ip", "netns", "exec", fmt.Sprintf("fixture-client-%d", id), "wget", "-q", "-T", "2", "-O", "-", "http://192.168.0.100:8080").CombinedOutput()
			if err == nil {
				return strings.TrimSpace(string(out))
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("tenant %d HTTP failed: %v: %s", id, err, out)
		return ""
	}
	for id := 1; id <= 2; id++ {
		if got := fetch(id); got != fmt.Sprintf("tenant%d", id) {
			t.Fatalf("tenant %d reached wrong LAN: %s", id, got)
		}
		run("ip", "netns", "exec", fmt.Sprintf("fixture-client-%d", id), "ping", "-c", "1", "-W", "2", fmt.Sprintf("10.100.%d.1", id))
		// Ordinary host applications do not bind a source address or interface.
		// They must still find the uniquely allocated client tunnel address.
		target := fmt.Sprintf("10.100.%d.3", id)
		localRoute := run("ip", "-4", "route", "get", target)
		if !strings.Contains(localRoute, "dev "+TenantInterface(id)) {
			t.Fatalf("unbound server traffic misses tenant: %s", localRoute)
		}
		run("ping", "-c", "1", "-W", "2", target)
		route := run("ip", "-4", "route", "get", "192.168.0.100", "from", fmt.Sprintf("10.100.%d.3", id), "iif", TenantInterface(id))
		if !strings.Contains(route, "dev "+TenantInterface(id)) || !strings.Contains(route, fmt.Sprintf("table %d", 20000+id)) {
			t.Fatalf("wrong route: %s", route)
		}
		ok, _, detail := ProbeTCPOnInterface(TenantInterface(id), fmt.Sprintf("10.100.%d.2:49151", id), 3*time.Second)
		if !ok {
			t.Log(run("ip", "-4", "route", "get", fmt.Sprintf("10.100.%d.2", id), "oif", TenantInterface(id)))
			t.Log(run("iptables", "-nvL", "INPUT"))
			t.Log(run("iptables", "-t", "raw", "-nvL"))
			t.Log(run("ip", "netns", "exec", fmt.Sprintf("fixture-gateway-%d", id), "ip", "route", "get", fmt.Sprintf("10.100.%d.1", id)))
			t.Fatalf("tenant %d probe failed: %s", id, detail)
		}
	}
	if got := run("ip", "-4", "route", "show", "table", "main"); got != mainBefore {
		t.Fatalf("main route pollution: before=%s after=%s", mainBefore, got)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var clients []*exec.Cmd
	for id := 1; id <= 2; id++ {
		cmd := exec.Command("ip", "netns", "exec", fmt.Sprintf("fixture-client-%d", id), binary, "-test.run=^TestTenantClientFixture$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("WGM_CLIENT_FIXTURE=tenant%d", id))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, cmd)
	}
	for _, cmd := range clients {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("overlapping TCP tuple failed: %v", err)
		}
	}
	// Reapply configuration: no duplicate policy rules or firewall jumps.
	for i := 0; i < 2; i++ {
		if err := network.ConfigureRouting("wgm1", tunnelAddress(1)); err != nil {
			t.Fatal(err)
		}
	}
	if count := strings.Count(run("ip", "-4", "rule", "show"), "iif wgm1"); count != 1 {
		t.Fatalf("duplicate iif rule: %d", count)
	}
	if count := strings.Count(run("iptables", "-S", "FORWARD"), "-j WGM-FORWARD"); count != 1 {
		t.Fatalf("duplicate firewall jump: %d", count)
	}
	if got := fetch(1); got != "tenant1" {
		t.Fatal(got)
	}
	// Deliberately misroute A into B: firewall must still reject it, even with
	// an existing global ACCEPT rule and valid WireGuard destination in B.
	run("ip", "-4", "route", "add", "10.100.2.0/24", "dev", "wgm2", "table", "20001")
	for _, target := range []string{"10.100.2.3", "10.100.2.1", "1.1.1.1"} {
		if out, err := exec.Command("ip", "netns", "exec", "fixture-client-1", "ping", "-c", "1", "-W", "1", target).CombinedOutput(); err == nil {
			t.Fatalf("isolation failed for %s: %s", target, out)
		}
	}
	if err := network.ApplyRateLimit("wgm1", 10, 10); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run("tc", "qdisc", "show", "dev", "wgm1"), "tbf") {
		t.Fatal("download limit missing")
	}
	if err := network.SetLinkState("wgm1", false); err != nil {
		t.Fatal(err)
	}
	if HostUDPPortInUse(51820) {
		t.Fatal("disabled tenant still listens")
	}
	if err := network.SetTenantEnabled(servers[0], tenantPeers[1], true); err != nil {
		t.Fatal(err)
	}
	if got := fetch(1); got != "tenant1" {
		t.Fatal(got)
	}
	if err := network.Destroy("wgm1", tunnelAddress(1)); err != nil {
		t.Fatal(err)
	}
	if network.LinkExists("wgm1") || HostUDPPortInUse(51820) {
		t.Fatal("deleted tenant still exists")
	}
	if strings.Contains(run("ip", "-4", "rule", "show"), "wgm1") {
		t.Fatal("deleted tenant policy leaked")
	}
	for _, table := range []string{"raw", "nat", "filter"} {
		if strings.Contains(run("iptables", "-t", table, "-S"), "wgm1") {
			t.Fatalf("deleted tenant firewall leaked in %s", table)
		}
	}
	if got := fetch(2); got != "tenant2" {
		t.Fatalf("deleting A broke B: %s", got)
	}
	if err := network.Destroy("wgm1", tunnelAddress(1)); err != nil {
		t.Fatalf("cleanup is not idempotent: %v", err)
	}
}

func TestLegacyMigrationIntegration(t *testing.T) {
	if os.Getenv("WGM_ISOLATED_NETWORK_TEST") != "1" {
		t.Skip("requires disposable network test container")
	}
	service := NewUserNetworkService(t.TempDir(), "10.200", 51820, "eth0", 1380)
	for n, enabled := range []bool{true, false} {
		id := n + 3
		uid := fmt.Sprintf("legacy%d", id)
		ns := "wg_" + uid
		run := func(name string, args ...string) string {
			t.Helper()
			out, err := exec.Command(name, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("%s %v: %v: %s", name, args, err, out)
			}
			return strings.TrimSpace(string(out))
		}
		priv, pub, err := service.wireguardService.GenerateKeys()
		if err != nil {
			t.Fatal(err)
		}
		server := &models.WireguardServer{Namespace: ns, WgInterface: "wg0", WgAddress: tunnelAddress(id), WgPort: 51900 + id, WgPrivateKey: priv, WgPublicKey: pub, Enabled: enabled, DownloadRate: 20, UploadRate: 30}
		key := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(key, []byte(priv), 0600); err != nil {
			t.Fatal(err)
		}
		run("ip", "netns", "add", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
		run("ip", "link", "add", "oldwg", "type", "wireguard")
		run("ip", "link", "set", "oldwg", "netns", ns)
		run("ip", "-n", ns, "link", "set", "oldwg", "name", "wg0")
		run("ip", "netns", "exec", ns, "wg", "set", "wg0", "private-key", key, "listen-port", fmt.Sprint(server.WgPort))
		if enabled {
			run("ip", "-n", ns, "link", "set", "wg0", "up")
		}
		for retry := 0; retry < 2; retry++ {
			if err := service.EnsureUserNetwork(server, uid); err != nil {
				t.Fatal(err)
			}
			if server.WgPrivateKey != priv || server.WgPublicKey != pub || server.WgPort != 51900+id || server.WgAddress != tunnelAddress(id) || server.Enabled != enabled || server.DownloadRate != 20 || server.UploadRate != 30 {
				t.Fatal("migration changed existing tenant configuration")
			}
			if got := run("wg", "show", server.WgInterface, "public-key"); got != pub {
				t.Fatal("migration changed key")
			}
			if server.NetworkMode != NetworkModeMultiInterface || server.WgInterface != TenantInterface(id) {
				t.Fatal("migration metadata missing")
			}
			if HostUDPPortInUse(server.WgPort) != enabled {
				t.Fatal("migration changed enabled state")
			}
		}
		if strings.Contains(run("ip", "netns", "list"), ns) {
			t.Fatal("legacy namespace remains")
		}
		if err := service.DestroyUserNetwork(server, uid); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProvisionRollbackIntegration(t *testing.T) {
	if os.Getenv("WGM_ISOLATED_NETWORK_TEST") != "1" {
		t.Skip("requires disposable network test container")
	}
	service := NewUserNetworkService(t.TempDir(), "10.200", 51820, "eth0", 1380)
	priv, pub, err := service.wireguardService.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server := &models.WireguardServer{WgInterface: "wgm5", WgAddress: tunnelAddress(5), WgPort: 51905, WgPrivateKey: priv, WgPublicKey: pub, Enabled: true}
	realRun := service.interfaceService.run
	service.interfaceService.run = func(name string, args ...string) ([]byte, error) {
		if name == "ip" && strings.Contains(strings.Join(args, " "), "route replace 10.100.5.0/24") {
			return []byte("injected route failure"), fmt.Errorf("injected")
		}
		return realRun(name, args...)
	}
	if err := service.createNetwork(server, "rollback"); err == nil {
		t.Fatal("expected injected failure")
	}
	if service.interfaceService.LinkExists("wgm5") || HostUDPPortInUse(51905) {
		t.Fatal("failed provision left a live interface/socket")
	}
	out, err := exec.Command("ip", "-4", "rule", "show").Output()
	if err != nil || strings.Contains(string(out), "wgm5") {
		t.Fatalf("policy rollback failed: %v %s", err, out)
	}
	for _, table := range []string{"filter", "nat", "raw"} {
		out, err = exec.Command("iptables", "-t", table, "-S").Output()
		if err != nil || strings.Contains(string(out), "wgm5") {
			t.Fatalf("firewall rollback failed: %v %s", err, out)
		}
	}
}
