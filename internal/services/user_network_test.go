package services

import (
	"testing"

	"cloud-platform/internal/models"
)

func TestSubnetIDFromAddress(t *testing.T) {
	cases := []struct {
		address string
		want    int
	}{
		{"10.100.156.1/24", 156},
		{"10.100.1.1/24", 1},
		{"10.100.254.1/24", 254},
		{"10.100.0.1/24", 0},   // 0 号网段不参与分配
		{"10.100.255.1/24", 0}, // 超出范围
		{"10.100.156.1", 156},  // 无掩码也能解析
		{"", 0},
		{"garbage", 0},
		{"10.100.abc.1/24", 0},
	}

	for _, c := range cases {
		if got := subnetIDFromAddress(c.address); got != c.want {
			t.Errorf("subnetIDFromAddress(%q) = %d, want %d", c.address, got, c.want)
		}
	}
}

func TestTunnelAddress(t *testing.T) {
	if got := tunnelAddress(156); got != "10.100.156.1/24" {
		t.Errorf("tunnelAddress(156) = %q, want %q", got, "10.100.156.1/24")
	}
}

func TestTempLinkNameLength(t *testing.T) {
	// Linux 接口名上限 15 字符（IFNAMSIZ 含结尾 NUL），超长会在 ip link add 时报错
	for _, uid := range []string{"admin001", "0123456789abcdef", "ab"} {
		name := tempLinkName(uid)
		if len(name) > 15 {
			t.Errorf("tempLinkName(%q) = %q (%d bytes), exceeds IFNAMSIZ", uid, name, len(name))
		}
	}

	if got := tempLinkName("0123456789abcdef"); got != "wgx-01234567" {
		t.Errorf("tempLinkName truncation failed: got %q", got)
	}
}

func TestInterfaceNamingIsLengthSafe(t *testing.T) {
	// 旧实现直接切片 UserUID[:6]，短 UID 会 panic；这组用例锁定修复后的行为
	uids := []string{"admin001", "0123456789abcdef", "ab", "a", ""}

	for _, uid := range uids {
		for _, name := range []string{tempLinkName(uid), legacyVethHostName(uid)} {
			if len(name) > 15 {
				t.Errorf("interface name %q exceeds IFNAMSIZ (15)", name)
			}
		}
	}

	if got := legacyVethHostName("admin001"); got != "veth-h-admin0" {
		t.Errorf("legacyVethHostName = %q, want %q", got, "veth-h-admin0")
	}
	if got := legacyVethHostName("ab"); got != "veth-h-ab" {
		t.Errorf("legacyVethHostName with a short uid = %q, want %q", got, "veth-h-ab")
	}
}

func TestAllocationsFromServers(t *testing.T) {
	servers := []models.WireguardServer{
		{WgPort: 51820, WgAddress: "10.100.1.1/24"},
		{WgPort: 51821, WgAddress: "10.100.2.1/24"},
	}

	got := AllocationsFromServers(servers)
	if len(got) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(got))
	}
	if got[0].WgPort != 51820 || got[0].SubnetID != 1 {
		t.Errorf("first allocation = %+v, want port 51820 subnet 1", got[0])
	}
	if got[1].WgPort != 51821 || got[1].SubnetID != 2 {
		t.Errorf("second allocation = %+v, want port 51821 subnet 2", got[1])
	}
}

func TestAllocateNetworkAvoidsConflicts(t *testing.T) {
	service := NewUserNetworkService("/tmp/wg-test", "10.200", 51820, "eth0")

	// 前三个端口与网段已被占用（宿主机端口占用无法在测试中伪造，这里只验证数据库侧避让）
	existing := []NetworkAllocation{
		{WgPort: 51820, SubnetID: 1},
		{WgPort: 51821, SubnetID: 2},
		{WgPort: 51822, SubnetID: 3},
	}

	port, subnetID, err := service.allocateNetwork(existing)
	if err != nil {
		t.Fatalf("allocateNetwork returned error: %v", err)
	}
	if port == 51820 || port == 51821 || port == 51822 {
		t.Errorf("allocated port %d collides with an existing allocation", port)
	}
	if subnetID <= 3 {
		t.Errorf("allocated subnet %d collides with an existing allocation", subnetID)
	}
}

func TestAllocateNetworkExhaustion(t *testing.T) {
	service := NewUserNetworkService("/tmp/wg-test", "10.200", 51820, "eth0")

	// 占满全部可用网段
	existing := make([]NetworkAllocation, 0, maxSubnetID)
	for id := 1; id <= maxSubnetID; id++ {
		existing = append(existing, NetworkAllocation{SubnetID: id})
	}

	if _, _, err := service.allocateNetwork(existing); err != ErrNoAvailableSubnet {
		t.Errorf("expected ErrNoAvailableSubnet when the address space is full, got %v", err)
	}
}

func TestServerAllowedIPs(t *testing.T) {
	cases := []struct {
		name      string
		peer      models.WireguardPeer
		wantExact string
	}{
		{
			name:      "仅自身地址",
			peer:      models.WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "10.100.1.2/32"},
			wantExact: "10.100.1.2/32",
		},
		{
			name:      "附带下挂网段",
			peer:      models.WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "10.100.1.2/32,192.168.10.0/24"},
			wantExact: "10.100.1.2/32,192.168.10.0/24",
		},
		{
			name:      "排除全局代理",
			peer:      models.WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "0.0.0.0/0"},
			wantExact: "10.100.1.2/32",
		},
		{
			name:      "排除全局代理但保留下挂网段",
			peer:      models.WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "0.0.0.0/0, 192.168.10.0/24"},
			wantExact: "10.100.1.2/32,192.168.10.0/24",
		},
		{
			name:      "排除 IPv6 全局代理",
			peer:      models.WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "::/0"},
			wantExact: "10.100.1.2/32",
		},
	}

	for _, c := range cases {
		if got := ServerAllowedIPs(&c.peer); got != c.wantExact {
			t.Errorf("%s: ServerAllowedIPs = %q, want %q", c.name, got, c.wantExact)
		}
	}
}

func TestIsDefaultRoute(t *testing.T) {
	for _, cidr := range []string{"0.0.0.0/0", "::/0"} {
		if !isDefaultRoute(cidr) {
			t.Errorf("isDefaultRoute(%q) = false, want true", cidr)
		}
	}
	for _, cidr := range []string{"192.168.10.0/24", "10.100.1.2/32", ""} {
		if isDefaultRoute(cidr) {
			t.Errorf("isDefaultRoute(%q) = true, want false", cidr)
		}
	}
}
