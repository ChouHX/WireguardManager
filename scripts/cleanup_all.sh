#!/usr/bin/env bash
#
# 清理平台在宿主机上创建的网络资源，以及本地数据与配置。
#
# ── 为什么需要单独一个脚本 ──
# 后端以 host 网络 + privileged 运行，账号的网络命名空间、隧道接口、以及宿主机侧
# 的 iptables 规则都是宿主内核对象。进程退出并不会带走它们，隧道会继续为客户端
# 服务 —— 这正是热更新 / 滚动重启能做到连接不中断的原因，因此后端刻意不在关闭
# 时拆除数据面。需要真正下架、迁移或彻底重置时，用本脚本收敛。
#
# 日常升级（docker compose pull && up -d）不要跑这个脚本，它会断开全部在线设备。
#
# 用法：
#   sudo ./scripts/cleanup_all.sh                 清理网络 + 数据 + 配置
#   sudo ./scripts/cleanup_all.sh --yes           跳过确认（脚本化调用）
#   sudo ./scripts/cleanup_all.sh --dry-run       只列出将要执行的操作
#   sudo ./scripts/cleanup_all.sh --network-only  只清网络，保留数据与配置
#
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# 配置目录与后端默认值保持一致，可用环境变量覆盖
CONFIG_DIR="${WM_NETWORK_CONFIG_DIR:-/etc/wg_config}"
# 数据库路径同理，默认落在仓库的 data/ 下
DB_PATH="${WM_DB_PATH:-${REPO_ROOT}/data/cloud_platform.db}"

# 账号命名空间统一以该前缀命名
NS_PREFIX="wg_"
# 宿主命名空间里属于本平台的网卡：旧版 veth 的两端，以及新版编排中途的临时接口
LINK_PATTERNS=("veth-h-*" "veth-ns-*" "wgx-*")
# 平台相关 iptables 规则的特征：隧道网段、旧版 veth 网段、旧版 veth 网卡通配
RULE_PATTERN='10\.(200|100)\.|veth\+'

assume_yes=0
dry_run=0
network_only=0

usage() {
    cat <<'EOF'
清理平台在宿主机上创建的网络资源，以及本地数据与配置。

用法:
  sudo ./scripts/cleanup_all.sh                 清理网络 + 数据 + 配置
  sudo ./scripts/cleanup_all.sh --yes           跳过确认（脚本化调用）
  sudo ./scripts/cleanup_all.sh --dry-run       只列出将要执行的操作
  sudo ./scripts/cleanup_all.sh --network-only  只清网络，保留数据与配置
  sudo ./scripts/cleanup_all.sh --help          显示本帮助

环境变量:
  WM_NETWORK_CONFIG_DIR  配置目录（默认 /etc/wg_config）
  WM_DB_PATH             数据库路径（默认 <仓库>/data/cloud_platform.db）
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes)       assume_yes=1 ;;
        -n|--dry-run)   dry_run=1 ;;
        --network-only) network_only=1 ;;
        -h|--help)      usage; exit 0 ;;
        *)              echo "未知参数: $1" >&2; usage; exit 1 ;;
    esac
    shift
done

if [ "$(id -u)" -ne 0 ]; then
    echo "需要 root 权限：要操作宿主网络命名空间与 iptables。" >&2
    exit 1
fi

# dry-run 时只打印、不执行
run_quiet() {
    if [ "${dry_run}" -eq 1 ]; then
        printf '      [dry-run] %s\n' "$*"
        return 0
    fi
    "$@" >/dev/null 2>&1
}

# ── 前置提示 ──
backend_running=0
if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' 2>/dev/null | grep -qx 'cloud-app'; then
    backend_running=1
fi
if pgrep -f 'wm-backend' >/dev/null 2>&1; then
    backend_running=1
fi

if [ "${backend_running}" -eq 1 ]; then
    echo "警告：后端似乎仍在运行。"
    echo "      继续清理会让运行中的实例与实际网络状态不一致，建议先停止服务："
    echo "        docker compose down"
    echo
fi

if [ -n "$(ip netns list 2>/dev/null | awk '{print $1}' | grep "^${NS_PREFIX}" || true)" ] && [ "${dry_run}" -eq 0 ]; then
    echo "本操作会断开全部在线设备。"
fi

if [ "${assume_yes}" -ne 1 ] && [ "${dry_run}" -eq 0 ]; then
    if [ -t 0 ]; then
        printf '按回车继续，Ctrl+C 取消: '
        read -r _ || exit 1
    else
        echo "非交互环境，请显式传入 --yes。" >&2
        exit 1
    fi
fi

echo
echo "===== 开始清理 ====="

# ── 1. 账号网络命名空间 ──
echo "[1/4] 清理账号网络命名空间…"
ns_list="$(ip netns list 2>/dev/null | awk '{print $1}' | grep "^${NS_PREFIX}" || true)"

if [ -z "${ns_list}" ]; then
    echo "  （无）"
else
    for ns in ${ns_list}; do
        echo "  - ${ns}"
        # 删除命名空间会连带销毁其中的隧道接口；接口销毁时内核同步释放宿主
        # 命名空间里的加密 UDP socket，隧道即刻停止服务
        run_quiet ip netns del "${ns}"

        # 命名空间文件被占用或挂载残留时，常规删除会静默失败，这里兜一层
        if [ "${dry_run}" -eq 0 ] && ip netns list 2>/dev/null | awk '{print $1}' | grep -qx "${ns}"; then
            echo "      常规删除未生效，尝试卸载挂载点"
            umount "/var/run/netns/${ns}" 2>/dev/null || true
        fi
    done
fi

# 列出当前命名空间内的网卡名。
# 用 ip 而非 /sys/class/net：后者是 sysfs 的视角，并不随命名空间切换，
# 在容器或子命名空间里会列出宿主全部网卡，与实际可操作的范围不一致。
list_links() {
    ip -br link show 2>/dev/null | awk '{print $1}' | cut -d@ -f1
}

# ── 2. 宿主命名空间里的残留网卡 ──
echo "[2/4] 清理宿主命名空间里的残留网卡…"
link_found=0

for iface in $(list_links); do
    for pattern in "${LINK_PATTERNS[@]}"; do
        case "${iface}" in
            ${pattern})
                echo "  - ${iface}"
                run_quiet ip link del "${iface}"
                link_found=1
                ;;
        esac
    done
done

if [ "${link_found}" -eq 0 ]; then
    echo "  （无）"
fi

# ── 3. 宿主机 iptables 规则 ──
echo "[3/4] 清理宿主机 iptables 规则…"
rule_found=0

# 逐表扫描并删除带平台特征的规则。读取的是 iptables-save 的快照，
# 因此循环中逐条删除不会影响后续遍历。
purge_table() {
    local table="$1" line spec

    while IFS= read -r line; do
        case "${line}" in
            -A\ *) ;;
            *) continue ;;
        esac

        spec="${line#-A }"

        case "${spec}" in
            *10.200.*|*10.100.*|*veth+*) ;;
            *) continue ;;
        esac

        echo "  - [${table}] -A ${spec}"
        rule_found=1

        if [ "${dry_run}" -eq 0 ]; then
            # 旧版为每个账号都追加过同一份规则，循环删除直到该条不再存在
            while iptables -t "${table}" -D ${spec} 2>/dev/null; do :; done
        fi
    done < <(iptables-save -t "${table}" 2>/dev/null || true)
}

purge_table nat
purge_table filter

if [ "${rule_found}" -eq 0 ]; then
    echo "  （无）"
fi

# ── 4. 数据与配置 ──
if [ "${network_only}" -eq 1 ]; then
    echo "[4/4] 保留数据与配置（--network-only）"
else
    echo "[4/4] 清理数据与配置…"

    if [ -d "${CONFIG_DIR}" ]; then
        echo "  - ${CONFIG_DIR}"
        run_quiet rm -rf "${CONFIG_DIR}"
    else
        echo "  - ${CONFIG_DIR}（不存在）"
    fi

    for f in "${DB_PATH}" "${DB_PATH}-wal" "${DB_PATH}-shm" "$(dirname "${DB_PATH}")/jwt.secret"; do
        if [ -e "${f}" ]; then
            echo "  - ${f}"
            run_quiet rm -f "${f}"
        fi
    done
fi

# ── 残留自检 ──
echo
echo "===== 残留检查 ====="
if [ "${dry_run}" -eq 1 ]; then
    echo "（dry-run 未改动任何内容，以下反映当前实际状态）"
fi

remain_ns="$(ip netns list 2>/dev/null | awk '{print $1}' | grep -c "^${NS_PREFIX}" || true)"
remain_link="$(list_links | grep -cE '^(veth-h-|veth-ns-|wgx-)' || true)"
remain_rule="$(iptables-save 2>/dev/null | grep -cE "^-A .*(${RULE_PATTERN})" || true)"

printf '  账号命名空间  : %s\n' "${remain_ns}"
printf '  残留网卡      : %s\n' "${remain_link}"
printf '  特征 iptables : %s\n' "${remain_rule}"

# 旧版还在 INPUT 上留过端口放行规则，它只放行端口、不参与转发，
# 特征不足以同用户自有规则区分，因此这里只报告、不自动删除。
input_rules="$(iptables-save -t filter 2>/dev/null | grep -E '^-A INPUT .*--dport' || true)"
if [ -n "${input_rules}" ]; then
    echo
    echo "以下 INPUT 放行规则疑似旧版遗留。它们不改变转发行为，留着影响很小；"
    echo "若要一并清除，请确认确实属于本平台后手工删除（脚本不替你判断）："
    echo "${input_rules}" | sed 's/^/    /'
fi

echo
if [ "${dry_run}" -eq 1 ]; then
    echo "dry-run 结束，未做任何改动。去掉 --dry-run 即真正执行。"
else
    echo "清理完成。"
    if [ "${network_only}" -eq 0 ]; then
        echo "下次启动会重新创建默认管理员：admin@platform.com / password"
    fi
fi
