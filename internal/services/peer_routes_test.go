package services

import (
	"errors"
	"strings"
	"testing"

	"cloud-platform/internal/models"
)

func TestTenantRouteOwnership(t *testing.T) {
	peer := models.WireguardPeer{ID: 1, ServerID: 1, PeerAddress: "10.100.1.2", AllowedIPs: "192.168.0.0/24"}
	other := models.WireguardPeer{ID: 2, ServerID: 2, PeerAddress: "10.100.2.2", AllowedIPs: "192.168.0.0/24"}
	if err := ValidatePeerRoutes(&peer, []models.WireguardPeer{other}); err != nil {
		t.Fatalf("cross-tenant overlap must work: %v", err)
	}
	other.ServerID = 1
	if err := ValidatePeerRoutes(&peer, []models.WireguardPeer{other}); err == nil {
		t.Fatal("same-tenant duplicate must fail")
	}
	other.AllowedIPs = "192.168.0.100/32"
	if err := ValidatePeerRoutes(&peer, []models.WireguardPeer{other}); err == nil {
		t.Fatal("same-tenant contained route must fail")
	}
	for _, route := range []string{"10.100.2.2/32", "10.0.0.0/8", "10.100.1.0/24", "2001:db8::/64", "invalid"} {
		peer.AllowedIPs = route
		if err := ValidatePeerRoutes(&peer, nil); err == nil {
			t.Fatalf("unsafe route accepted: %s", route)
		}
	}
}

func TestClientRoutesDoNotBecomeGatewayRoutes(t *testing.T) {
	routes, err := NormalizeClientRoutes("192.168.0.100, 192.168.0.100/32,192.168.2.99/24")
	if err != nil || routes != "192.168.0.100/32, 192.168.2.0/24" {
		t.Fatalf("normalization: %q %v", routes, err)
	}
	peer := models.WireguardPeer{PeerAddress: "10.100.1.3", ClientAllowedIPs: routes}
	if got := ServerAllowedIPs(&peer); got != "10.100.1.3/32" {
		t.Fatalf("client destinations hijacked server routing: %s", got)
	}
	for _, value := range []string{"0.0.0.0/0", "::/0", "2001:db8::1/128", "bad"} {
		if _, err := NormalizeClientRoutes(value); err == nil {
			t.Fatalf("accepted unsupported client target %s", value)
		}
	}
}

func TestRouteFailureRollsBackOnlyNewRoutes(t *testing.T) {
	for _, alreadyExists := range []bool{false, true} {
		var commands []string
		s := &InterfaceService{run: func(name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			if strings.Contains(command, "link show") {
				return []byte("1: wgm1: <POINTOPOINT,UP>"), nil
			}
			if strings.Contains(command, "route add 192.168.2.0/24") {
				return []byte("injected failure"), errors.New("route add")
			}
			if alreadyExists && strings.Contains(command, "route add 192.168.1.0/24") {
				return []byte("RTNETLINK answers: File exists"), errors.New("exists")
			}
			return nil, nil
		}}
		if err := s.AddRouteForPeer("wgm1", "192.168.1.0/24,192.168.2.0/24"); err == nil {
			t.Fatal("expected route failure")
		}
		deleted := strings.Contains(strings.Join(commands, "\n"), "route del 192.168.1.0/24")
		if deleted == alreadyExists {
			t.Fatalf("wrong rollback (existing=%v): %v", alreadyExists, commands)
		}
		for _, command := range commands {
			if strings.Contains(command, "route ") && !strings.Contains(command, "table 20001") {
				t.Fatalf("route escaped tenant table: %s", command)
			}
		}
	}
}

func TestInvalidRoutesDoNotPartiallyApply(t *testing.T) {
	called := false
	s := &InterfaceService{run: func(string, ...string) ([]byte, error) { called = true; return nil, nil }}
	if err := s.AddRouteForPeer("wgm1", "192.168.1.0/24,garbage"); err == nil || called {
		t.Fatal("invalid list partially applied")
	}
	if err := s.AddRouteForPeer("wg0", "192.168.0.0/24"); err == nil || called {
		t.Fatal("unmanaged interface accepted")
	}
}
