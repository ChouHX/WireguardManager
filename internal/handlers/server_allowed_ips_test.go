package handlers

import (
	"testing"

	"cloud-platform/internal/models"
)

// buildServerAllowedIPs 决定服务端把哪些目标网段路由给该设备。
// 遗漏网段会导致跨网段转发失败，误加 0.0.0.0/0 则会抢走全部流量。
func TestBuildServerAllowedIPs(t *testing.T) {
	cases := []struct {
		name        string
		peerAddress string
		allowedIPs  string
		want        string
	}{
		{
			name:        "仅自身地址",
			peerAddress: "10.100.0.2",
			allowedIPs:  "10.100.0.2/32",
			want:        "10.100.0.2/32",
		},
		{
			name:        "追加设备背后的局域网网段",
			peerAddress: "10.100.0.2",
			allowedIPs:  "10.100.0.2/32, 192.168.1.0/24",
			want:        "10.100.0.2/32,192.168.1.0/24",
		},
		{
			name:        "多个网段",
			peerAddress: "10.100.0.2",
			allowedIPs:  "192.168.1.0/24,10.20.30.0/24",
			want:        "10.100.0.2/32,192.168.1.0/24,10.20.30.0/24",
		},
		{
			name:        "排除全局代理，避免抢走所有流量",
			peerAddress: "10.100.0.2",
			allowedIPs:  "0.0.0.0/0, ::/0",
			want:        "10.100.0.2/32",
		},
		{
			name:        "全局代理与内网网段混排时只保留内网",
			peerAddress: "10.100.0.2",
			allowedIPs:  "0.0.0.0/0, 192.168.1.0/24",
			want:        "10.100.0.2/32,192.168.1.0/24",
		},
		{
			name:        "忽略重复的自身地址与空项",
			peerAddress: "10.100.0.2",
			allowedIPs:  "10.100.0.2/32,, 10.100.0.2 ,192.168.1.0/24",
			want:        "10.100.0.2/32,192.168.1.0/24",
		},
		{
			name:        "allowed_ips 为空时仅保留自身地址",
			peerAddress: "10.100.0.2",
			allowedIPs:  "",
			want:        "10.100.0.2/32",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			peer := &models.WireguardPeer{
				PeerAddress: tc.peerAddress,
				AllowedIPs:  tc.allowedIPs,
			}
			if got := buildServerAllowedIPs(peer); got != tc.want {
				t.Fatalf("buildServerAllowedIPs() = %q, want %q", got, tc.want)
			}
		})
	}
}
