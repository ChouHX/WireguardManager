package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/response"
	"cloud-platform/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Windows registers its own public key. It never adopts a remote gateway's key.
// Retrying a lost response finds the same access peer; an account cannot claim
// another account's key or turn an existing gateway into a desktop connection.
func accessPeer(server *models.WireguardServer, publicKey, name string) (models.WireguardPeer, bool, error) {
	var peer models.WireguardPeer
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != publicKey || publicKey == base64.StdEncoding.EncodeToString(make([]byte, 32)) {
		return peer, false, errors.New("invalid desktop public key")
	}
	err = database.DB.Where("public_key = ?", publicKey).First(&peer).Error
	if err == nil {
		if peer.ServerID != server.ID || peer.DeviceRole != "access" {
			return peer, false, errors.New("public key is already assigned")
		}
		return peer, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return peer, false, err
	}
	addr, err := allocatePeerIP(server.ID, server.WgAddress)
	if err != nil {
		return peer, false, err
	}
	var psk [32]byte
	if _, err = rand.Read(psk[:]); err != nil {
		return peer, false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Windows"
	}
	if len(name) > 128 {
		name = name[:128]
	}
	peer = models.WireguardPeer{ServerID: server.ID, DeviceRole: "access", PublicKey: publicKey, PeerAddress: addr, AllowedIPs: addr + "/32", PresharedKey: base64.StdEncoding.EncodeToString(psk[:]), PersistentKeepalive: 25, Comment: "访问终端 · " + name}
	if err = database.DB.Create(&peer).Error; err != nil {
		return peer, false, err
	}
	return peer, true, nil
}

func accessConfig(server models.WireguardServer, peer models.WireguardPeer, endpoint string) string {
	// No private key: only the registering computer holds it in its encrypted store.
	return fmt.Sprintf("[Interface]\nAddress = %s/32\n\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nEndpoint = %s\nAllowedIPs = %s\nPersistentKeepalive = %d\n", peer.PeerAddress, server.WgPublicKey, peer.PresharedKey, endpoint, clientAllowedIPs(server.WgAddress, peer.PeerAddress), peer.PersistentKeepalive)
}

func EnsureDesktopAccess(c *gin.Context) {
	peerMutationMu.Lock()
	defer peerMutationMu.Unlock()
	u, ok := currentUser(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	var req struct {
		PublicKey string `json:"public_key" binding:"required"`
		Name      string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid desktop registration")
		return
	}
	var server models.WireguardServer
	if err := database.DB.Where("user_id = ?", u.ID).First(&server).Error; err != nil {
		response.BadRequest(c, "User has no WireGuard server configured", nil)
		return
	}
	if !server.Enabled {
		response.Forbidden(c, "This account's WireGuard server has been disabled")
		return
	}
	endpoint, err := services.ClientEndpoint(server.ServerEndpoint, services.GetSettings().String(services.SettingNetworkServerIP, config.AppConfig.Network.ServerIP), server.WgPort)
	if err != nil {
		response.ServiceUnavailable(c, err.Error())
		return
	}
	peer, created, err := accessPeer(&server, req.PublicKey, req.Name)
	if err != nil {
		response.BadRequest(c, "Unable to register desktop public key", nil)
		return
	}
	if created {
		wg := services.NewWireguardService(config.AppConfig.Network.ConfigDir)
		if err := provisionPeerResources(wg, services.NewInterfaceService(), server.WgInterface, &peer); err != nil {
			database.DB.Delete(&peer)
			response.InternalError(c, "Failed to provision desktop access")
			return
		}
	}
	response.Success(c, "Desktop access ready", gin.H{"peer": peer.ToResponse(), "config": accessConfig(server, peer, endpoint)})
}
