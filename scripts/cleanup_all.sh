#!/bin/bash

# 清理所有用户配置脚本
# 用途：完全重置系统，删除所有用户和配置

set -e

CONFIG_DIR="/etc/wg_config"
DB_NAME="cloud_platform"
DB_USER="cloud_user"

echo "===== 警告：即将删除所有用户配置 ====="
echo "按 Ctrl+C 取消，或按回车继续..."
read

echo ""
echo "===== 开始清理所有配置 ====="

# 1. 列出并删除所有网络命名空间（以 wg_ 开头的）
echo "[1/5] 清理所有 WireGuard 命名空间..."
for ns in $(ip netns list | grep "^wg_" | awk '{print $1}'); do
    echo "  - 处理命名空间: ${ns}"
    
    # 尝试停止 WireGuard
    ip netns exec ${ns} ip link show wg0 &>/dev/null && {
        echo "    停止 WireGuard..."
        ip netns exec ${ns} ip link del wg0 2>/dev/null || true
    }
    
    # 删除命名空间
    echo "    删除命名空间..."
    ip netns del ${ns}
done

# 2. 清理所有 veth 设备
echo "[2/5] 清理所有 veth 设备..."
for veth in $(ip link show | grep "veth-h-" | awk -F: '{print $2}' | tr -d ' '); do
    echo "  - 删除: ${veth}"
    ip link del ${veth} 2>/dev/null || true
done

# 3. 清理所有 iptables NAT 规则
echo "[3/5] 清理 iptables NAT 规则..."
# 清理与 10.200 和 10.100 相关的 MASQUERADE 规则
iptables-save | grep -E "10\.(200|100)" | grep "MASQUERADE" | while read rule; do
    if [[ $rule == -A* ]]; then
        rule_spec=$(echo "$rule" | sed 's/^-A //')
        echo "  - 删除规则: $rule_spec"
        iptables -t nat -D $rule_spec 2>/dev/null || true
    fi
done

# 清理 FORWARD 规则
iptables-save | grep -E "10\.(200|100)" | grep "FORWARD" | while read rule; do
    if [[ $rule == -A* ]]; then
        rule_spec=$(echo "$rule" | sed 's/^-A //')
        echo "  - 删除规则: $rule_spec"
        iptables -D $rule_spec 2>/dev/null || true
    fi
done

# 4. 删除所有配置文件
echo "[4/5] 删除配置文件目录..."
if [ -d "${CONFIG_DIR}" ]; then
    echo "  - 删除: ${CONFIG_DIR}"
    rm -rf ${CONFIG_DIR}
else
    echo "  - 配置目录不存在"
fi

# 5. 清空数据库表
echo "[5/5] 清空数据库..."
echo "  - 删除所有数据..."
docker compose -f docker-compose-db.yml down
rm -rf ./postgres_data
echo " - 删除完毕，重启数据库..."
docker compose -f docker-compose-db.yml up -d

echo ""
echo "===== 清理完成 ====="
echo ""
echo "系统已完全重置。现在可以："
echo "  1. 重新运行: sudo go run main.go"
echo "  2. 系统会自动创建默认管理员"
echo "  3. 管理员邮箱: admin@platform.com"
echo "  4. 默认密码: password"
echo ""
