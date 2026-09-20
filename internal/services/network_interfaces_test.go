package services

import (
	"strings"
	"testing"
)

func TestIsVirtualInterface(t *testing.T) {
	virtual := []string{"lo", "wg0", "veth1234", "docker0", "br-abcdef", "tun0", "kube-ipvs0", "tailscale0", "ztabcdef", "nebula1"}
	physical := []string{"eth0", "ens33", "enp4s0", "wlan0", "eno1", "bond0"}

	for _, name := range virtual {
		if !isVirtualInterface(name) {
			t.Errorf("isVirtualInterface(%q) = false, want true", name)
		}
	}
	for _, name := range physical {
		if isVirtualInterface(name) {
			t.Errorf("isVirtualInterface(%q) = true, want false", name)
		}
	}
}

func TestDetectNetworkInterfaces(t *testing.T) {
	interfaces, detected := DetectNetworkInterfaces()

	if len(interfaces) == 0 {
		t.Fatal("期望至少探测到一个网络接口")
	}

	for _, item := range interfaces {
		if strings.TrimSpace(item.Name) == "" {
			t.Errorf("接口名不应为空: %+v", item)
		}
	}

	// 探测到默认出口时，它必须出现在列表里并被标记
	if detected != "" {
		var found bool
		for _, item := range interfaces {
			if item.Name == detected {
				found = true
				if !item.IsDefault {
					t.Errorf("默认出口 %q 未被标记 IsDefault", detected)
				}
			}
		}
		if !found {
			t.Errorf("默认出口 %q 未出现在接口列表中", detected)
		}

		if interfaces[0].Name != detected {
			t.Errorf("默认出口应排在首位，实际首位为 %q", interfaces[0].Name)
		}
	}

	// 排序约束：虚拟接口不应排在物理接口之前
	firstVirtualIdx := len(interfaces)
	for i, item := range interfaces {
		if item.IsVirtual && !item.IsLoopback {
			firstVirtualIdx = i
			break
		}
	}
	for i := 0; i < firstVirtualIdx; i++ {
		if interfaces[i].IsVirtual && !interfaces[i].IsDefault && !interfaces[i].IsLoopback {
			t.Errorf("物理接口 %q 排在了虚拟接口之后", interfaces[i].Name)
		}
	}
}
