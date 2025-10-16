# Frontend Dockerfile with Nginx
# Stage 1: Build the Next.js application
FROM node:20-alpine AS builder

# 使用阿里云镜像源加速 Alpine 包下载
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 配置 npm 使用淘宝镜像源
RUN npm config set registry https://registry.npmmirror.com

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Build the Next.js application for production
# Set output to standalone for optimized Docker images
RUN npm run build

# Stage 2: Production image with Nginx
FROM nginx:alpine

# 使用阿里云镜像源加速 Alpine 包下载
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# Install Node.js for Next.js standalone server and netcat for health check
RUN apk add --no-cache nodejs netcat-openbsd

# Remove default nginx config
RUN rm /etc/nginx/conf.d/default.conf

# Copy custom nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built Next.js app from builder
WORKDIR /app
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

# Copy startup script
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80

# Start both Next.js server and Nginx
ENTRYPOINT ["/docker-entrypoint.sh"]
