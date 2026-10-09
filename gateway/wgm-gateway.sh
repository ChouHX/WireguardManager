#!/bin/sh
# WireGuard Manager gateway bootstrap v1 — owned files and rules only.
set -eu
umask 077
DIR=/etc/wireguard-manager
BIN=/usr/libexec/wgm-gateway
IF=wgm-gw
FWD=WGM-GATEWAY-FWD
NAT=WGM-GATEWAY-NAT
fail() { echo "$*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || fail '请以 root 执行'
for cmd in ip wg awk sysctl; do command -v "$cmd" >/dev/null || fail "缺少 $cmd；请先安装 ip / wireguard-tools"; done
is_openwrt() { [ -f /etc/openwrt_release ] && command -v uci >/dev/null; }
openwrt() { is_openwrt && command -v fw4 >/dev/null; }
owned_service() { [ ! -e "$1" ] || grep -q '^# WireGuard Manager gateway$\|^Description=WireGuard Manager gateway$' "$1"; }
route_check() {
 if ip -4 route show exact "$NETWORK" | grep -v "dev $IF " | grep -q .; then fail "隧道网段 $NETWORK 与已有路由冲突"; fi
}
read_config() {
 [ -f "$DIR/owned" ] || fail '尚未安装网关配置'
 ADDRESS=$(cat "$DIR/address")
 NETWORK=$(cat "$DIR/network")
}
firewall() {
 read_config
 if openwrt; then
  # fw4 retains its other zones/rules. Its explicit source-zone rule admits this
  # tunnel, while this separate NAT table changes only traffic arriving on it.
  if nft list table ip wgm_gateway >/dev/null 2>&1; then nft delete table ip wgm_gateway; fi
  nft -f - <<EOF
table ip wgm_gateway {
 chain postrouting {
  type nat hook postrouting priority srcnat; policy accept;
  iifname "$IF" oifname != "$IF" ip saddr $NETWORK masquerade
 }
}
EOF
 else
  iptables -w 5 -N "$FWD" 2>/dev/null || true
  iptables -w 5 -t nat -N "$NAT" 2>/dev/null || true
  iptables -w 5 -F "$FWD"
  iptables -w 5 -t nat -F "$NAT"
  iptables -w 5 -A "$FWD" -i "$IF" ! -o "$IF" -s "$NETWORK" -j ACCEPT
  iptables -w 5 -A "$FWD" ! -i "$IF" -o "$IF" -d "$NETWORK" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  iptables -w 5 -t nat -A "$NAT" -s "$NETWORK" ! -o "$IF" -j MASQUERADE
  iptables -w 5 -C FORWARD -j "$FWD" 2>/dev/null || iptables -w 5 -I FORWARD 1 -j "$FWD"
  iptables -w 5 -t nat -C POSTROUTING -j "$NAT" 2>/dev/null || iptables -w 5 -t nat -I POSTROUTING 1 -j "$NAT"
 fi
}
down() {
 [ -f "$DIR/owned" ] || return 0
 if ip link show dev "$IF" >/dev/null 2>&1; then
  ip -o link show dev "$IF" | grep -q 'alias wireguard-manager-gateway' || fail '拒绝删除非本程序创建的网卡'
  ip link del dev "$IF"
 fi
 if openwrt; then
  nft delete table ip wgm_gateway 2>/dev/null || true
 else
  while iptables -w 5 -C FORWARD -j "$FWD" 2>/dev/null; do iptables -w 5 -D FORWARD -j "$FWD"; done
  while iptables -w 5 -t nat -C POSTROUTING -j "$NAT" 2>/dev/null; do iptables -w 5 -t nat -D POSTROUTING -j "$NAT"; done
  iptables -w 5 -F "$FWD" 2>/dev/null || true
  iptables -w 5 -X "$FWD" 2>/dev/null || true
  iptables -w 5 -t nat -F "$NAT" 2>/dev/null || true
  iptables -w 5 -t nat -X "$NAT" 2>/dev/null || true
 fi
 # Leave forwarding enabled: it is a shared host setting, and another service
 # may have started depending on it after this gateway was installed.
}
up() {
 read_config
 route_check
 if ip link show dev "$IF" >/dev/null 2>&1; then
  ip -o link show dev "$IF" | grep -q 'alias wireguard-manager-gateway' || fail '网卡名称已被其他程序占用'
 else
  ip link add dev "$IF" type wireguard
  ip link set dev "$IF" alias wireguard-manager-gateway
 fi
 wg setconf "$IF" "$DIR/wireguard.conf"
 ip address replace "$ADDRESS" dev "$IF"
 ip link set dev "$IF" mtu 1420 up
 # Only the fixed tenant tunnel prefix is routed through WG. LAN lookup keeps
 # using the gateway's existing routes, regardless of later cloud target edits.
 ip -4 route replace "$NETWORK" dev "$IF"
 sysctl -w net.ipv4.ip_forward=1 >/dev/null
 firewall
}
install_gateway() {
 [ "$#" = 1 ] && [ -f "$1" ] || fail '用法：wgm-gateway.sh install <WireGuard 配置>'
 owned_service /etc/init.d/wgm-gateway || fail "同名开机服务已存在"
 owned_service /etc/systemd/system/wgm-gateway.service || fail "同名 systemd 服务已存在"
 if is_openwrt; then
  [ -z "$(uci changes firewall)" ] || fail "存在未提交的防火墙修改，请先自行保存或撤销后重试"
 fi
 if openwrt; then command -v nft >/dev/null || fail '需要 OpenWrt fw4/nftables'; else command -v iptables >/dev/null || fail '需要 iptables；OpenWrt 请使用带 fw4 的系统'; fi
 [ ! -e "$DIR" ] || [ -f "$DIR/owned" ] || fail "$DIR 已存在且不属于本程序"
 [ ! -e "$BIN" ] || [ -f "$DIR/owned" ] || fail "$BIN 已存在且不属于本程序"
 if [ ! -f "$DIR/owned" ]; then
  ip link show dev "$IF" >/dev/null 2>&1 && fail '网卡 wgm-gw 已存在，未修改原网络'
  if is_openwrt; then
   for section in wgm_gateway_zone wgm_gateway_forward wgm_gateway_reload; do uci -q get "firewall.$section" >/dev/null && fail '同名防火墙配置已存在'; done
  fi
  if openwrt; then
   nft list table ip wgm_gateway >/dev/null 2>&1 && fail '同名 nft 表已存在'
  else
   iptables -w 5 -S "$FWD" >/dev/null 2>&1 && fail '同名防火墙链已存在'
   iptables -w 5 -t nat -S "$NAT" >/dev/null 2>&1 && fail '同名 NAT 链已存在'
  fi
 fi
 tmp=$(mktemp -d)
 fresh=0
 cleanup_install() {
  result=$?
  trap - EXIT
  if [ "$result" != 0 ] && [ "$fresh" = 1 ]; then
   # Roll back only this attempted enrollment, never a previous installation.
   (set +e; uninstall_gateway) >&2 || echo "初始化回滚未完成，请执行 $BIN uninstall 重试" >&2
  fi
  rm -f "$tmp/address" "$tmp/network" "$tmp/wg" "$tmp/key"
  rmdir "$tmp"
  exit "$result"
 }
 trap cleanup_install EXIT
 trap 'exit 1' HUP INT TERM
 # Never execute wg-quick hooks from imported configuration.
 awk -v dir="$tmp" '
 /^[[:space:]]*[#;]/ {next}
 {gsub(/\r/, "")}
 /^\[Interface\]$/ {section="interface"; interfaces++;print;next}
 /^\[Peer\]$/ {section="peer";peers++;print;next}
 /^[[:space:]]*$/ {next}
 {split($0,a,"=");key=a[1];gsub(/[[:space:]]/,"",key);value=substr($0,index($0,"=")+1);gsub(/^[[:space:]]+|[[:space:]]+$/,"",value)}
 section=="interface"&&key=="Address" {if(address++)exit 2;print value >dir"/address";next}
 section=="interface"&&key=="PrivateKey" {if(private++)exit 2;print value >dir"/key";print;next}
 section=="peer"&&key=="AllowedIPs" {if(allowed++)exit 2;print value >dir"/network";print;next}
 section=="peer"&&(key=="PublicKey"||key=="PresharedKey"||key=="Endpoint"||key=="PersistentKeepalive") {if(seen[key]++)exit 2;print;next}
 section=="interface"&&(key=="DNS"||key=="MTU") {next}
 {exit 2}
 END {if(interfaces!=1||peers!=1||address!=1||private!=1||allowed!=1||seen["PublicKey"]!=1||seen["Endpoint"]!=1)exit 2}
 ' "$1" >> "$tmp/wg" || fail '仅支持单个租户 Peer 的标准配置，不支持执行 hooks'
 ADDRESS=$(cat "$tmp/address");NETWORK=$(cat "$tmp/network")
 # Manager allocations are 10.100.N.0/24. A stable prefix permits gateway
 # return traffic for every access terminal without per-target device changes.
 printf '%s\n%s\n' "$ADDRESS" "$NETWORK" | awk '
 NR==1 {n=split($0,a,/[.\/]/);if($0!~/^[0-9.]+\/32$/||n!=5||a[1]!=10||a[2]!=100||a[3]<1||a[3]>254||a[4]<2||a[4]>254||a[5]!=32)exit 1;subnet=a[3]}
 NR==2 {if($0!="10.100."subnet".0/24")exit 1}
 ' || fail '配置必须使用平台分配的独立隧道网段'
 route_check
 if [ -f "$DIR/owned" ]; then
  cmp -s "$tmp/wg" "$DIR/wireguard.conf" && cmp -s "$tmp/address" "$DIR/address" || fail "已安装其他网关配置；请先执行 $BIN uninstall 再接入"
  up
  echo "该网关已初始化，无需重复安装"
  return
 fi
 pub=$(wg pubkey < "$tmp/key")
 for link in $(wg show interfaces); do
  [ "$link" = "$IF" ] && continue
  [ "$(wg show "$link" public-key)" != "$pub" ] || fail "该配置已由 $link 使用，请先停用原连接，避免重复身份"
 done
 mkdir -p "$DIR" /usr/libexec
 chmod 700 "$DIR"
 cp "$tmp/wg" "$DIR/wireguard.conf"
 cp "$tmp/address" "$DIR/address"
 cp "$tmp/network" "$DIR/network"
 touch "$DIR/owned"
 fresh=1
 [ "$0" = "$BIN" ] || cp "$0" "$BIN"
 chmod 700 "$BIN"
 if is_openwrt; then
  uci set firewall.wgm_gateway_zone=zone
  uci set firewall.wgm_gateway_zone.name=wgm_gateway
  uci set firewall.wgm_gateway_zone.input=REJECT
  uci set firewall.wgm_gateway_zone.output=ACCEPT
  uci set firewall.wgm_gateway_zone.forward=REJECT
  uci -q delete firewall.wgm_gateway_zone.device || true
  uci add_list firewall.wgm_gateway_zone.device="$IF"
  uci set firewall.wgm_gateway_forward=rule
  uci set firewall.wgm_gateway_forward.name=WireGuard-Manager-Gateway
  uci set firewall.wgm_gateway_forward.src=wgm_gateway
  uci set firewall.wgm_gateway_forward.dest='*'
  uci set firewall.wgm_gateway_forward.src_ip="$NETWORK"
  uci set firewall.wgm_gateway_forward.proto=all
  uci set firewall.wgm_gateway_forward.family=ipv4
  uci set firewall.wgm_gateway_forward.target=ACCEPT
  uci set firewall.wgm_gateway_reload=include
  uci set firewall.wgm_gateway_reload.type=script
  uci set firewall.wgm_gateway_reload.path="$DIR/firewall-reload"
  uci set firewall.wgm_gateway_reload.fw4_compatible=1
  uci set firewall.wgm_gateway_reload.reload=1
  printf '#!/bin/sh\nexec %s firewall\n' "$BIN" > "$DIR/firewall-reload"
  chmod 700 "$DIR/firewall-reload"
  uci commit firewall
  cat > /etc/init.d/wgm-gateway <<'EOF'
#!/bin/sh /etc/rc.common
# WireGuard Manager gateway
START=95
STOP=10
start() { /usr/libexec/wgm-gateway up; }
stop() { /usr/libexec/wgm-gateway down; }
EOF
  chmod 755 /etc/init.d/wgm-gateway
  /etc/init.d/wgm-gateway enable
  /etc/init.d/firewall reload
 elif command -v systemctl >/dev/null && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/wgm-gateway.service <<'EOF'
[Unit]
Description=WireGuard Manager gateway
Wants=network-online.target
After=network-online.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/libexec/wgm-gateway up
ExecStop=/usr/libexec/wgm-gateway down
[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable wgm-gateway.service
 fi
 up
 echo '网关已接入：转发与回程 NAT 已启用。后续目标 IP / 网段仅在 Windows 客户端修改。'
 if ! is_openwrt && [ ! -d /run/systemd/system ]; then echo '此系统无 systemd/fw4；请将 /usr/libexec/wgm-gateway up 加入系统启动流程。'; fi
}
uninstall_gateway() {
 if is_openwrt; then
  [ -z "$(uci changes firewall)" ] || [ "${fresh:-0}" = 1 ] || fail "存在未提交的防火墙修改，请先保存或撤销"
 fi
 [ -f "$DIR/owned" ] || fail '未找到本程序安装记录'
 down
 if is_openwrt; then
  [ ! -f /etc/init.d/wgm-gateway ] || /etc/init.d/wgm-gateway disable
  for section in wgm_gateway_zone wgm_gateway_forward wgm_gateway_reload; do uci -q delete "firewall.$section" || true; done
  uci commit firewall
  /etc/init.d/firewall reload
  rm -f /etc/init.d/wgm-gateway
 elif [ -f /etc/systemd/system/wgm-gateway.service ]; then
  systemctl disable wgm-gateway.service
  rm -f /etc/systemd/system/wgm-gateway.service
  systemctl daemon-reload
 fi
 rm -f "$DIR/wireguard.conf" "$DIR/address" "$DIR/network" "$DIR/owned" "$DIR/firewall-reload" "$BIN"
 rmdir "$DIR"
}
case ${1:-} in
 install) shift;install_gateway "$@" ;;
 up) up ;;
 down) down ;;
 uninstall) uninstall_gateway ;;
 firewall) firewall ;;
 *) fail '用法：install <配置> | up | down | firewall | uninstall' ;;
esac
