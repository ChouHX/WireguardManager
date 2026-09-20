# ---------- 构建阶段：Vite 静态构建 ----------
FROM node:22-alpine AS builder

# 国内构建可保持镜像源；海外环境可传 --build-arg NPM_REGISTRY=https://registry.npmjs.org
ARG NPM_REGISTRY=https://registry.npmmirror.com

WORKDIR /app

# 先装依赖，最大化利用 Docker 层缓存
COPY package.json package-lock.json ./
RUN npm config set registry ${NPM_REGISTRY} \
    && npm ci --no-audit --no-fund

COPY . .
RUN npm run build

# ---------- 运行阶段：Nginx 托管静态产物 + 反代后端 ----------
FROM nginx:1.27-alpine

# 使用模板机制注入后端地址（官方镜像启动时自动 envsubst）
RUN rm -f /etc/nginx/conf.d/default.conf
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template

COPY --from=builder /app/dist /usr/share/nginx/html

# 后端地址，由 docker-compose 覆盖
ENV BACKEND_UPSTREAM=http://host.docker.internal:8080

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1/ >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
