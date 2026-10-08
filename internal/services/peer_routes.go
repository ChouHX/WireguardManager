package services

import (
	"cloud-platform/internal/models"
	"fmt"
	"net/netip"
	"strings"
)

// ValidatePeerRoutes only compares peers within one tenant. Overlapping LANs in
// different tenants are intentionally allowed; within a tenant they would steal
// WireGuard's cryptokey route from one another.
func ValidatePeerRoutes(peer *models.WireguardPeer, others []models.WireguardPeer) error {
	routes, err := PeerRoutes(peer.AllowedIPs)
	if err != nil {
		return err
	}
	reserved := netip.MustParsePrefix("10.100.0.0/16")
	self := peer.PeerAddress + "/32"
	for _, route := range routes {
		prefix := netip.MustParsePrefix(route)
		if prefix.Overlaps(reserved) && route != self {
			return fmt.Errorf("route %s overlaps the reserved tunnel address pool", route)
		}
		for _, other := range others {
			if other.ID == peer.ID || other.ServerID != peer.ServerID {
				continue
			}
			otherRoutes, err := PeerRoutes(ServerAllowedIPs(&other))
			if err != nil {
				return fmt.Errorf("existing peer %d has invalid routes: %w", other.ID, err)
			}
			for _, existing := range otherRoutes {
				if prefix.Overlaps(netip.MustParsePrefix(existing)) {
					return fmt.Errorf("route %s overlaps peer %d within this tenant", route, other.ID)
				}
			}
		}
	}
	return nil
}

// NormalizeClientRoutes accepts IPv4 addresses (/32) and prefixes, but never a
// default route. These destinations belong in the client export, not in the
// server-side AllowedIPs for that client's peer.
func NormalizeClientRoutes(value string) (string, error) {
	for _, part := range strings.Split(value, ",") {
		if isDefaultRoute(strings.TrimSpace(part)) {
			return "", fmt.Errorf("client target routes must not contain a default route")
		}
	}
	routes, err := PeerRoutes(value)
	return strings.Join(routes, ", "), err
}

// ChangePeerRoutes installs new routes first. On failure it removes only routes
// added by this call; existing destinations remain reachable.
func (s *InterfaceService) ChangePeerRoutes(link, oldIPs, newIPs string) error {
	oldRoutes, err := PeerRoutes(oldIPs)
	if err != nil {
		return err
	}
	newRoutes, err := PeerRoutes(newIPs)
	if err != nil {
		return err
	}
	difference := func(a, b []string) []string {
		seen := map[string]bool{}
		for _, route := range b {
			seen[route] = true
		}
		var result []string
		for _, route := range a {
			if !seen[route] {
				result = append(result, route)
			}
		}
		return result
	}
	added := strings.Join(difference(newRoutes, oldRoutes), ",")
	removed := strings.Join(difference(oldRoutes, newRoutes), ",")
	if err := s.AddRouteForPeer(link, added); err != nil {
		return err
	}
	if err := s.DeleteRouteForPeer(link, removed); err != nil {
		_ = s.AddRouteForPeer(link, removed)
		_ = s.DeleteRouteForPeer(link, added)
		return err
	}
	return nil
}

// SetTenantEnabled restores all device routes lost when Linux took the interface
// down. Any restore failure closes the UDP listener again.
func (s *InterfaceService) SetTenantEnabled(server *models.WireguardServer, peers []models.WireguardPeer, enabled bool) (result error) {
	if !enabled {
		return s.SetLinkState(server.WgInterface, false)
	}
	defer func() {
		if result != nil {
			_ = s.SetLinkState(server.WgInterface, false)
		}
	}()
	if err := s.ConfigureRouting(server.WgInterface, server.WgAddress); err != nil {
		return err
	}
	for _, peer := range peers {
		if err := s.AddRouteForPeer(server.WgInterface, ServerAllowedIPs(&peer)); err != nil {
			return err
		}
	}
	return nil
}
