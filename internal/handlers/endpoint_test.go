package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/services"
)

func TestConfigMissingEndpointHostDoesNotExportCredentials(t *testing.T) {
	fixture := setupPeerConfigFixture(t, "", "10.100.1.1/24", "10.100.1.2")
	config.AppConfig.Network.ServerIP = ""
	if err := database.DB.Model(&fixture.server).Update("server_endpoint", "").Error; err != nil {
		t.Fatal(err)
	}
	// The HTTPS Host is deliberately different: it must not become a UDP endpoint.
	fixture.context.Request.Host = "cdn.example.com"
	GetPeerConfig(fixture.context)
	if fixture.recorder.Code != http.StatusServiceUnavailable {
		t.Fatal(fixture.recorder.Code)
	}
	var result struct {
		Success bool
		Error   struct{ Message string }
		Data    json.RawMessage
	}
	if err := json.Unmarshal(fixture.recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Success || len(result.Data) != 0 || !strings.Contains(result.Error.Message, "network.server_ip") {
		t.Fatal("missing actionable deployment error")
	}
	if strings.Contains(fixture.recorder.Body.String(), fixture.peer.PrivateKey) {
		t.Fatal("invalid export returned private key")
	}
}

func TestConfigUsesConfiguredRelayHostAndTenantPort(t *testing.T) {
	fixture := setupPeerConfigFixture(t, "", "10.100.1.1/24", "10.100.1.2")
	config.AppConfig.Network.ServerIP = "wg.example.com"
	if err := database.DB.Model(&fixture.server).Update("server_endpoint", "").Error; err != nil {
		t.Fatal(err)
	}
	fixture.context.Request.Host = "cdn.example.com"
	GetPeerConfig(fixture.context)
	if fixture.recorder.Code != http.StatusOK {
		t.Fatal(fixture.recorder.Code)
	}
	if !strings.Contains(fixture.recorder.Body.String(), "Endpoint = wg.example.com:51821") {
		t.Fatal("relay host or tenant UDP port lost")
	}
}

func TestPublicHostSettingValidation(t *testing.T) {
	def, _ := services.SettingDefByKey(services.SettingNetworkServerIP)
	for _, value := range []string{"", "https://relay.example", "relay.example:51820"} {
		if _, err := normalizeSettingValue(def, value); err == nil {
			t.Errorf("accepted invalid relay setting %q", value)
		}
	}
	if value, err := normalizeSettingValue(def, " WG.Example.com "); err != nil || value != "wg.example.com" {
		t.Fatal(value, err)
	}
}
