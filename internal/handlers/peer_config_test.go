package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// peerConfigFixture 构造一套最小的用户/服务器/peer 数据，用于验证客户端配置生成。
type peerConfigFixture struct {
	user         models.User
	server       models.WireguardServer
	peer         models.WireguardPeer
	context      *gin.Context
	recorder     *httptest.ResponseRecorder
}

func setupPeerConfigFixture(t *testing.T, clientAllowedIPs, serverAddress, peerAddress string) peerConfigFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	config.AppConfig = &config.Config{
		Network: config.NetworkConfig{
			DNS:              "1.1.1.1, 8.8.8.8",
			ServerIP:         "203.0.113.10",
			ClientAllowedIPs: clientAllowedIPs,
		},
	}

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.WireguardServer{}, &models.WireguardPeer{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.DB = db

	user := models.User{Email: "peer@example.com", Name: "peer", Role: models.RoleNormalUser}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	server := models.WireguardServer{
		UserID:         user.ID,
		Namespace:      "wg_" + user.UserUID,
		WgInterface:    "wg0",
		WgPort:         51821,
		WgPublicKey:    "SERVERPUBLICKEY000000000000000000000000000000000=",
		WgPrivateKey:   "SERVERPRIVATEKEY00000000000000000000000000000000=",
		WgAddress:      serverAddress,
		ServerEndpoint: "203.0.113.10:51821",
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("create server: %v", err)
	}

	peer := models.WireguardPeer{
		ServerID:            server.ID,
		PublicKey:           "PEERPUBLICKEY0000000000000000000000000000000000=",
		PrivateKey:          "PEERPRIVATEKEY000000000000000000000000000000000=",
		PeerAddress:         peerAddress,
		AllowedIPs:          peerAddress + "/32",
		PersistentKeepalive: 25,
	}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatalf("create peer: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(peer.ID), 10)}}
	ctx.Request = httptest.NewRequest("GET", "/api/wireguard/peers/"+strconv.FormatUint(uint64(peer.ID), 10)+"/config", nil)
	ctx.Set("user", &user)

	return peerConfigFixture{user: user, server: server, peer: peer, context: ctx, recorder: recorder}
}

// renderedConfig 调用 GetPeerConfig 并取回生成的配置文本。
func renderedConfig(t *testing.T, clientAllowedIPs, serverAddress, peerAddress string) string {
	t.Helper()
	fixture := setupPeerConfigFixture(t, clientAllowedIPs, serverAddress, peerAddress)
	GetPeerConfig(fixture.context)

	var body struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(fixture.recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, fixture.recorder.Body.String())
	}
	if !body.Success {
		t.Fatalf("expected success response, got: %s", fixture.recorder.Body.String())
	}
	return body.Data["config"]
}

func allowedIPsLine(configText string) string {
	for _, line := range strings.Split(configText, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "AllowedIPs") {
			return strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
		}
	}
	return ""
}

func TestClientAllowedIPs(t *testing.T) {
	cases := []struct {
		name             string
		clientAllowedIPs string
		serverAddress    string
		peerAddress      string
		want             string
	}{
		{
			name:          "默认按 peer 所在网段推导（/24）",
			serverAddress: "10.100.0.1/24",
			peerAddress:   "10.100.0.2",
			want:          "10.100.0.0/24",
		},
		{
			name:          "默认推导尊重服务端掩码（/16）",
			serverAddress: "10.100.0.1/16",
			peerAddress:   "10.100.0.2",
			want:          "10.100.0.0/16",
		},
		{
			name:          "服务端地址缺少掩码时回退到 /32",
			serverAddress: "10.100.0.1",
			peerAddress:   "10.100.0.2",
			want:          "10.100.0.2/32",
		},
		{
			name:             "显式配置覆盖默认推导",
			clientAllowedIPs: "10.0.0.0/8",
			serverAddress:    "10.100.0.1/24",
			peerAddress:      "10.100.0.2",
			want:             "10.0.0.0/8",
		},
		{
			name:             "显式配置全局代理",
			clientAllowedIPs: "0.0.0.0/0, ::/0",
			serverAddress:    "10.100.0.1/24",
			peerAddress:      "10.100.0.2",
			want:             "0.0.0.0/0, ::/0",
		},
		{
			name:          "IPv6 peer 不使用 IPv4 掩码",
			serverAddress: "10.100.0.1/24",
			peerAddress:   "fd00::2",
			want:          "fd00::2/128",
		},
		{
			name:          "IPv6 服务端与 peer 同族时套用其掩码",
			serverAddress: "fd00::1/64",
			peerAddress:   "fd00::2",
			want:          "fd00::/64",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configText := renderedConfig(t, tc.clientAllowedIPs, tc.serverAddress, tc.peerAddress)
			if got := allowedIPsLine(configText); got != tc.want {
				t.Fatalf("AllowedIPs = %q, want %q\n--- config ---\n%s", got, tc.want, configText)
			}
			if strings.Contains(configText, "AllowedIPs = 0.0.0.0/0") && tc.clientAllowedIPs == "" {
				t.Fatalf("默认配置不应下发全流量 AllowedIPs:\n%s", configText)
			}
		})
	}
}

func TestClientAllowedIPsDefaultsToSubnetNotFullTunnel(t *testing.T) {
	configText := renderedConfig(t, "", "10.100.0.1/24", "10.100.0.2")

	if !strings.Contains(configText, "AllowedIPs = 10.100.0.0/24") {
		t.Fatalf("期望下发 peer 所在网段，实际配置:\n%s", configText)
	}
	if strings.Contains(configText, "::/0") {
		t.Fatalf("非全局代理模式下不应出现 ::/0:\n%s", configText)
	}
	// 其余关键字段保持稳定
	for _, want := range []string{"[Interface]", "[Peer]", "PrivateKey = ", "Address = 10.100.0.2/32", "Endpoint = 203.0.113.10:51821", "PersistentKeepalive = 25"} {
		if !strings.Contains(configText, want) {
			t.Fatalf("配置缺少 %q:\n%s", want, configText)
		}
	}
}
