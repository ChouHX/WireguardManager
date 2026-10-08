package service

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Config and Profile never cross the Wails bridge: they contain private keys.
type Config struct {
	PrivateKey   [32]byte
	PublicKey    [32]byte
	PresharedKey [32]byte
	Address      netip.Prefix
	Endpoint     string
	Keepalive    uint16
	MTU          uint32
	BaseRoutes   []netip.Prefix
	DeviceLANs   []netip.Prefix
}

type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Config    Config `json:"config"`
	Targets   string `json:"targets"`
	AdapterID string `json:"adapterID"`
}

type ProfileView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Address       string `json:"address"`
	DeviceLANs    string `json:"deviceLANs"`
	TunnelNetwork string `json:"tunnelNetwork"`
	Targets       string `json:"targets"`
	AdapterID     string `json:"adapterID"`
}

func (p Profile) View() ProfileView {
	return ProfileView{p.ID, p.Name, p.Config.Endpoint, p.Config.Address.String(), JoinPrefixes(p.Config.DeviceLANs), JoinPrefixes(p.Config.BaseRoutes), p.Targets, p.AdapterID}
}

func JoinPrefixes(prefixes []netip.Prefix) string {
	values := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		values = append(values, p.String())
	}
	return strings.Join(values, ", ")
}

func ParsePrefixes(raw string) ([]netip.Prefix, error) {
	prefixes := []netip.Prefix{}
	seen := map[netip.Prefix]bool{}
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ';' }) {
		p, err := netip.ParsePrefix(token)
		if err != nil {
			ip, e := netip.ParseAddr(token)
			if e != nil {
				return nil, fmt.Errorf("无效的 IP / 网段：%s", token)
			}
			p = netip.PrefixFrom(ip, ip.BitLen())
		}
		if !p.Addr().Is4() || p.Bits() == 0 || p.Addr().IsUnspecified() || p.Addr().IsLoopback() || p.Addr().IsMulticast() || p.Addr() == netip.MustParseAddr("255.255.255.255") {
			return nil, errors.New("仅支持 IPv4 单播目标，不支持默认路由")
		}
		p = p.Masked()
		if !seen[p] {
			prefixes = append(prefixes, p)
			seen[p] = true
		}
	}
	if len(prefixes) > 256 {
		return nil, errors.New("最多配置 256 条路由")
	}
	return prefixes, nil
}

func parseKey(raw string) (key [32]byte, err error) {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return key, errors.New("WireGuard 密钥格式无效")
	}
	copy(key[:], b)
	if key == [32]byte{} {
		return key, errors.New("WireGuard 密钥不能为零")
	}
	return key, nil
}

func ParseConfig(raw string) (Config, error) {
	c := Config{Keepalive: 25, MTU: 1420}
	if len(raw) > 64*1024 {
		return c, errors.New("配置文件过大")
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(raw, "\ufeff")))
	section := ""
	sections := map[string]bool{}
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# WGM-Device-LAN =") {
			var err error
			c.DeviceLANs, err = ParsePrefixes(strings.TrimSpace(strings.TrimPrefix(line, "# WGM-Device-LAN =")))
			if err != nil {
				return c, err
			}
			continue
		}
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if line != "[Interface]" && line != "[Peer]" {
				return c, errors.New("仅支持一个 Interface 和一个云端 Peer")
			}
			if sections[line] {
				return c, errors.New("不支持多个 Interface 或 Peer")
			}
			sections[line] = true
			section = line
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || section == "" {
			return c, errors.New("配置格式无效")
		}
		field, value := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		name := section + field
		if seen[name] {
			return c, fmt.Errorf("重复配置项：%s", field)
		}
		seen[name] = true
		var err error
		switch name {
		case "[Interface]PrivateKey":
			c.PrivateKey, err = parseKey(value)
		case "[Interface]Address":
			c.Address, err = netip.ParsePrefix(value)
			if err == nil && (!c.Address.Addr().Is4() || !c.Address.Addr().IsPrivate() || c.Address.Bits() != 32) {
				err = errors.New("隧道地址必须是 IPv4 私有地址 /32，请重新从平台导出配置")
			}
		case "[Interface]MTU":
			var n uint64
			n, err = strconv.ParseUint(value, 10, 32)
			if err == nil && (n < 576 || n > 9000) {
				err = errors.New("MTU 应在 576–9000 之间")
			}
			c.MTU = uint32(n)
		case "[Interface]DNS": // Preserve the machine's DNS for split tunnel use; no global DNS override.
		case "[Peer]PublicKey":
			c.PublicKey, err = parseKey(value)
		case "[Peer]PresharedKey":
			c.PresharedKey, err = parseKey(value)
		case "[Peer]Endpoint":
			var host, port string
			host, port, err = net.SplitHostPort(value)
			if err == nil {
				var n uint64
				n, err = strconv.ParseUint(port, 10, 16)
				if host == "" || n == 0 {
					err = errors.New("Endpoint 必须包含主机和有效 UDP 端口")
				}
			}
			c.Endpoint = value
		case "[Peer]AllowedIPs":
			c.BaseRoutes, err = ParsePrefixes(value)
		case "[Peer]PersistentKeepalive":
			var n uint64
			n, err = strconv.ParseUint(value, 10, 16)
			c.Keepalive = uint16(n)
		default:
			return c, fmt.Errorf("不支持配置项 %s；请使用平台新导出的配置（不支持脚本钩子）", field)
		}
		if err != nil {
			return c, fmt.Errorf("%s：配置值无效", field)
		} // Never echo secret values.
	}
	if err := scanner.Err(); err != nil {
		return c, errors.New("读取配置失败")
	}
	if c.PrivateKey == [32]byte{} || c.PublicKey == [32]byte{} || !c.Address.IsValid() || c.Endpoint == "" {
		return c, errors.New("缺少密钥、隧道地址或云端 Endpoint")
	}
	// Import only platform split-tunnel exports. Destinations must be selected locally.
	if len(c.BaseRoutes) != 1 || !c.BaseRoutes[0].Contains(c.Address.Addr()) || c.BaseRoutes[0].Bits() < 16 {
		return c, errors.New("AllowedIPs 必须仅包含所属租户的隧道网段，请重新从平台导出配置")
	}
	for _, p := range c.DeviceLANs {
		if p.Overlaps(c.BaseRoutes[0]) {
			return c, errors.New("设备局域网不能与隧道网段重叠")
		}
	}
	return c, nil
}

func (p Profile) Routes() ([]netip.Prefix, error) {
	targets, err := ParsePrefixes(p.Targets)
	if err != nil {
		return nil, err
	}
	routes := append([]netip.Prefix{}, p.Config.BaseRoutes...)
	for _, target := range targets {
		for _, lan := range p.Config.DeviceLANs {
			if lan.Overlaps(target) {
				return nil, errors.New("访问目标不能与本设备声明的局域网重叠；请使用另一台设备的配置访问该现场")
			}
		}
		for _, base := range p.Config.BaseRoutes {
			if base.Overlaps(target) {
				return nil, errors.New("访问目标不能与隧道网段重叠")
			}
		}
		routes = append(routes, target)
	}
	return routes, nil
}
