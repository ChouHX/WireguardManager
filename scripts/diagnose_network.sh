#!/bin/sh
# Read-only observations; never print wg dump/showconf/private-key/PSK values.
set -u

usage() {
  echo "Usage: sudo sh scripts/diagnose_network.sh wgm1 10.100.1.2 [capture-seconds: 0..60]" >&2
}
ipv4() {
  printf '%s\n' "$1" | awk -F. 'NF!=4 {exit 1} {for(i=1;i<=4;i++) if($i !~ /^[0-9]+$/ || length($i)>3 || $i+0>255) exit 1}'
}
if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then usage; exit 2; fi
link=$1
peer_ip=$2
capture_seconds=${3:-20}
if ! printf '%s\n' "$link" | awk '$0 !~ /^wgm[1-9][0-9]*$/ || length($0)>6 || substr($0,4)+0>254 {exit 1}'; then usage; exit 2; fi
if ! ipv4 "$peer_ip"; then usage; exit 2; fi
case $capture_seconds in
  [0-9]|[1-5][0-9]|60) ;;
  *) usage; exit 2 ;;
esac
if [ "$(id -u)" -ne 0 ]; then echo "Run as root to read network state and packet headers." >&2; exit 1; fi
for required in ip wg timeout awk; do
  if ! command -v "$required" >/dev/null; then echo "Missing command: $required" >&2; exit 1; fi
done
if ! ip link show dev "$link" >/dev/null 2>&1; then echo "Interface does not exist: $link" >&2; exit 1; fi

observe() {
  printf '\n$'; printf ' %s' "$@"; printf '\n'
  if ! command -v "$1" >/dev/null; then echo "[not installed]"; return; fi
  timeout 8 "$@" 2>&1 || echo "[command failed or timed out; continuing]"
}
port=$(wg show "$link" listen-port) || exit 1
if ! printf '%s\n' "$port" | awk '$0 !~ /^[0-9]+$/ || $0+0<1 || $0+0>65535 {exit 1}'; then echo "No active WireGuard UDP port" >&2; exit 1; fi
server_ip=$(ip -4 -o addr show dev "$link" | awk 'NR==1 {split($4, a, "/"); print a[1]}')
peer_key=$(wg show "$link" allowed-ips | awk -v ip="$peer_ip/32" '{n=split($2,a,","); for(i=1;i<=n;i++) if(a[i]==ip) print $1}')
endpoint=$(wg show "$link" endpoints | awk -v key="$peer_key" '$1==key {print $2}')
endpoint_ip=${endpoint%:*}
table=$((20000 + ${link#wgm}))

printf 'WireGuard network observations · %s UTC\n' "$(date -u +%FT%T)"
printf 'Interface=%s Peer=%s Endpoint=%s\n' "$link" "$peer_ip" "${endpoint:-(unknown)}"
echo "Contains IP addresses, public keys and firewall rules; no private keys or packet payload dumps."
echo "Keep the client connected and pinging its cloud tunnel IP during collection."
observe uname -r
for field in public-key listen-port fwmark allowed-ips endpoints latest-handshakes transfer; do observe wg show "$link" "$field"; done
printf '\nPSK enabled per public key (values omitted):\n'
wg show "$link" preshared-keys | awk '{print $1, ($2=="(none)" ? "disabled" : "enabled")}'
observe ip -details -statistics link show dev "$link"
observe ip -4 addr show dev "$link"
observe ip -4 rule show
observe ip -4 route show table all
observe ip -4 route get "$peer_ip"
if ipv4 "$server_ip"; then observe ip -4 route get "$peer_ip" from "$server_ip"; fi
if ipv4 "$endpoint_ip"; then
  observe ip -4 route get "$endpoint_ip"
  outer_dev=$(ip -4 route get "$endpoint_ip" | awk '{for(i=1;i<NF;i++) if($i=="dev") {print $(i+1);exit}}')
  if [ -n "$outer_dev" ]; then
    observe ip -statistics link show dev "$outer_dev"
    observe sysctl "net.ipv4.conf.$outer_dev.rp_filter"
    observe tc -s qdisc show dev "$outer_dev"
  fi
fi
observe sysctl net.ipv4.ip_forward net.ipv4.conf.all.rp_filter "net.ipv4.conf.$link.rp_filter"
observe tc -s qdisc show dev "$link"
observe tc -s filter show dev "$link" ingress
observe iptables --version
observe iptables-save -c
# Native nft rules may coexist with iptables and drop packets later in a hook.
if command -v nft >/dev/null; then observe nft -a list ruleset; fi
if command -v iptables-legacy-save >/dev/null; then observe iptables-legacy-save -c; fi
if command -v conntrack >/dev/null; then
  observe conntrack -L -p udp --dport "$port"
  observe conntrack -L -p udp --sport "$port"
  observe conntrack -L --zone "$table"
fi

if [ "$capture_seconds" -gt 0 ]; then
  if command -v tcpdump >/dev/null; then
    filter="(udp port $port) or (icmp and host $peer_ip)"
    # Capture replies whose source port was rewritten by host NAT too.
    if ipv4 "$endpoint_ip"; then filter="($filter) or (udp and host $endpoint_ip)"; fi
    printf '\nPacket headers for %s seconds (up to 400 packets):\n' "$capture_seconds"
    timeout --signal=INT "$capture_seconds" tcpdump -p -i any -nn -tttt -q -l -s 96 -c 400 "$filter" 2>&1
    capture_result=$?
    if [ "$capture_result" -ne 0 ] && [ "$capture_result" -ne 124 ]; then echo "[capture failed: $capture_result]"; fi
  else
    echo "[tcpdump not installed; packet capture skipped]"
  fi
fi
echo
echo "After observation (compare handshake timestamps, endpoints and counters):"
for field in endpoints latest-handshakes transfer; do observe wg show "$link" "$field"; done
observe iptables-save -c
echo
echo "A handshake timestamp of 0 means no completed handshake on the current peer instance."
echo "148-byte UDP initiations and 92-byte replies alone do not establish a working data tunnel."
echo "An outbound capture proves the packet reached the host egress capture point, not client receipt."
