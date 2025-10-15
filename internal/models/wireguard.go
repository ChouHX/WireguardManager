package models

import (
	"time"

	"gorm.io/gorm"
)

// WireguardServer WireGuard服务器配置
type WireguardServer struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	UserID          uint      `json:"user_id" gorm:"uniqueIndex;not null"`
	User            User      `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Namespace       string    `json:"namespace" gorm:"uniqueIndex;not null"` // 网络命名空间名称
	WgInterface     string    `json:"wg_interface" gorm:"not null"`          // WireGuard接口名称（如wg0）
	WgPort          int       `json:"wg_port" gorm:"not null"`               // WireGuard监听端口
	WgPublicKey     string    `json:"wg_public_key" gorm:"not null"`         // WireGuard服务器公钥
	WgPrivateKey    string    `json:"-" gorm:"not null"`                     // WireGuard服务器私钥（不返回）
	WgAddress       string    `json:"wg_address" gorm:"not null"`            // WireGuard接口IP地址
	ServerEndpoint  string    `json:"server_endpoint" gorm:""`               // 服务器外部访问地址（IP:Port）
	Enabled         bool      `json:"enabled" gorm:"default:true"`           // 是否启用
	DownloadRate    int       `json:"download_rate" gorm:"default:0"`        // 下载速率限制（Mbps，0表示不限速）
	UploadRate      int       `json:"upload_rate" gorm:"default:0"`          // 上传速率限制（Mbps，0表示不限速）
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// WireguardServerResponse 服务器响应结构
type WireguardServerResponse struct {
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
	ID                  uint      `json:"id" gorm:"primaryKey"`
	ServerID            uint      `json:"server_id" gorm:"index;not null"`
	Server              WireguardServer `json:"server,omitempty" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
	PublicKey           string    `json:"public_key" gorm:"uniqueIndex;not null"`
	PrivateKey          string    `json:"-" gorm:"not null"` // peer私钥，不返回给客户端
	PresharedKey        string    `json:"-" gorm:""` // 不返回给客户端
	PeerAddress         string    `json:"peer_address" gorm:"not null"` // peer在WireGuard网段中的IP地址
	AllowedIPs          string    `json:"allowed_ips" gorm:"not null"` // peer可以访问的IP地址或网段
	Endpoint            string    `json:"endpoint" gorm:""`
	PersistentKeepalive int       `json:"persistent_keepalive" gorm:"default:0"`
	Comment             string    `json:"comment" gorm:""` // 备注，如设备名称
	EnableForwarding    bool      `json:"enable_forwarding" gorm:"default:false"` // 是否启用转发（作为网关）
	ForwardInterface    string    `json:"forward_interface" gorm:""` // 转发接口名称（如 eth0）
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// WireguardPeerResponse Peer响应结构
type WireguardPeerResponse struct {
	ID                  uint      `json:"id"`
	PublicKey           string    `json:"public_key"`
	PrivateKey          string    `json:"private_key"` // 返回私钥供客户端配置使用
	PeerAddress         string    `json:"peer_address"` // peer的WireGuard IP地址
	AllowedIPs          string    `json:"allowed_ips"`
	Endpoint            string    `json:"endpoint,omitempty"`
	PersistentKeepalive int       `json:"persistent_keepalive"`
	Comment             string    `json:"comment,omitempty"`
	EnableForwarding    bool      `json:"enable_forwarding"`
	ForwardInterface    string    `json:"forward_interface,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// WireguardPeerStats Peer实时统计信息
type WireguardPeerStats struct {
	PublicKey         string    `json:"public_key"`
	Endpoint          string    `json:"endpoint,omitempty"`
	AllowedIPs        string    `json:"allowed_ips"`
	LatestHandshake   time.Time `json:"latest_handshake,omitempty"`
	TransferRx        int64     `json:"transfer_rx"` // 接收字节数
	TransferTx        int64     `json:"transfer_tx"` // 发送字节数
	PersistentKeepalive int     `json:"persistent_keepalive"`
	Comment           string    `json:"comment,omitempty"`
}

// WireguardServerStats 服务器级别统计
type WireguardServerStats struct {
	Interface    string               `json:"interface"`
	PublicKey    string               `json:"public_key"`
	ListenPort   int                  `json:"listen_port"`
	PeerCount    int                  `json:"peer_count"`
	TotalRx      int64                `json:"total_rx"`
	TotalTx      int64                `json:"total_tx"`
	Peers        []WireguardPeerStats `json:"peers"`
}

// UserTrafficStats 用户流量统计
type UserTrafficStats struct {
	UserID       uint                     `json:"user_id"`
	UserUID      string                   `json:"user_uid"`
	Email        string                   `json:"email"`
	ServerInfo   *WireguardServerResponse `json:"server_info"`
	ServerStats  *WireguardServerStats    `json:"server_stats"`
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
		AllowedIPs:          p.AllowedIPs,
		Endpoint:            p.Endpoint,
		PersistentKeepalive: p.PersistentKeepalive,
		Comment:             p.Comment,
		EnableForwarding:    p.EnableForwarding,
		ForwardInterface:    p.ForwardInterface,
		CreatedAt:           p.CreatedAt,
	}
}

// BeforeCreate Hook
func (p *WireguardPeer) BeforeCreate(tx *gorm.DB) error {
	if p.PersistentKeepalive == 0 {
		p.PersistentKeepalive = 25 // 默认25秒
	}
	return nil
}
