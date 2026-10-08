package models

import (
	"net/netip"
	"strings"
	"time"

	"gorm.io/gorm"
)

// WireguardServer WireGuard服务器配置
type WireguardServer struct {
	NetworkMode    string    `json:"network_mode"`
	ID             uint      `json:"id" gorm:"primaryKey"`
	UserID         uint      `json:"user_id" gorm:"uniqueIndex;not null"`
	User           User      `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Namespace      string    `json:"namespace" gorm:"uniqueIndex;not null"` // 旧版迁移标识；不再创建命名空间
	WgInterface    string    `json:"wg_interface" gorm:"not null"`          // WireGuard接口名称（如wgm1）
	WgPort         int       `json:"wg_port" gorm:"not null"`               // WireGuard监听端口
	WgPublicKey    string    `json:"wg_public_key" gorm:"not null"`         // WireGuard服务器公钥
	WgPrivateKey   string    `json:"-" gorm:"not null"`                     // WireGuard服务器私钥（不返回）
	WgAddress      string    `json:"wg_address" gorm:"not null"`            // WireGuard接口IP地址
	ServerEndpoint string    `json:"server_endpoint" gorm:""`               // 服务器外部访问地址（IP:Port）
	Enabled        bool      `json:"enabled" gorm:"default:true"`           // 是否启用
	DownloadRate   int       `json:"download_rate" gorm:"default:0"`        // 下载速率限制（Mbps，0表示不限速）
	UploadRate     int       `json:"upload_rate" gorm:"default:0"`          // 上传速率限制（Mbps，0表示不限速）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// WireguardServerResponse 服务器响应结构
type WireguardServerResponse struct {
	NetworkMode    string    `json:"network_mode"`
	ID             uint      `json:"id"`
	UserID         uint      `json:"user_id"`
	Namespace      string    `json:"namespace"`
	WgInterface    string    `json:"wg_interface"`
	WgPort         int       `json:"wg_port"`
	WgPublicKey    string    `json:"wg_public_key"`
	WgAddress      string    `json:"wg_address"`
	ServerEndpoint string    `json:"server_endpoint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ToResponse 转换为响应格式
func (s *WireguardServer) ToResponse() WireguardServerResponse {
	return WireguardServerResponse{
		NetworkMode:    s.NetworkMode,
		ID:             s.ID,
		UserID:         s.UserID,
		Namespace:      s.Namespace,
		WgInterface:    s.WgInterface,
		WgPort:         s.WgPort,
		WgPublicKey:    s.WgPublicKey,
		WgAddress:      s.WgAddress,
		ServerEndpoint: s.ServerEndpoint,
		CreatedAt:      s.CreatedAt,
	}
}

// WireguardPeer WireGuard peer信息
type WireguardPeer struct {
	ClientAllowedIPs    string          `json:"-" gorm:"default:''"`
	ID                  uint            `json:"id" gorm:"primaryKey"`
	ServerID            uint            `json:"server_id" gorm:"index;not null"`
	Server              WireguardServer `json:"server,omitempty" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
	PublicKey           string          `json:"public_key" gorm:"uniqueIndex;not null"`
	PrivateKey          string          `json:"-" gorm:"not null"`            // peer私钥，不返回给客户端
	PresharedKey        string          `json:"-" gorm:""`                    // 不返回给客户端
	PeerAddress         string          `json:"peer_address" gorm:"not null"` // peer在WireGuard网段中的IP地址
	AllowedIPs          string          `json:"allowed_ips" gorm:"not null"`  // 设备自身 /32 与设备局域网
	Endpoint            string          `json:"endpoint" gorm:""`
	PersistentKeepalive int             `json:"persistent_keepalive" gorm:"default:0"`
	Comment             string          `json:"comment" gorm:""` // 备注，如设备名称
	// Legacy columns retained for database compatibility; all devices forward locally.
	EnableForwarding bool      `json:"-" gorm:"default:true"`
	ForwardInterface string    `json:"-" gorm:""`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WireguardPeerResponse Peer响应结构
type WireguardPeerResponse struct {
	ID                  uint   `json:"id"`
	PublicKey           string `json:"public_key"`
	PrivateKey          string `json:"private_key"`  // 返回私钥供客户端配置使用
	PeerAddress         string `json:"peer_address"` // peer的WireGuard IP地址
	AllowedIPs          string `json:"allowed_ips"`  // 完整服务端 cryptokey routes，始终包含 peer 自身 /32
	DeviceLAN           string `json:"device_lan"`   // 单独用于局域网展示与编辑
	Endpoint            string `json:"endpoint,omitempty"`
	PersistentKeepalive int    `json:"persistent_keepalive"`
	Comment             string `json:"comment,omitempty"`
	// UsePresharedKey 仅表示是否启用，密钥本身不下发到管理端
	UsePresharedKey bool      `json:"use_preshared_key"`
	CreatedAt       time.Time `json:"created_at"`
}

// WireguardPeerStats Peer实时统计信息
type WireguardPeerStats struct {
	DeviceLAN           string    `json:"device_lan"`
	PublicKey           string    `json:"public_key"`
	Endpoint            string    `json:"endpoint,omitempty"`
	AllowedIPs          string    `json:"allowed_ips"`
	LatestHandshake     time.Time `json:"latest_handshake,omitempty"`
	TransferRx          int64     `json:"transfer_rx"` // 接收字节数
	TransferTx          int64     `json:"transfer_tx"` // 发送字节数
	PersistentKeepalive int       `json:"persistent_keepalive"`
	Comment             string    `json:"comment,omitempty"`
}

// WireguardServerStats 服务器级别统计
type WireguardServerStats struct {
	Interface  string               `json:"interface"`
	PublicKey  string               `json:"public_key"`
	ListenPort int                  `json:"listen_port"`
	PeerCount  int                  `json:"peer_count"`
	TotalRx    int64                `json:"total_rx"`
	TotalTx    int64                `json:"total_tx"`
	Peers      []WireguardPeerStats `json:"peers"`
}

// UserTrafficStats 用户流量统计
type UserTrafficStats struct {
	UserID      uint                     `json:"user_id"`
	UserUID     string                   `json:"user_uid"`
	Email       string                   `json:"email"`
	ServerInfo  *WireguardServerResponse `json:"server_info"`
	ServerStats *WireguardServerStats    `json:"server_stats"`
}

// UserTrafficSummary 用户流量摘要（用于轮询）
type UserTrafficSummary struct {
	PeerCount int                  `json:"peer_count"`
	TotalRx   int64                `json:"total_rx"`
	TotalTx   int64                `json:"total_tx"`
	Peers     []PeerTrafficSummary `json:"peers"`
}

// PeerTrafficSummary Peer流量摘要
type PeerTrafficSummary struct {
	PublicKey       string    `json:"public_key"`
	LatestHandshake time.Time `json:"latest_handshake,omitempty"`
	TransferRx      int64     `json:"transfer_rx"`
	TransferTx      int64     `json:"transfer_tx"`
	Comment         string    `json:"comment,omitempty"`
}

// AdminUserTraffic 管理员查看的用户流量信息
type AdminUserTraffic struct {
	WgInterface  string `json:"wg_interface"`
	NetworkMode  string `json:"network_mode"`
	ServerID     uint   `json:"server_id"`
	UserID       uint   `json:"user_id"`
	UserUID      string `json:"user_uid"`
	Email        string `json:"email"`
	PeerCount    int    `json:"peer_count"`
	TotalRx      int64  `json:"total_rx"`
	TotalTx      int64  `json:"total_tx"`
	WgPort       int    `json:"wg_port"`
	WgAddress    string `json:"wg_address"`
	Namespace    string `json:"namespace"`
	Enabled      bool   `json:"enabled"`
	DownloadRate int    `json:"download_rate"` // Mbps
	UploadRate   int    `json:"upload_rate"`   // Mbps
}

// ToResponse 转换为响应格式
func (p *WireguardPeer) ToResponse() WireguardPeerResponse {
	return WireguardPeerResponse{
		ID:                  p.ID,
		PublicKey:           p.PublicKey,
		PrivateKey:          p.PrivateKey,
		PeerAddress:         p.PeerAddress,
		AllowedIPs:          p.ServerAllowedIPs(),
		DeviceLAN:           p.DeviceLANs(),
		Endpoint:            p.Endpoint,
		PersistentKeepalive: p.PersistentKeepalive,
		Comment:             p.Comment,
		UsePresharedKey:     p.PresharedKey != "",
		CreatedAt:           p.CreatedAt,
	}
}

// BeforeCreate Hook
func (p *WireguardPeer) BeforeCreate(tx *gorm.DB) error {
	if p.PeerAddress != "" {
		p.AllowedIPs = p.ServerAllowedIPs()
	}
	p.EnableForwarding = true
	p.ClientAllowedIPs = ""
	p.ForwardInterface = ""
	if p.PersistentKeepalive == 0 {
		p.PersistentKeepalive = 25 // 默认25秒
	}
	return nil
}

// DeviceLANs omits WireGuard addresses from the editable LAN declaration.
// ServerAllowedIPs still adds the device's tunnel /32 when configuring WireGuard.
func (p *WireguardPeer) DeviceLANs() string {
	var result []string
	seen := map[string]bool{}
	peerIP, _ := netip.ParseAddr(p.PeerAddress)
	reserved := netip.MustParsePrefix("10.100.0.0/16")
	for _, raw := range strings.Split(p.AllowedIPs, ",") {
		raw = strings.TrimSpace(raw)
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		prefix = prefix.Masked()
		if prefix.Contains(peerIP) || prefix.Overlaps(reserved) {
			continue
		}
		if !seen[prefix.String()] {
			result = append(result, prefix.String())
			seen[prefix.String()] = true
		}
	}
	return strings.Join(result, ", ")
}

// ServerAllowedIPs preserves the immutable tunnel address alongside LAN routes.
// Both API responses and the WireGuard backend use this same representation.
func (p *WireguardPeer) ServerAllowedIPs() string {
	self := p.PeerAddress + "/32"
	parts := []string{self}

	for _, cidr := range strings.Split(p.AllowedIPs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" || cidr == self || cidr == p.PeerAddress {
			continue
		}
		if cidr == "0.0.0.0/0" || cidr == "::/0" {
			continue
		}
		parts = append(parts, cidr)
	}

	return strings.Join(parts, ",")
}
