package service

import (
	"errors"
	"strings"
)

// INetFwRule.RemoteAddresses rejects whitespace after commas even though the
// same comma-space format is appropriate in UI labels and WireGuard configs.
// Validate the complete scope before creating any firewall objects, and never
// interpret an empty list as allowing every remote address.
func firewallRemoteAddresses(raw string) (string, error) {
	prefixes, err := ParsePrefixes(raw)
	if err != nil {
		return "", err
	}
	if len(prefixes) == 0 {
		return "", errors.New("防火墙允许地址不能为空")
	}
	values := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		values[i] = prefix.String()
	}
	return strings.Join(values, ","), nil
}
