// Package gateway supplies an architecture-independent Linux/OpenWrt bootstrap.
package gateway

import (
	_ "embed"
	"encoding/base64"
	"fmt"
)

//go:embed wgm-gateway.sh
var installer string

// SetupScript is an authenticated configuration export. It contains this
// gateway's credentials and must never be logged or sent to the desktop UI.
func SetupScript(config string) string {
	return fmt.Sprintf(`#!/bin/sh
# WireGuard Manager gateway enrollment. Contains this gateway's private key.
set -eu
umask 077
[ "$(id -u)" = 0 ] || { echo "请以 root 执行" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -f "$work/config" "$work/install.sh"; rmdir "$work"' EXIT
trap 'exit 1' HUP INT TERM
base64 -d > "$work/config" <<'WGM_CONFIG'
%s
WGM_CONFIG
base64 -d > "$work/install.sh" <<'WGM_INSTALLER'
%s
WGM_INSTALLER
sh "$work/install.sh" install "$work/config"
`, base64.StdEncoding.EncodeToString([]byte(config)), base64.StdEncoding.EncodeToString([]byte(installer)))
}
