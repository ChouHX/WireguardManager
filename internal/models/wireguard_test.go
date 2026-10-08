package models

import "testing"

func TestDeviceLANsHideTunnelAndRetainStoredRoutes(t *testing.T) {
	peer := WireguardPeer{PeerAddress: "10.100.1.2", AllowedIPs: "10.100.1.2/32, 10.100.1.0/24, 192.168.0.9/24, 192.168.0.0/24"}
	before := peer.AllowedIPs
	if got := peer.ToResponse().AllowedIPs; got != "192.168.0.0/24" {
		t.Fatal(got)
	}
	if peer.AllowedIPs != before {
		t.Fatal("rendering changed cryptokey routes")
	}
}
func TestDeviceDefaultForwarding(t *testing.T) {
	peer := WireguardPeer{EnableForwarding: false, ForwardInterface: "eth0", ClientAllowedIPs: "0.0.0.0/0"}
	if err := peer.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if !peer.EnableForwarding || peer.ForwardInterface != "" || peer.ClientAllowedIPs != "" {
		t.Fatal("obsolete preferences remain")
	}
}
