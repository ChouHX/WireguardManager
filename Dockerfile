# 单容器镜像：后端直接托管前端静态产物 + WireGuard 工具链。
#
# 前端不再单独用 Nginx 托管，而是由后端进程一并提供，
# 于是整个系统只有一个进程、一个端口、一个容器——不需要进程管理器，
# 停止信号由 Go 直接处理，关闭过程天然优雅。
#
#   docker build -t wireguardmanager:latest .
#
# 构建上下文为仓库根目录。

# ---------- 阶段 1：构建前端静态产物 ----------
FROM node:22-alpine AS web-builder

ARG NPM_REGISTRY=https://registry.npmmirror.com

WORKDIR /app

COPY frontend/package.json frontend/package-lock.json ./
RUN npm config set registry ${NPM_REGISTRY} \
    && npm ci --no-audit --no-fund

COPY frontend/ .
RUN npm run build

# ---------- 阶段 2：编译后端 ----------
FROM golang:1.23-alpine AS api-builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache git

ENV GOPROXY=https://goproxy.cn,direct \
    GO111MODULE=on \
    CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal ./internal

# 依赖为纯 Go 实现（glebarez/sqlite），无需 CGO
RUN go build -a -installsuffix cgo -o /out/main .

# ---------- 阶段 3：运行时 ----------
FROM alpine:3.20

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk --no-cache add ca-certificates wireguard-tools iptables iproute2 tzdata

COPY --from=api-builder /out/main /usr/local/bin/wm-backend
COPY --from=web-builder /app/dist /usr/share/wireguard-manager/webui

RUN mkdir -p /etc/wg_config /root/data

# 由后端统一提供控制台与 API，因此只需一个端口
ENV WEB_ROOT=/usr/share/wireguard-manager/webui \
    WM_SERVER_PORT=3000 \
    TZ=Asia/Shanghai

WORKDIR /root

EXPOSE 3000

# 探针指向 /ready 而非 /health：
#   /health 是无条件 200 的存活探针，数据库不可用时它也照样返回 200，
#   容器会一直显示 healthy 而实际已经不可服务；
#   /ready 会真正 Ping 数据库并检查采集器状态，能反映「进程活着但不能干活」。
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${WM_SERVER_PORT}/ready" >/dev/null 2>&1 || exit 1

# 后端即 PID 1：Go 直接处理 SIGTERM 做优雅关闭
ENTRYPOINT ["/usr/local/bin/wm-backend"]
