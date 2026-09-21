#!/bin/bash

# WireGuard Manager 部署脚本
#
#   ./deploy.sh           从 GHCR 拉取预构建镜像并启动（默认，无需本地编译）
#   ./deploy.sh --build   使用 docker-compose.build.yml 从源码构建后启动
#
# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 解析部署模式
BUILD_LOCAL=false
for arg in "$@"; do
    case "$arg" in
        --build|-b) BUILD_LOCAL=true ;;
        --help|-h)
            echo "用法: ./deploy.sh [--build]"
            echo "  默认     从 GHCR 拉取预构建镜像并启动"
            echo "  --build  本地从源码构建镜像后启动"
            exit 0
            ;;
        *)
            echo "未知参数: $arg（可用: --build）"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  WireGuard Manager Docker 部署脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then 
    echo -e "${YELLOW}提示: 某些操作可能需要 sudo 权限${NC}"
fi

# 检查 Docker 是否安装
echo -e "${YELLOW}[1/8] 检查 Docker...${NC}"
if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: Docker 未安装${NC}"
    echo "请先安装 Docker: https://docs.docker.com/get-docker/"
    exit 1
fi
echo -e "${GREEN}✓ Docker 已安装${NC}"

# 检查 Docker Compose 是否安装
echo -e "${YELLOW}[2/8] 检查 Docker Compose...${NC}"
if ! docker compose version &> /dev/null; then
    echo -e "${RED}错误: Docker Compose 未安装${NC}"
    echo "请先安装 Docker Compose"
    exit 1
fi
echo -e "${GREEN}✓ Docker Compose 已安装${NC}"

# 配置 Docker 镜像加速器（国内用户）
echo -e "${YELLOW}[3/8] 配置 Docker 镜像加速器...${NC}"
if [ -f "daemon.json" ]; then
    read -p "是否配置 Docker 镜像加速器（国内推荐）? [Y/n] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
        sudo cp daemon.json /etc/docker/daemon.json
        sudo systemctl daemon-reload
        sudo systemctl restart docker
        echo -e "${GREEN}✓ Docker 镜像加速器已配置${NC}"
    else
        echo -e "${YELLOW}跳过镜像加速器配置${NC}"
    fi
else
    echo -e "${YELLOW}未找到 daemon.json，跳过${NC}"
fi

# 检查 WireGuard 内核模块
echo -e "${YELLOW}[4/8] 检查 WireGuard 内核模块...${NC}"
if ! lsmod | grep -q wireguard; then
    echo -e "${YELLOW}WireGuard 模块未加载，尝试加载...${NC}"
    if sudo modprobe wireguard 2>/dev/null; then
        echo -e "${GREEN}✓ WireGuard 模块已加载${NC}"
    else
        echo -e "${RED}警告: 无法加载 WireGuard 模块${NC}"
        echo "请确保系统支持 WireGuard 或已安装 wireguard-dkms"
    fi
else
    echo -e "${GREEN}✓ WireGuard 模块已加载${NC}"
fi
# 说明：宿主无需安装 wireguard-tools，wg / ip / iptables 都在后端容器内，
# 容器以特权 + host 网络运行并共享 /var/run/netns，因此能直接管理宿主的网络环境。
echo -e "  ${GREEN}提示:${NC} 宿主机无需安装 wg 命令，工具链由后端容器提供（第 8 步会自检）"

# 准备环境变量文件
echo -e "${YELLOW}[5/8] 准备环境变量文件...${NC}"

if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo -e "${GREEN}✓ 已从 .env.example 创建 .env${NC}"
    else
        echo -e "${RED}错误: 未找到 .env.example${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ .env 文件已存在${NC}"
fi

# JWT 密钥：为空时自动生成，避免部署后因缺少密钥而无法启动
if grep -qE '^WM_JWT_SECRET=.+' .env; then
    echo -e "${GREEN}✓ WM_JWT_SECRET 已设置${NC}"
else
    secret=$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
    if sed -i "s|^WM_JWT_SECRET=.*|WM_JWT_SECRET=${secret}|" .env; then
        echo -e "${GREEN}✓ 已自动生成 WM_JWT_SECRET${NC}"
    else
        echo -e "${YELLOW}提示: 请手动在 .env 中设置 WM_JWT_SECRET${NC}"
    fi
fi

echo -e "  ${GREEN}提示:${NC} 网络、监控、在线判定等参数已移至管理界面「系统设置」，无需在此配置"

# 创建必要的目录
echo -e "${YELLOW}[6/8] 创建必要的目录...${NC}"
sudo mkdir -p /etc/wg_config
sudo chmod 755 /etc/wg_config
echo -e "${GREEN}✓ 已创建 /etc/wg_config 目录${NC}"

# 创建网络命名空间目录（关键！）
sudo mkdir -p /var/run/netns
sudo chmod 755 /var/run/netns
echo -e "${GREEN}✓ 已创建 /var/run/netns 目录${NC}"

# 创建 SQLite 数据目录（数据库为嵌入式 SQLite，无需独立数据库容器）
mkdir -p data
echo -e "${GREEN}✓ 已创建 data 目录（存放 SQLite 数据库文件）${NC}"

# 获取并启动服务
if [ "$BUILD_LOCAL" = true ]; then
    echo -e "${YELLOW}[7/8] 本地构建镜像并启动服务...${NC}"
    echo "使用 docker-compose.build.yml 从源码编译，可能需要几分钟..."
    echo ""

    UP_CMD="docker compose -f docker-compose.build.yml up -d --build"
else
    echo -e "${YELLOW}[7/8] 拉取 GHCR 镜像并启动服务...${NC}"
    echo "使用默认 docker-compose.yml（预构建镜像）..."
    echo ""

    if ! docker compose pull; then
        echo -e "${RED}错误: 镜像拉取失败${NC}"
        echo "请检查网络连接；如需本地编译请执行: ./deploy.sh --build"
        exit 1
    fi
    echo -e "${GREEN}✓ 镜像拉取完成${NC}"
    echo ""

    UP_CMD="docker compose up -d"
fi

if $UP_CMD; then
    echo -e "${GREEN}✓ 服务启动成功${NC}"
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  部署完成！${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "访问地址: http://$(hostname -I | awk '{print $1}'):3000/"
    echo "默认账号: admin@platform.com"
    echo "默认密码: password"
    echo ""
    echo "查看日志: docker compose logs -f"
    echo "停止服务: docker compose down"
    echo ""
else
    echo -e "${RED}错误: 服务启动失败${NC}"
    echo "查看日志: docker compose logs"
    exit 1
fi

# ---------- 部署自检 ----------
echo -e "${YELLOW}[8/8] 运行部署自检...${NC}"

SELF_CHECK_FAILED=0
if [ "$BUILD_LOCAL" = true ]; then
    COMPOSE_FILE="docker-compose.build.yml"
else
    COMPOSE_FILE="docker-compose.yml"
fi

# 优先 curl，其次 wget；返回 2 表示宿主机两者都没有
http_ok() {
    local url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsS --max-time 5 "$url" >/dev/null 2>&1
    elif command -v wget >/dev/null 2>&1; then
        wget -q -T 5 -O /dev/null "$url" >/dev/null 2>&1
    else
        return 2
    fi
}

# 1) 容器内工具链：宿主无需安装 wireguard-tools
if docker compose -f "$COMPOSE_FILE" exec -T app sh -c \
    'command -v wg >/dev/null 2>&1 && command -v ip >/dev/null 2>&1 && command -v iptables >/dev/null 2>&1' 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} 容器内 wg / ip / iptables 可用（宿主无需安装 wireguard-tools）"
else
    echo -e "  ${RED}✗${NC} 容器内缺少 WireGuard 工具链，创建网络与设备会失败"
    SELF_CHECK_FAILED=1
fi

# 2) 命名空间共享：容器里应能直接列出宿主的 netns
if docker compose -f "$COMPOSE_FILE" exec -T app ip netns list >/dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} 容器可访问宿主网络命名空间（/var/run/netns 已共享）"
else
    echo -e "  ${RED}✗${NC} 容器无法访问 /var/run/netns，请确认该挂载为 shared"
    SELF_CHECK_FAILED=1
fi

# 3) 控制台与 API 可访问（单容器下由同一个进程、同一个端口提供）
CONSOLE_PORT=$(docker compose -f "$COMPOSE_FILE" exec -T app printenv WM_SERVER_PORT 2>/dev/null | tr -d '\r\n')
CONSOLE_PORT=${CONSOLE_PORT:-3000}

http_ok "http://127.0.0.1:${CONSOLE_PORT}/health"
health_status=$?
case $health_status in
    0) echo -e "  ${GREEN}✓${NC} 控制台与 API 可访问（127.0.0.1:${CONSOLE_PORT}/health）" ;;
    2) echo -e "  ${YELLOW}!${NC} 宿主机缺少 curl/wget，跳过 HTTP 检查" ;;
    *)
        echo -e "  ${RED}✗${NC} 服务不可访问（127.0.0.1:${CONSOLE_PORT}）"
        echo "     端口取自容器的 WM_SERVER_PORT，可用 ss -ltn 复核"
        echo "     查看日志: docker compose -f ${COMPOSE_FILE} logs --tail=50 app"
        SELF_CHECK_FAILED=1
        ;;
esac

echo ""
if [ "$SELF_CHECK_FAILED" -eq 0 ]; then
    echo -e "${GREEN}✓ 自检通过，服务已就绪。${NC}"
else
    echo -e "${RED}✗ 自检发现问题，请按上述提示排查。${NC}"
fi
echo ""
