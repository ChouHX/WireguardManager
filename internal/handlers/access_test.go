package handlers

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"github.com/gin-gonic/gin"
)

func desktopPublicKey(t *testing.T) string {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}
func TestAccessRegistrationIsIdempotentAndTenantScoped(t *testing.T) {
	f := setupPeerConfigFixture(t, "", "10.100.1.1/24", "10.100.1.2")
	public := desktopPublicKey(t)
	peer, created, err := accessPeer(&f.server, public, "Laptop")
	if err != nil || !created {
		t.Fatal("register", err)
	}
	if peer.PrivateKey != "" || peer.DeviceRole != "access" || peer.PeerAddress != "10.100.1.3" || peer.AllowedIPs != "10.100.1.3/32" || peer.PresharedKey == "" {
		t.Fatal("invalid access identity")
	}
	again, created, err := accessPeer(&f.server, public, "retry")
	if err != nil || created || again.ID != peer.ID || again.PresharedKey != peer.PresharedKey {
		t.Fatal("retry rotated credentials")
	}
	other := f.server
	other.ID++
	if _, _, err = accessPeer(&other, public, "cross tenant"); err == nil {
		t.Fatal("claimed other tenant identity")
	}
	gatewayPublic := desktopPublicKey(t)
	if err = database.DB.Model(&f.peer).Update("public_key", gatewayPublic).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err = accessPeer(&f.server, gatewayPublic, "gateway takeover"); err == nil {
		t.Fatal("claimed gateway identity")
	}
	for _, bad := range []string{"invalid", base64.StdEncoding.EncodeToString(make([]byte, 32))} {
		if _, _, err = accessPeer(&f.server, bad, ""); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	cfg := accessConfig(f.server, peer, "203.0.113.10:51820")
	if strings.Contains(cfg, "PrivateKey") || !strings.Contains(cfg, "Address = 10.100.1.3/32") || !strings.Contains(cfg, "AllowedIPs = 10.100.1.0/24") {
		t.Fatal("wrong desktop configuration")
	}
	// HTTP retry returns the same public identity without needing kernel changes.
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("user", &f.user)
	body, _ := json.Marshal(map[string]string{"public_key": public, "name": "Laptop"})
	c.Request = httptest.NewRequest("POST", "/api/wireguard/access", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	EnsureDesktopAccess(c)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "PrivateKey") {
		t.Fatal("HTTP retry failed", rec.Code)
	}
}
func TestAccessPeerCannotExportOrDeclareGatewayRoutes(t *testing.T) {
	for _, action := range []string{"export", "routes", "psk"} {
		t.Run(action, func(t *testing.T) {
			f := setupPeerConfigFixture(t, "", "10.100.1.1/24", "10.100.1.2")
			database.DB.Model(&f.peer).Updates(map[string]any{"device_role": "access", "private_key": ""})
			if action == "export" {
				GetPeerConfig(f.context)
			} else {
				body := `{"allowed_ips":"192.168.0.0/24"}`
				if action == "psk" {
					body = `{"use_preshared_key":false}`
				}
				f.context.Request = httptest.NewRequest("PATCH", "/api/wireguard/peers/1", strings.NewReader(body))
				f.context.Request.Header.Set("Content-Type", "application/json")
				UpdatePeer(f.context)
			}
			if f.recorder.Code < 400 {
				t.Fatal("access identity allowed gateway operation")
			}
			var saved models.WireguardPeer
			database.DB.First(&saved, f.peer.ID)
			if saved.AllowedIPs != f.peer.AllowedIPs {
				t.Fatal("access routes changed")
			}
		})
	}
}
func TestAccessRegistrationRejectsDisabledServer(t *testing.T) {
	f := setupPeerConfigFixture(t, "", "10.100.1.1/24", "10.100.1.2")
	database.DB.Model(&f.server).Update("enabled", false)
	f.context.Request = httptest.NewRequest("POST", "/api/wireguard/access", strings.NewReader(`{"public_key":"`+desktopPublicKey(t)+`"}`))
	f.context.Request.Header.Set("Content-Type", "application/json")
	EnsureDesktopAccess(f.context)
	if f.recorder.Code != 403 {
		t.Fatal(f.recorder.Code)
	}
	var n int64
	database.DB.Model(&models.WireguardPeer{}).Count(&n)
	if n != 1 {
		t.Fatal("disabled tenant got new peer")
	}
}
