package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func fixtureConfig() string {
	key := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	return "[Interface]\nPrivateKey = " + key + "\nAddress = 10.100.1.3/32\nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = " + key + "\nEndpoint = relay.example:51821\nAllowedIPs = 10.100.1.0/24\nPersistentKeepalive = 25\n"
}
func TestImportSplitTunnel(t *testing.T) {
	cfg, err := ParseConfig("# WGM-Device-LAN = 192.168.1.0/24\n" + fixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := Profile{Config: cfg, Targets: "192.168.0.100, 192.168.2.7/24,192.168.0.100"}
	routes, err := p.Routes()
	if err != nil {
		t.Fatal(err)
	}
	if got := JoinPrefixes(routes); got != "10.100.1.0/24, 192.168.0.100/32, 192.168.2.0/24" {
		t.Fatal(got)
	}
	data, err := json.Marshal(p.View())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "123456789") || strings.Contains(string(data), "Key") {
		t.Fatal("view leaked key material")
	}
	if !strings.Contains(string(data), "192.168.1.0/24") {
		t.Fatal("lost device LAN")
	}
}
func TestRejectDangerousConfig(t *testing.T) {
	tests := map[string]string{
		"hooks":                  strings.Replace(fixtureConfig(), "[Peer]", "PostUp = echo test\n[Peer]", 1),
		"duplicate":              fixtureConfig() + "[Peer]\n",
		"bad key":                strings.Replace(fixtureConfig(), "PrivateKey = ", "PrivateKey = secret-invalid", 1),
		"default route":          strings.Replace(fixtureConfig(), "AllowedIPs = 10.100.1.0/24", "AllowedIPs = 0.0.0.0/0", 1),
		"remote route in export": strings.Replace(fixtureConfig(), "AllowedIPs = 10.100.1.0/24", "AllowedIPs = 10.100.1.0/24, 192.168.0.0/24", 1),
		"missing endpoint":       strings.Replace(fixtureConfig(), "Endpoint = relay.example:51821\n", "", 1),
		"bad tunnel mask":        strings.Replace(fixtureConfig(), "Address = 10.100.1.3/32", "Address = 10.100.1.3/24", 1),
		"overlapping lan":        "# WGM-Device-LAN = 10.100.1.0/24\n" + fixtureConfig(),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseConfig(raw)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if strings.Contains(err.Error(), "secret-invalid") {
				t.Fatal("error leaked secret")
			}
		})
	}
}
func TestLocalTargets(t *testing.T) {
	cfg, err := ParseConfig("# WGM-Device-LAN = 192.168.1.0/24\n" + fixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"0.0.0.0/0", "::/0", "127.0.0.1", "224.0.0.1", "192.168.1.100", "10.100.1.9", "bad"} {
		if _, err := (Profile{Config: cfg, Targets: raw}).Routes(); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestMissingEndpointHostExplainsServerSetting(t *testing.T) {
	raw := strings.Replace(fixtureConfig(), "relay.example:51821", ":51820", 1)
	_, err := ParseConfig(raw)
	if err == nil || !strings.Contains(err.Error(), "Endpoint") || !strings.Contains(err.Error(), "系统设置") || !strings.Contains(err.Error(), "公网") {
		t.Fatalf("missing actionable error: %v", err)
	}
}

func TestFieldErrorsExplainCauseWithoutExposingValues(t *testing.T) {
	for _, tc := range []struct{ field, original, replacement, reason string }{
		{"Endpoint", "relay.example:51821", "https://sensitive-value.example:51820", "UDP"},
		{"Endpoint", "relay.example:51821", "relay.example:sensitive-value", "端口"},
		{"Address", "10.100.1.3/32", "sensitive-value", "/32"},
		{"AllowedIPs", "10.100.1.0/24", "sensitive-value", "IPv4"},
		{"PersistentKeepalive", "PersistentKeepalive = 25", "PersistentKeepalive = sensitive-value", "保活"},
	} {
		t.Run(tc.field+tc.reason, func(t *testing.T) {
			_, err := ParseConfig(strings.Replace(fixtureConfig(), tc.original, tc.replacement, 1))
			if err == nil || !strings.Contains(err.Error(), tc.field) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("missing reason: %v", err)
			}
			if strings.Contains(err.Error(), "sensitive-value") {
				t.Fatal("raw configuration value leaked")
			}
		})
	}
	for _, field := range []string{"PrivateKey", "PublicKey"} {
		key := strings.Split(strings.Split(fixtureConfig(), field+" = ")[1], "\n")[0]
		_, err := ParseConfig(strings.Replace(fixtureConfig(), field+" = "+key, field+" = sensitive-value", 1))
		if err == nil || !strings.Contains(err.Error(), "密钥") || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("unsafe key error: %v", err)
		}
	}
}
