# Third-party components

This project is an independent WireGuard-based client, not the official WireGuard client.

- WireGuard Windows Go bindings (driver and winipcfg), v1.1.1: https://git.zx2c4.com/wireguard-windows/ (MIT; Copyright WireGuard LLC). License: https://git.zx2c4.com/wireguard-windows/tree/COPYING
- WireGuardNT prebuilt driver 1.1: https://download.wireguard.com/wireguard-nt/wireguard-nt-1.1.zip. The unchanged driver is embedded and accessed through its permitted API. The distribution includes its complete prebuilt binary license in `WireGuardNT-LICENSE.txt`; this driver has its own terms, separate from the Go bindings.
- Wails v2: https://github.com/wailsapp/wails (MIT).
- React: https://github.com/facebook/react (MIT).
- Go / golang.org/x packages: https://go.dev/LICENSE (BSD-3-Clause).
- winres (build tool): https://github.com/tc-hib/winres (MIT).

Dependency versions are pinned in go.mod, go.sum, and frontend/package-lock.json. Redistribution must retain the relevant licenses and copyright notices; the distribution includes complete dependency notices in the `licenses/` directory.
