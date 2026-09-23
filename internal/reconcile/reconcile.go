// Package reconcile 在启动时把已登记的账号网络收敛到目标形态。
//
// 需要收敛的情况有两类：
//   - 旧版方案（veth + DNAT）编排的账号：当时的加密 socket 建在账号命名空间内，
//     回程依赖 conntrack 反向改写，端口漂移与握手抖动即源于此；
//   - 命名空间或隧道接口因外部原因缺失的账号。
//
// 判定依据是宿主命名空间里 <listen-port> 是否已有 UDP socket —— 新方案下加密
// socket 恒定落在宿主命名空间，旧方案下则落在账号命名空间内。
//
// 收敛过程沿用数据库中已记录的端口与隧道地址，因此下发给客户端的配置不变，
// 设备侧无需重新导入。
package reconcile

import (
	"log"

	"cloud-platform/internal/config"
	"cloud-platform/internal/database"
	"cloud-platform/internal/models"
	"cloud-platform/internal/services"
)

// Networks 收敛全部账号网络。
// 单个账号失败只记日志，不阻断其余账号，也不影响服务启动。
func Networks() {
	var servers []models.WireguardServer
	if err := database.DB.Find(&servers).Error; err != nil {
		log.Printf("reconcile: failed to list wireguard servers: %v", err)
		return
	}
	if len(servers) == 0 {
		return
	}

	networkService := services.NewUserNetworkServiceFromRuntime()
	netnsService := services.NewNetnsService()
	wgService := services.NewWireguardService(config.AppConfig.Network.ConfigDir)

	rebuilt := 0

	for i := range servers {
		server := servers[i]

		var user models.User
		if err := database.DB.First(&user, server.UserID).Error; err != nil {
			log.Printf("reconcile: server %d has no loadable user (%d), skipping: %v", server.ID, server.UserID, err)
			continue
		}

		var peers []models.WireguardPeer
		if err := database.DB.Where("server_id = ?", server.ID).Find(&peers).Error; err != nil {
			log.Printf("reconcile: failed to load peers of server %d: %v", server.ID, err)
			continue
		}

		if networkReady(netnsService, &server) {
			continue
		}

		if err := networkService.EnsureUserNetwork(&server, user.UserUID); err != nil {
			log.Printf("reconcile: failed to rebuild network for server %d (%s): %v", server.ID, server.Namespace, err)
			continue
		}

		if err := restorePeers(netnsService, wgService, &server, peers); err != nil {
			log.Printf("reconcile: network of server %d was rebuilt, but restoring its peers failed: %v", server.ID, err)
			continue
		}

		// 限速规则挂在 netdev 上，命名空间重建后 netdev 是新对象、规则已随旧接口消失，
		// 必须按数据库里的取值重新下发，否则重启一次限速就悄悄失效了。
		if err := netnsService.ApplyRateLimit(server.Namespace, server.WgInterface,
			server.DownloadRate, server.UploadRate); err != nil {
			log.Printf("reconcile: peers of server %d were restored, but applying its rate limit failed: %v",
				server.ID, err)
			continue
		}

		rebuilt++
		log.Printf("reconcile: server %d (%s) now runs on the native cross-namespace layout, %d peer(s) restored",
			server.ID, server.Namespace, len(peers))
	}

	if rebuilt > 0 {
		log.Printf("reconcile: %d of %d wireguard server(s) were rebuilt", rebuilt, len(servers))
	}
}

// networkReady 判断账号网络是否已处于目标形态。
func networkReady(netnsService *services.NetnsService, server *models.WireguardServer) bool {
	if server.Namespace == "" || server.WgInterface == "" || server.WgPort <= 0 {
		return false
	}

	exists, err := netnsService.NamespaceExists(server.Namespace)
	if err != nil || !exists {
		return false
	}

	if !netnsService.LinkExistsInNamespace(server.Namespace, server.WgInterface) {
		return false
	}

	// 被禁用的账号：接口按设计保持 down，内核不会为它建立 socket，
	// 「socket 落在宿主命名空间」这条判据对它不适用。此时只要确认接口确实是
	// down 的就视为已就绪；若它反被拉起（人为误操作），下面的重建会纠正回来。
	if !server.Enabled {
		return !netnsService.LinkIsUpInNamespace(server.Namespace, server.WgInterface)
	}

	// 新形态下加密 socket 一定落在宿主命名空间；缺失说明仍是旧形态或接口未拉起
	return services.HostUDPPortInUse(server.WgPort)
}

// restorePeers 重新下发全部 peer。
// wg set 对已存在的 peer 是就地更新，因此该过程幂等，可安全重复执行。
func restorePeers(netnsService *services.NetnsService, wgService *services.WireguardService, server *models.WireguardServer, peers []models.WireguardPeer) error {
	for i := range peers {
		peer := peers[i]

		// allowed-ips 同时承担 cryptokey routing 与内核自动路由，必须与服务端一致
		if err := wgService.AddPeer(server.Namespace, server.WgInterface, peer.PublicKey, services.ServerAllowedIPs(&peer), ""); err != nil {
			return err
		}

		if peer.PresharedKey != "" {
			if err := wgService.SetPeerPresharedKey(server.Namespace, server.WgInterface, peer.PublicKey, peer.PresharedKey); err != nil {
				return err
			}
		}

		// 声明了背后网段的设备补一条兜底路由（内核通常已自动生成）
		if peer.AllowedIPs != "" && peer.AllowedIPs != peer.PeerAddress+"/32" && peer.AllowedIPs != "0.0.0.0/0" {
			if err := netnsService.AddRouteForPeer(server.Namespace, server.WgInterface, peer.AllowedIPs); err != nil {
				return err
			}
		}
	}

	return nil
}
