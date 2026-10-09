package services

import (
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

const NetworkModeMultiInterface = "multi-interface"

// wgm1..wgm254 and tables 20001..20254 are reserved for this application.
// Policy rules select these tables; tenant LAN routes never enter main.
func TenantInterface(subnetID int) string { return fmt.Sprintf("wgm%d", subnetID) }

func TenantTable(link string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(link, "wgm"))
	if err != nil || id < 1 || id > maxSubnetID || link != TenantInterface(id) {
		return 0, fmt.Errorf("invalid managed interface %q", link)
	}
	return 20000 + id, nil
}

type InterfaceService struct {
	run func(string, ...string) ([]byte, error)
}

func NewInterfaceService() *InterfaceService {
	return &InterfaceService{run: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}}
}

func (s *InterfaceService) command(name string, args ...string) error {
	out, err := s.run(name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *InterfaceService) CreateWireguardDevice(link string) error {
	if _, err := TenantTable(link); err != nil {
		return err
	}
	return s.command("ip", "link", "add", "dev", link, "type", "wireguard")
}

func (s *InterfaceService) LinkExists(link string) bool {
	_, err := s.run("ip", "link", "show", "dev", link)
	return err == nil
}

func (s *InterfaceService) DeleteLinkInHost(link string) error {
	out, err := s.run("ip", "link", "del", "dev", link)
	if err != nil && !strings.Contains(string(out), "Cannot find device") {
		return fmt.Errorf("delete interface %s: %w: %s", link, err, out)
	}
	return nil
}

func (s *InterfaceService) SetLinkState(link string, up bool) error {
	state := "down"
	if up {
		state = "up"
	}
	return s.command("ip", "link", "set", "dev", link, state)
}

func (s *InterfaceService) SetLinkMTU(link string, mtu int) error {
	if mtu <= 0 {
		return nil
	}
	return s.command("ip", "link", "set", "dev", link, "mtu", strconv.Itoa(mtu))
}

// firewallMu also serializes rule check/add across concurrent account provisioning.
var firewallMu sync.Mutex

func (s *InterfaceService) ensureRule(table, chain string, rule ...string) error {
	args := []string{"-w", "5", "-t", table, "-C", chain}
	if _, err := s.run("iptables", append(args, rule...)...); err == nil {
		return nil
	}
	args = []string{"-w", "5", "-t", table, "-I", chain, "1"}
	return s.command("iptables", append(args, rule...)...)
}

// Each enabled interface owns its UDP admission rule. Tunnel ingress is excluded
// so admitting an outer handshake never bypasses tenant host-access isolation.
func (s *InterfaceService) SetUDPAdmission(link string, port int, enabled bool) error {
	if _, err := TenantTable(link); err != nil {
		return err
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid WireGuard UDP port %d", port)
	}
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if err := s.removeUDPAdmission(link, port, enabled); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	return s.ensureRule("filter", "INPUT", "!", "-i", "wgm+", "-p", "udp", "--dport", strconv.Itoa(port), "-m", "comment", "--comment", "wgm-udp-"+link, "-j", "ACCEPT")
}

// Remove only rules carrying this interface's exact ownership marker. Retain
// the current rule on reapplication; drop obsolete ports if an allocation moved.
// Caller holds firewallMu. No global chain is flushed or restored.
func (s *InterfaceService) removeUDPAdmission(link string, keepPort int, keep bool) error {
	out, err := s.run("iptables", "-w", "5", "-t", "filter", "-S", "INPUT")
	if err != nil {
		return fmt.Errorf("read UDP admission rules: %w: %s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		owned, port := false, ""
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "--comment" && strings.Trim(fields[i+1], "\"") == "wgm-udp-"+link {
				owned = true
			}
			if fields[i] == "--dport" {
				port = fields[i+1]
			}
		}
		if !owned || (keep && port == strconv.Itoa(keepPort)) || len(fields) < 2 || fields[0] != "-A" {
			continue
		}
		fields[0] = "-D"
		for i := range fields {
			fields[i] = strings.Trim(fields[i], "\"")
		}
		if err := s.command("iptables", append([]string{"-w", "5", "-t", "filter"}, fields...)...); err != nil {
			return err
		}
	}
	return nil
}

// EnsureIsolation installs a guard before any existing FORWARD accept rule.
// A packet can only leave through the same tenant interface it arrived on.
// Conntrack zones separate identical LAN tuples belonging to different tenants.
func (s *InterfaceService) EnsureIsolation(link, address string) error {
	table, err := TenantTable(link)
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil || !prefix.Addr().Is4() {
		return fmt.Errorf("invalid IPv4 tunnel address %q", address)
	}
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if _, err := s.run("iptables", "-w", "5", "-t", "filter", "-S", "WGM-FORWARD"); err != nil {
		if err := s.command("iptables", "-w", "5", "-t", "filter", "-N", "WGM-FORWARD"); err != nil {
			return err
		}
	}
	// Insertion order is deliberate: tenant-specific accepts precede both rejects.
	for _, rule := range [][]string{
		{"-o", "wgm+", "-j", "REJECT"},
		{"-i", "wgm+", "-j", "REJECT"},
		{"-i", link, "-o", link, "-j", "ACCEPT"},
	} {
		if err := s.ensureRule("filter", "WGM-FORWARD", rule...); err != nil {
			return err
		}
	}
	// Reinsert the jump at the head on startup, even if another application moved it.
	// Insert before removing duplicates so there is never an unguarded interval.
	if err := s.command("iptables", "-w", "5", "-I", "FORWARD", "1", "-j", "WGM-FORWARD"); err != nil {
		return err
	}
	out, err := s.run("iptables", "-w", "5", "-S", "FORWARD")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	position := 0
	var duplicatePositions []int
	for _, line := range lines {
		if !strings.HasPrefix(line, "-A FORWARD ") {
			continue
		}
		position++
		if position > 1 && line == "-A FORWARD -j WGM-FORWARD" {
			duplicatePositions = append(duplicatePositions, position)
		}
	}
	for i := len(duplicatePositions) - 1; i >= 0; i-- {
		if err := s.command("iptables", "-w", "5", "-D", "FORWARD", strconv.Itoa(duplicatePositions[i])); err != nil {
			return err
		}
	}
	for _, spec := range tenantFirewallRules(link, prefix.Addr().String(), table) {
		if err := s.ensureRule(spec.table, spec.chain, spec.rule...); err != nil {
			return err
		}
	}
	return nil
}

type firewallRule struct {
	table, chain string
	rule         []string
}

func tenantFirewallRules(link, serverIP string, zone int) []firewallRule {
	return []firewallRule{
		{"raw", "PREROUTING", []string{"-i", link, "-j", "CT", "--zone", strconv.Itoa(zone)}},
		{"raw", "OUTPUT", []string{"-o", link, "-j", "CT", "--zone", strconv.Itoa(zone)}},
		// A broad host MASQUERADE must not rewrite traffic returning into a tenant.
		{"nat", "POSTROUTING", []string{"-o", link, "-j", "ACCEPT"}},
		// Protect all host addresses (including other tenants' local tunnel IPs).
		{"filter", "INPUT", []string{"-i", link, "-j", "REJECT"}},
		{"filter", "INPUT", []string{"-i", link, "-d", serverIP, "-p", "icmp", "-j", "ACCEPT"}},
		{"filter", "INPUT", []string{"-i", link, "-d", serverIP, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}},
	}
}

// ConfigureRouting uses noprefixroute to avoid connected routes in main.
// The terminal unreachable route prevents fallback to the host default route.
// Binding a probe socket to the interface selects the same table via oif.
func (s *InterfaceService) ConfigureRouting(link, address string) error {
	table, err := TenantTable(link)
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil || !prefix.Addr().Is4() {
		return fmt.Errorf("invalid IPv4 tunnel address %q", address)
	}
	if err := s.EnsureIsolation(link, address); err != nil {
		return err
	}
	// Strict reverse-path filtering uses main and drops overlapping tenant LANs.
	// Loose mode on this interface overrides an all.rp_filter=1 host setting.
	for _, setting := range []string{
		"net.ipv4.ip_forward=1",
		"net.ipv4.conf." + link + ".rp_filter=2",
		"net.ipv4.conf." + link + ".send_redirects=0",
		"net.ipv4.conf." + link + ".accept_redirects=0",
		"net.ipv6.conf." + link + ".disable_ipv6=1",
	} {
		if err := s.command("sysctl", "-w", setting); err != nil {
			return err
		}
	}
	// Routes require an administratively UP nexthop. New devices have no key yet.
	if err := s.SetLinkState(link, true); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"-4", "addr", "replace", address, "dev", link, "noprefixroute"},
		{"-4", "route", "replace", "unreachable", "default", "table", strconv.Itoa(table), "metric", "4278198272"},
		{"-4", "route", "replace", prefix.Masked().String(), "dev", link, "src", prefix.Addr().String(), "table", strconv.Itoa(table)},
	} {
		if err := s.command("ip", args...); err != nil {
			return err
		}
	}
	// iif isolates forwarded traffic; oif selects bound probe sockets; the
	// server-source rule also routes local replies and reverse-path checks. A
	// loopback-iif destination rule handles unbound host applications before they
	// have a source address; only the unique tunnel subnet is eligible, never LANs.
	for offset, selector := range tenantPolicySelectors(link, prefix) {
		priority := table - 10000 + offset*1000
		args := append([]string{"priority", strconv.Itoa(priority)}, selector...)
		args = append(args, "lookup", strconv.Itoa(table))
		out, err := s.run("ip", append([]string{"-4", "rule", "show"}, args...)...)
		if err != nil {
			return fmt.Errorf("read tenant policy rule: %w: %s", err, out)
		}
		if strings.TrimSpace(string(out)) == "" {
			if err := s.command("ip", append([]string{"-4", "rule", "add"}, args...)...); err != nil {
				return err
			}
		}
	}

	return nil
}

func tenantPolicySelectors(link string, prefix netip.Prefix) [][]string {
	return [][]string{
		{"iif", link},
		{"oif", link},
		{"from", prefix.Addr().String() + "/32"},
		{"iif", "lo", "to", prefix.Masked().String()},
	}
}

// PeerRoutes rejects unsupported families and canonicalizes routes before any
// mutation. Defaults retain legacy semantics: they never become server routes.
func PeerRoutes(allowedIPs string) ([]string, error) {
	var routes []string
	seen := map[string]bool{}
	for _, value := range strings.Split(allowedIPs, ",") {
		value = strings.TrimSpace(value)
		if value == "" || isDefaultRoute(value) {
			continue
		}
		if ip, err := netip.ParseAddr(value); err == nil && ip.Is4() {
			value += "/32"
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 {
			return nil, fmt.Errorf("invalid IPv4 peer route %q", value)
		}
		value = prefix.Masked().String()
		if !seen[value] {
			routes = append(routes, value)
			seen[value] = true
		}
	}
	return routes, nil
}

func (s *InterfaceService) AddRouteForPeer(link, allowedIPs string) error {
	table, err := TenantTable(link)
	if err != nil {
		return err
	}
	routes, err := PeerRoutes(allowedIPs)
	if err != nil {
		return err
	}
	up, err := s.LinkIsUp(link)
	if err != nil {
		return err
	}
	// IPv4 removes device routes on DOWN. Restore them from DB when enabling.
	if !up {
		return nil
	}
	var added []string
	for _, route := range routes {
		out, err := s.run("ip", "-4", "route", "add", route, "dev", link, "table", strconv.Itoa(table))
		if err != nil {
			if strings.Contains(string(out), "File exists") {
				continue
			}
			for _, previous := range added {
				_ = s.DeleteRouteForPeer(link, previous)
			}
			return fmt.Errorf("add tenant route %s: %w: %s", route, err, out)
		}
		added = append(added, route)
	}
	return nil
}

func (s *InterfaceService) DeleteRouteForPeer(link, allowedIPs string) error {
	table, err := TenantTable(link)
	if err != nil {
		return err
	}
	routes, err := PeerRoutes(allowedIPs)
	if err != nil {
		return err
	}
	var failures []error
	for _, route := range routes {
		out, err := s.run("ip", "-4", "route", "del", route, "dev", link, "table", strconv.Itoa(table))
		if err != nil && !strings.Contains(string(out), "No such process") && !strings.Contains(string(out), "FIB table does not exist") {
			failures = append(failures, fmt.Errorf("delete tenant route %s: %w: %s", route, err, out))
		}
	}
	return errors.Join(failures...)
}

// Destroy removes only the resources selected by this tenant's reserved ID.
// Keep the shared guard in place; other tenants may still be active.
func (s *InterfaceService) Destroy(link, address string) error {
	table, err := TenantTable(link)
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil {
		return err
	}
	if err := s.DeleteLinkInHost(link); err != nil {
		return err
	}
	for offset, selector := range tenantPolicySelectors(link, prefix) {
		priority := table - 10000 + offset*1000
		args := append([]string{"-4", "rule", "del", "priority", strconv.Itoa(priority)}, selector...)
		args = append(args, "lookup", strconv.Itoa(table))
		for count := 0; ; count++ {
			if count >= maxRuleRemovals {
				return fmt.Errorf("too many duplicate rules for %s", link)
			}
			out, err := s.run("ip", args...)
			if err == nil {
				continue
			}
			if strings.Contains(string(out), "No such file") {
				break
			}
			return fmt.Errorf("delete policy rule: %w: %s", err, out)
		}
	}
	out, err := s.run("ip", "-4", "route", "flush", "table", strconv.Itoa(table))
	if err != nil && !strings.Contains(string(out), "FIB table does not exist") {
		return fmt.Errorf("flush tenant routes: %w: %s", err, out)
	}
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if err := s.removeUDPAdmission(link, 0, false); err != nil {
		return err
	}
	rules := tenantFirewallRules(link, prefix.Addr().String(), table)
	rules = append(rules, firewallRule{"filter", "WGM-FORWARD", []string{"-i", link, "-o", link, "-j", "ACCEPT"}})
	for _, spec := range rules {
		check := append([]string{"-w", "5", "-t", spec.table, "-C", spec.chain}, spec.rule...)
		for count := 0; ; count++ {
			if _, err := s.run("iptables", check...); err != nil {
				break
			}
			if count >= maxRuleRemovals {
				return fmt.Errorf("too many duplicate firewall rules for %s", link)
			}
			if err := s.command("iptables", append([]string{"-w", "5", "-t", spec.table, "-D", spec.chain}, spec.rule...)...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *InterfaceService) LinkIsUp(link string) (bool, error) {
	out, err := s.run("ip", "-o", "link", "show", "dev", link)
	if err != nil {
		return false, fmt.Errorf("read interface %s: %w: %s", link, err, out)
	}
	line := string(out)
	start, end := strings.Index(line, "<"), strings.Index(line, ">")
	if start < 0 || end <= start {
		return false, fmt.Errorf("invalid interface flags for %s", link)
	}
	for _, flag := range strings.Split(line[start+1:end], ",") {
		if flag == "UP" {
			return true, nil
		}
	}
	return false, nil
}
