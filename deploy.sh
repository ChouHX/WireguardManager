#!/bin/bash

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  WireGuard Manager Docker 部署脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then 
    echo -e "${YELLOW}提示: 某些操作可能需要 sudo 权限${NC}"
fi

# 检查 Docker 是否安装
echo -e "${YELLOW}[1/7] 检查 Docker...${NC}"
if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: Docker 未安装${NC}"
    echo "请先安装 Docker: https://docs.docker.com/get-docker/"
    exit 1
fi
echo -e "${GREEN}✓ Docker 已安装${NC}"

# 检查 Docker Compose 是否安装
echo -e "${YELLOW}[2/7] 检查 Docker Compose...${NC}"
if ! docker compose version &> /dev/null; then
    echo -e "${RED}错误: Docker Compose 未安装${NC}"
    echo "请先安装 Docker Compose"
    exit 1
fi
echo -e "${GREEN}✓ Docker Compose 已安装${NC}"

# 配置 Docker 镜像加速器（国内用户）
echo -e "${YELLOW}[3/7] 配置 Docker 镜像加速器...${NC}"
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
echo -e "${YELLOW}[4/7] 检查 WireGuard 内核模块...${NC}"
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

# 准备配置文件
echo -e "${YELLOW}[5/7] 准备配置文件...${NC}"

if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo -e "${GREEN}✓ 已创建 .env 文件${NC}"
    else
        echo -e "${RED}错误: 未找到 .env.example${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ .env 文件已存在${NC}"
fi

if [ ! -f "config.yaml" ]; then
    if [ -f "config.yaml.example" ]; then
        cp config.yaml.example config.yaml
        echo -e "${YELLOW}✓ 已创建 config.yaml 文件${NC}"
        echo -e "${RED}重要: 请编辑 config.yaml 文件，修改以下配置:${NC}"
        echo "  - network.server_ip: 修改为服务器公网 IP"
        echo "  - network.out_interface: 修改为网络接口名称（如 eth0）"
        echo ""
        read -p "是否现在编辑配置文件? [Y/n] " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
            ${EDITOR:-nano} config.yaml
        fi
    else
        echo -e "${RED}错误: 未找到 config.yaml.example${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ config.yaml 文件已存在${NC}"
fi

# 创建必要的目录
echo -e "${YELLOW}[6/7] 创建必要的目录...${NC}"
sudo mkdir -p /etc/wg_config
sudo chmod 755 /etc/wg_config
echo -e "${GREEN}✓ 已创建 /etc/wg_config 目录${NC}"

# 创建网络命名空间目录（关键！）
sudo mkdir -p /var/run/netns
sudo chmod 755 /var/run/netns
echo -e "${GREEN}✓ 已创建 /var/run/netns 目录${NC}"

# 构建并启动服务
echo -e "${YELLOW}[7/7] 构建并启动服务...${NC}"
echo "这可能需要几分钟时间，请耐心等待..."
echo ""

if docker compose build; then
    echo -e "${GREEN}✓ 镜像构建成功${NC}"
    echo ""
    
    if docker compose up -d; then
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
else
    echo -e "${RED}错误: 镜像构建失败${NC}"
    echo "请检查网络连接和 Docker 配置"
    exit 1
fi
