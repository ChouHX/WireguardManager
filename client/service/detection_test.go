package service

import (
	"net/netip"
	"testing"
)

func lanAdapter(id, address string, metric uint32) AdapterInfo {
	return AdapterInfo{ID: id, Name: id, Addresses: []string{address}, Metric: metric, AutoEligible: true}
}
func TestAutomaticLANSelection(t *testing.T) {
	adapters := []AdapterInfo{lanAdapter("broad", "192.168.0.4/16", 1), lanAdapter("ethernet", "192.168.1.10/24", 10), lanAdapter("wifi", "192.168.1.20/24", 20), lanAdapter("plant2", "172.16.1.10/24", 10)}
	lans, _ := ParsePrefixes("192.168.1.100,172.16.1.0/24")
	selected, err := DetectLANAdapters(lans, adapters)
	if err != nil || len(selected) != 2 || selected[0].ID != "ethernet" || selected[1].ID != "plant2" {
		t.Fatal(selected, err)
	}
	adapters[2].Metric = 10
	if _, err = DetectLANAdapters(lans, adapters); err == nil {
		t.Fatal("ambiguous adapters accepted")
	}
	if _, err = DetectLANAdapters([]netip.Prefix{netip.MustParsePrefix("10.23.0.0/24")}, adapters); err == nil {
		t.Fatal("selected a default route instead of connected LAN")
	}
}
func TestSuggestionsExcludeVirtualAndVPNNetworks(t *testing.T) {
	adapters := []AdapterInfo{lanAdapter("ethernet", "192.168.1.10/24", 10), lanAdapter("reserved", "10.100.2.10/24", 10), lanAdapter("tunnel", "10.10.0.2/24", 10), lanAdapter("docker", "172.17.0.1/16", 1)}
	adapters[3].AutoEligible = false
	if got := SuggestedLANs(adapters, []netip.Prefix{netip.MustParsePrefix("10.10.0.0/24")}); got != "192.168.1.0/24" {
		t.Fatal(got)
	}
	if _, err := DetectLANAdapters([]netip.Prefix{netip.MustParsePrefix("172.17.0.4/32")}, adapters); err == nil {
		t.Fatal("selected virtual adapter")
	}
}
func TestRemoteHostCanOverlapLocalSubnetButNotLocalIP(t *testing.T) {
	p := profileForTest(t, "A")
	adapters := []AdapterInfo{lanAdapter("ethernet", "192.168.0.10/24", 10)}
	for _, targets := range []string{"192.168.0.0/24", "192.168.0.10"} {
		p.Targets = targets
		if err := ValidateLocalRouteTargets(p, adapters); err == nil {
			t.Fatal("local IP route accepted")
		}
	}
	p.Targets = "192.168.0.100"
	if err := ValidateLocalRouteTargets(p, adapters); err != nil {
		t.Fatal(err)
	}
}
