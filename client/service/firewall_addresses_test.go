package service

import "testing"

func TestFirewallRemoteAddresses(t *testing.T) {
	actual, err := firewallRemoteAddresses("192.168.1.9/24, 192.168.2.0/24\n192.168.3.100; 192.168.2.0/24")
	if err != nil || actual != "192.168.1.0/24,192.168.2.0/24,192.168.3.100/32" {
		t.Fatalf("lost multi-network scope: %q, %v", actual, err)
	}
	for _, raw := range []string{"", "*", "0.0.0.0/0", "192.168.1.0/24,invalid"} {
		if _, err := firewallRemoteAddresses(raw); err == nil {
			t.Fatalf("unsafe firewall scope accepted: %q", raw)
		}
	}
}
