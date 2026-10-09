package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Sandbox all absolute installation paths and network commands. These tests
// exercise fw3/fw4 startup integration without changing the test host network.
func sandboxInstaller(t *testing.T, kind string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "mock-bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"etc/init.d", "etc/systemd/system", "usr/libexec"} {
		os.MkdirAll(filepath.Join(root, dir), 0700)
	}
	script := strings.NewReplacer("/etc/", root+"/etc/", "/usr/libexec", root+"/usr/libexec", "/run/systemd/system", root+"/run/systemd/system").Replace(installer)
	path := filepath.Join(root, "install.sh")
	os.WriteFile(path, []byte(script), 0700)
	mock := `#!/bin/sh
name=${0##*/}
if [ "$name" = sysctl ] && [ "${WGM_MOCK_FAIL:-}" = 1 ]; then exit 1; fi
printf '%s %s\n' "$name" "$*" >> "$WGM_MOCK_LOG"
case "$name:$*" in
 'id:-u') echo 0;;
 'ip:link show dev wgm-gw') test -f "$WGM_MOCK_ROOT/link";exit $?;;
 'ip:link add dev wgm-gw type wireguard') touch "$WGM_MOCK_ROOT/link";;
 'ip:-o link show dev wgm-gw') echo '1: wgm-gw: <UP> alias wireguard-manager-gateway';;
 'ip:link del dev wgm-gw') rm -f "$WGM_MOCK_ROOT/link";;
 'wg:pubkey') cat >/dev/null;echo fixture-public;;
 'uci:-q get '*|'iptables:'*' -S WGM-'*|'nft:list table '*) exit 1;;
 'iptables:'*' -C '*) exit 1;;
 'nft:-f -') cat >> "$WGM_MOCK_LOG";;
esac
exit 0
`
	for _, name := range []string{"id", "ip", "wg", "sysctl", "iptables", "uci", "nft", "systemctl"} {
		os.WriteFile(filepath.Join(bin, name), []byte(mock), 0700)
	}
	if strings.HasPrefix(kind, "fw") {
		os.WriteFile(filepath.Join(root, "etc/openwrt_release"), []byte("fixture"), 0600)
		// rc.common is a dispatcher on a real OpenWrt system.
		os.WriteFile(filepath.Join(root, "etc/rc.common"), []byte("#!/bin/sh\nexit 0\n"), 0700)
		os.WriteFile(filepath.Join(root, "etc/init.d/firewall"), []byte("#!/bin/sh\nexit 0\n"), 0700)
		if kind == "fw4" {
			os.WriteFile(filepath.Join(bin, "fw4"), []byte("#!/bin/sh\nexit 0\n"), 0700)
		}
	} else if kind == "systemd" {
		os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0700)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("WGM_MOCK_ROOT", root)
	t.Setenv("WGM_MOCK_LOG", filepath.Join(root, "commands"))
	config := "[Interface]\nPrivateKey = fixture-key\nAddress = 10.100.1.2/32\n[Peer]\nPublicKey = fixture-public\nAllowedIPs = 10.100.1.0/24\nEndpoint = 203.0.113.1:51820\nPersistentKeepalive = 25\n"
	cfg := filepath.Join(root, "input.conf")
	os.WriteFile(cfg, []byte(config), 0600)
	return root, path, cfg
}
func TestGatewayStartupIntegrations(t *testing.T) {
	for _, kind := range []string{"fw3", "fw4", "systemd"} {
		t.Run(kind, func(t *testing.T) {
			root, path, cfg := sandboxInstaller(t, kind)
			if out, err := exec.Command("sh", path, "install", cfg).CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			log, _ := os.ReadFile(filepath.Join(root, "commands"))
			commands := string(log)
			if strings.HasPrefix(kind, "fw") {
				for _, want := range []string{"firewall.wgm_gateway_zone.device=wgm-gw", "firewall.wgm_gateway_forward.src_ip=10.100.1.0/24", "firewall.wgm_gateway_forward.dest=*", "firewall.wgm_gateway_reload.reload=1"} {
					if !strings.Contains(commands, want) {
						t.Fatal("missing UCI option", want)
					}
				}
			} else if !strings.Contains(commands, "systemctl enable wgm-gateway.service") {
				t.Fatal("missing startup registration")
			}
			if kind == "fw4" && !strings.Contains(commands, `iifname "wgm-gw" oifname != "wgm-gw" ip saddr 10.100.1.0/24 masquerade`) {
				t.Fatal("NAT not scoped to tunnel")
			}
			if out, err := exec.Command("sh", path, "uninstall").CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(root, "etc/wireguard-manager")); !os.IsNotExist(err) {
				t.Fatal("owned files remained")
			}
		})
	}
}
func TestGatewayRejectsHooksDuplicateFieldsAndConflictingServices(t *testing.T) {
	for _, kind := range []string{"hook", "duplicate", "service", "subnet"} {
		t.Run(kind, func(t *testing.T) {
			root, path, cfg := sandboxInstaller(t, "linux")
			raw, _ := os.ReadFile(cfg)
			switch kind {
			case "hook":
				raw = append(raw, []byte("PostUp = touch /tmp/should-never-run\n")...)
			case "duplicate":
				raw = append(raw, []byte("AllowedIPs = 0.0.0.0/0\n")...)
			case "subnet":
				raw = []byte(strings.ReplaceAll(string(raw), "10.100.1.0/24", "0.0.0.0/0"))
			case "service":
				os.WriteFile(filepath.Join(root, "etc/init.d/wgm-gateway"), []byte("owned by someone else"), 0600)
			}
			os.WriteFile(cfg, raw, 0600)
			if err := exec.Command("sh", path, "install", cfg).Run(); err == nil {
				t.Fatal("unsafe/conflicting enrollment accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "etc/wireguard-manager")); !os.IsNotExist(err) {
				t.Fatal("rejected config mutated installation")
			}
		})
	}
}

func TestFailedGatewayInstallRollsBackOwnedResources(t *testing.T) {
	root, path, cfg := sandboxInstaller(t, "systemd")
	t.Setenv("WGM_MOCK_FAIL", "1")
	if err := exec.Command("sh", path, "install", cfg).Run(); err == nil {
		t.Fatal("injected failure ignored")
	}
	for _, name := range []string{"etc/wireguard-manager", "etc/systemd/system/wgm-gateway.service", "usr/libexec/wgm-gateway", "link"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("failed enrollment left resource", name)
		}
	}
}
