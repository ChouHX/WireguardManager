package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPerInterfaceDumpStatistics(t *testing.T) {
	// `wg show wgm1 dump` omits the interface-name column used by `wg show all dump`.
	raw := "private-server-key\tpublic-server-key\t51820\toff\n" +
		"public-peer-key\tprivate-psk\t198.51.100.2:54330\t10.100.1.2/32\t0\t444\t276\t10\n"
	stats, err := NewWireguardService("").parseWireguardDump(raw, "wgm1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.PublicKey != "public-server-key" || stats.ListenPort != 51820 {
		t.Fatal("interface dump columns were shifted")
	}
	if len(stats.Peers) != 1 || !stats.Peers[0].LatestHandshake.IsZero() || stats.TotalRx != 444 || stats.TotalTx != 276 {
		t.Fatal("traffic counters must not imply a completed handshake")
	}
	encoded, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-server-key", "private-psk"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("statistics leaked secret material")
		}
	}
}
