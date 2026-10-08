package reconcile

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/services"
)

// Networks runs before HTTP handlers and probes. A partially configured tenant
// is kept down, and startup reports failure rather than serving stale metadata.
func Networks() error {
	var servers []models.WireguardServer
	if err := database.DB.Find(&servers).Error; err != nil {
		return err
	}
	networkService := services.NewUserNetworkServiceFromRuntime()
	interfaceService := services.NewInterfaceService()
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)
	var failures []error
	for _, server := range servers {
		var user models.User
		if err := database.DB.First(&user, server.UserID).Error; err != nil {
			failures = append(failures, err)
			continue
		}
		var peers []models.WireguardPeer
		if err := database.DB.Where("server_id = ?", server.ID).Find(&peers).Error; err != nil {
			failures = append(failures, err)
			continue
		}
		if err := networkService.EnsureUserNetwork(&server, user.UserUID); err != nil {
			failures = append(failures, fmt.Errorf("server %d: %w", server.ID, err))
			continue
		}
		err := restorePeers(interfaceService, wgService, &server, peers)
		if err == nil {
			err = interfaceService.ApplyRateLimit(server.WgInterface, server.DownloadRate, server.UploadRate)
		}
		if err == nil {
			err = database.DB.Model(&models.WireguardServer{}).Where("id = ?", server.ID).Updates(map[string]interface{}{
				"wg_interface": server.WgInterface, "network_mode": services.NetworkModeMultiInterface,
			}).Error
		}
		if err != nil {
			downErr := interfaceService.SetLinkState(server.WgInterface, false)
			failures = append(failures, fmt.Errorf("server %d restore: %w", server.ID, errors.Join(err, downErr)))
			continue
		}
		log.Printf("reconcile: server %d now uses %s / UDP %d, %d peers restored", server.ID, server.WgInterface, server.WgPort, len(peers))
	}
	return errors.Join(failures...)
}

// restorePeers 重新下发全部 peer。
// wg set 对已存在的 peer 是就地更新，因此该过程幂等，可安全重复执行。
//
// 单个 peer 失败不会中止其余 peer：这里的每个 peer 都对应一个真实设备，
// 若一处数据异常就整体退出，该账号剩下的设备会一起失去 allowed-ips，
// 表现为「只发不收」却没有任何明显线索。因此逐个尝试、最后汇总上报。
func restorePeers(interfaceService *services.InterfaceService, wgService *services.WireguardService, server *models.WireguardServer, peers []models.WireguardPeer) error {
	var failures []string

	for i := range peers {
		peer := peers[i]
		if err := services.ValidatePeerRoutes(&peer, peers); err != nil {
			failures = append(failures, err.Error())
			continue
		}

		if err := restorePeer(interfaceService, wgService, server, &peer); err != nil {
			failures = append(failures, fmt.Sprintf("peer %s (addr=%q): %v", shortKey(peer.PublicKey), peer.PeerAddress, err))
			continue
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d of %d peer(s) failed to restore: %s",
			len(failures), len(peers), strings.Join(failures, "; "))
	}
	return nil
}

// restorePeer 下发单个 peer 的 allowed-ips、PSK 与下挂网段路由。
func restorePeer(interfaceService *services.InterfaceService, wgService *services.WireguardService, server *models.WireguardServer, peer *models.WireguardPeer) error {
	// PeerAddress 是服务端侧 allowed-ips 的基石。缺失时会拼出非法的 "/32"，
	// 直接交给 wg set 只会得到一句难以定位的报错，这里提前给出明确原因。
	if strings.TrimSpace(peer.PeerAddress) == "" {
		return fmt.Errorf("peer has no tunnel address; its allowed-ips cannot be derived")
	}

	// allowed-ips 负责 cryptokey routing；内核路由需要单独下发
	if err := wgService.AddPeer(server.WgInterface, peer.PublicKey, services.ServerAllowedIPs(peer), ""); err != nil {
		return err
	}

	if peer.PresharedKey != "" {
		if err := wgService.SetPeerPresharedKey(server.WgInterface, peer.PublicKey, peer.PresharedKey); err != nil {
			return err
		}
	}

	// 显式下发下挂网段路由到租户专属路由表
	if peer.AllowedIPs != "" && peer.AllowedIPs != peer.PeerAddress+"/32" && peer.AllowedIPs != "0.0.0.0/0" {
		if err := interfaceService.AddRouteForPeer(server.WgInterface, peer.AllowedIPs); err != nil {
			return err
		}
	}

	return nil
}

// shortKey 截断公钥用于日志，保留可辨识的前缀。
func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12] + "..."
}
