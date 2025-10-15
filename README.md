# 云平台 - WireGuard VPN 管理系统

一个基于 Go + Next.js 开发的现代化 WireGuard VPN 管理平台，提供用户友好的 Web 界面和完整的 VPN 管理功能。

## ✨ 功能特性

### 🔐 用户管理
- 用户注册与登录（JWT 认证）
- 基于角色的访问控制（管理员/普通用户）
- 个人资料管理
- 密码加密存储（bcrypt）

### 🌐 WireGuard VPN 管理
- **自动化部署**
  - 自动创建网络命名空间
  - 自动配置 WireGuard 接口
  - 自动设置 iptables 规则和路由
  
- **Peer 设备管理**
  - 添加/编辑/删除 Peer 设备
  - 自动生成密钥对和分配 IP 地址
  - 支持自定义 AllowedIPs 配置
  - 持久化 Keepalive 设置
  - 设备备注和标识
  
- **配置导出**
  - 一键下载配置文件
  - 二维码扫描导入（移动端）
  - 自动生成客户端配置
  
- **网关模式**
  - 支持 Peer 作为网关转发流量
  - 自定义转发接口配置
  - 自动配置 NAT 和转发规则

### 📊 流量监控
- 实时流量统计
- 设备连接状态监控
- 最后握手时间追踪
- 上传/下载流量统计
- 自动刷新数据

### 🎨 现代化 UI
- 响应式设计，支持移动端
- 深色/浅色主题切换
- 中英文国际化支持
- 直观的操作界面
- 实时数据更新

### 👨‍💼 管理员功能
- 查看所有用户流量统计
- 管理用户 WireGuard 服务器
- 删除/启用/禁用服务器
- 设置速率限制
- 系统资源监控

## 🛠 技术栈

### 后端
- **Go 1.21+** - 高性能后端服务
- **Gin** - Web 框架
- **PostgreSQL** - 数据库
- **GORM** - ORM 框架
- **JWT** - 身份认证
- **WireGuard** - VPN 核心
- **Linux Network Namespaces** - 网络隔离

### 前端
- **Next.js 14** - React 框架
- **TypeScript** - 类型安全
- **TailwindCSS** - 样式框架
- **shadcn/ui** - UI 组件库
- **Lucide Icons** - 图标库
- **QRCode** - 二维码生成
- **Zustand** - 状态管理

## 🚀 快速开始

### 前置要求

- **Linux 服务器**（需要 root 权限）
- **Go 1.21+**
- **Node.js 18+** 和 **pnpm**
- **PostgreSQL 14+**
- **WireGuard** 内核模块
- **Docker** 和 **Docker Compose**（可选，用于容器化部署）

### 使用 Docker Compose 部署（推荐）

> **架构说明**：
> - **后端容器**：Go + WireGuard（主机网络模式，特权模式）
> - **前端容器**：Nginx + Next.js（独立容器，可自定义端口）
> - **数据库容器**：PostgreSQL（独立容器）

1. **克隆项目**
```bash
git clone <repository-url>
cd cloud
```

2. **配置环境**

复制并编辑配置文件：
```bash
cp config.yaml.example config.yaml
vim config.yaml
```

关键配置项：
```yaml
server:
  port: 8080

database:
  host: localhost  # 后端使用主机网络，连接 localhost
  port: 5432
  user: root
  password: root123
  name: cloud_platform

jwt:
  secret: "your-super-secret-jwt-key"  # 修改为强随机密钥
  expire_hours: 168

network:
  config_dir: "/etc/wg_config"
  base_subnet: "10.200"
  base_port: 51820
  out_interface: "eth0"  # ⚠️ 修改为你的服务器外网接口名称
  server_ip: "YOUR_SERVER_PUBLIC_IP"  # ⚠️ 必填：服务器公网IP

default:
  username: admin@platform.com
  password: admin123
```

3. **自定义前端端口**（可选）

如果 80 端口被占用，修改 `docker-compose.yml`：
```yaml
frontend:
  ports:
    - "8000:8000"  # 改为其他端口，如 3000:8000
```

4. **启动服务**
```bash
docker-compose up -d
```

5. **访问应用**

默认访问地址：`http://your-server-ip:8000`

- 前端页面：`http://your-server-ip:8000/`
- API 接口：`http://your-server-ip:8000/api/`

> **架构优势**：
> - 前端容器独立，可自定义端口（避免占用 80）
> - 后端使用主机网络，支持 WireGuard 操作
> - Nginx 在前端容器内，统一处理前端和 API 请求
> - 前端使用相对路径调用 API，无需额外配置

### 手动部署

#### 后端部署

1. **安装依赖**
```bash
cd cloud
go mod tidy
```

2. **配置数据库**

创建 PostgreSQL 数据库：
```sql
CREATE DATABASE cloud_platform;
```

3. **配置文件**

编辑 `config.yaml`（参考上面的配置示例）

4. **运行后端**
```bash
sudo go run main.go
```

> **注意**：需要 root 权限来创建网络命名空间和配置 WireGuard

#### 前端部署

1. **安装依赖**
```bash
cd frontend
pnpm install
```

2. **配置环境变量**

创建 `.env.local`：
```env
NEXT_PUBLIC_API_URL=http://your-server-ip:8080
```

3. **构建并运行**
```bash
# 开发模式
pnpm dev

# 生产模式
pnpm build
pnpm start
```

### 默认管理员账户

首次启动时会自动创建管理员账户：
- **邮箱**: `admin@platform.com`
- **密码**: `admin123`（请在首次登录后修改）

## API 文档

### 统一响应格式

所有 API 接口都使用统一的响应格式：

#### 成功响应
```json
{
  "success": true,
  "message": "操作成功消息",
  "data": {
    // 具体的响应数据
  },
  "timestamp": 1640995200,
  "request_id": "optional-request-id"
}
```

#### 错误响应
```json
{
  "success": false,
  "message": "Request failed",
  "error": {
    "code": "ERROR_CODE",
    "message": "具体错误消息",
    "details": "可选的详细信息"
  },
  "timestamp": 1640995200,
  "request_id": "optional-request-id"
}
```

#### 分页响应
```json
{
  "success": true,
  "message": "数据获取成功",
  "data": {
    "items": [
      // 数据项数组
    ],
    "pagination": {
      "current_page": 1,
      "per_page": 20,
      "total_pages": 5,
      "total_items": 100,
      "has_next": true,
      "has_prev": false
    }
  },
  "timestamp": 1640995200
}
```

### 错误代码说明

| 错误代码 | 描述 | HTTP 状态码 |
|---------|------|------------|
| `INVALID_REQUEST` | 请求参数无效 | 400 |
| `VALIDATION_FAILED` | 数据验证失败 | 400 |
| `UNAUTHORIZED` | 未认证 | 401 |
| `INVALID_CREDENTIALS` | 凭据无效 | 401 |
| `TOKEN_INVALID` | Token 无效 | 401 |
| `TOKEN_EXPIRED` | Token 已过期 | 401 |
| `FORBIDDEN` | 权限不足 | 403 |
| `INSUFFICIENT_PERMISSION` | 权限不足 | 403 |
| `COMPANY_DISABLED` | 企业已禁用 | 403 |
| `API_ACCESS_DENIED` | API 访问被拒绝 | 403 |
| `NOT_FOUND` | 资源不存在 | 404 |
| `USER_EXISTS` | 用户已存在 | 400 |
| `INVALID_CODE` | 无效的邀请码 | 400 |
| `CODE_EXPIRED` | 邀请码已过期 | 400 |
| `CODE_EXHAUSTED` | 邀请码使用次数已达上限 | 400 |
| `INTERNAL_ERROR` | 服务器内部错误 | 500 |

### 认证接口

#### 用户注册
```http
POST /api/register
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "password123",
  "name": "用户名",
  "code": "企业码（可选）",
  "company_name": "企业名称（创建企业码时必填）"
}
```

**成功响应 (201):**
```json
{
  "success": true,
  "message": "User registered successfully",
  "data": {
    "id": 1,
    "email": "user@example.com",
    "name": "用户名",
    "role": "user",
    "company_id": 1,
    "company": {
      "id": 1,
      "name": "企业名称",
      "status": "active"
    },
    "disabled_api_prefixes": [],
    "created_at": "2024-01-01T00:00:00Z"
  },
  "timestamp": 1640995200
}
```

**错误响应 (400):**
```json
{
  "success": false,
  "message": "Request failed",
  "error": {
    "code": "USER_EXISTS",
    "message": "User with this email already exists"
  },
  "timestamp": 1640995200
}
```

#### 用户登录
```http
POST /api/login
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "password123"
}
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "Login successful",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user": {
      "id": 1,
      "email": "user@example.com",
      "name": "用户名",
      "role": "user",
      "company_id": 1,
      "company": {
        "id": 1,
        "name": "企业名称",
        "status": "active"
      },
      "disabled_api_prefixes": [],
      "created_at": "2024-01-01T00:00:00Z"
    }
  },
  "timestamp": 1640995200
}
```

**错误响应 (401):**
```json
{
  "success": false,
  "message": "Request failed",
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "Invalid email or password"
  },
  "timestamp": 1640995200
}
```

#### 获取当前用户信息
```http
GET /api/me
Authorization: Bearer <token>
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "User information retrieved successfully",
  "data": {
    "id": 1,
    "email": "user@example.com",
    "name": "用户名",
    "role": "user",
    "company_id": 1,
    "company": {
      "id": 1,
      "name": "企业名称",
      "status": "active"
    },
    "disabled_api_prefixes": [],
    "created_at": "2024-01-01T00:00:00Z"
  },
  "timestamp": 1640995200
}
```

#### 更新个人资料
```http
PATCH /api/me
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "新名称",
  "password": "新密码"
}
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "Profile updated successfully",
  "data": {
    "id": 1,
    "email": "user@example.com",
    "name": "新名称",
    "role": "user",
    "company_id": 1,
    "company": {
      "id": 1,
      "name": "企业名称",
      "status": "active"
    },
    "disabled_api_prefixes": [],
    "created_at": "2024-01-01T00:00:00Z"
  },
  "timestamp": 1640995200
}
```

### 企业管理接口（企业管理员）

#### 生成企业邀请码
```http
POST /api/company/codes
Authorization: Bearer <token>
Content-Type: application/json

{
  "code_type": "join",
  "expire_at": "2024-12-31T23:59:59Z",
  "max_uses": 10
}
```

**成功响应 (201):**
```json
{
  "success": true,
  "message": "Company code created successfully",
  "data": {
    "id": 1,
    "code": "ABC12345",
    "company_id": 1,
    "company": {
      "id": 1,
      "name": "企业名称",
      "status": "active"
    },
    "code_type": "join",
    "used": false,
    "expire_at": "2024-12-31T23:59:59Z",
    "max_uses": 10,
    "uses": 0,
    "created_at": "2024-01-01T00:00:00Z",
    "usage_logs": []
  },
  "timestamp": 1640995200
}
```

#### 查看企业邀请码
```http
GET /api/company/codes
Authorization: Bearer <token>
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "Company codes retrieved successfully",
  "data": [
    {
      "id": 1,
      "code": "ABC12345",
      "company_id": 1,
      "company": {
        "id": 1,
        "name": "企业名称",
        "status": "active"
      },
      "code_type": "join",
      "used": false,
      "expire_at": "2024-12-31T23:59:59Z",
      "max_uses": 10,
      "uses": 2,
      "created_at": "2024-01-01T00:00:00Z",
      "usage_logs": [
        {
          "id": 1,
          "code_id": 1,
          "code": "ABC12345",
          "user_id": 2,
          "user": {
            "id": 2,
            "email": "user2@example.com",
            "name": "用户2"
          },
          "used_at": "2024-01-02T10:00:00Z"
        }
      ]
    }
  ],
  "timestamp": 1640995200
}
```

#### 查看企业成员
```http
GET /api/company/users
Authorization: Bearer <token>
```

返回企业成员列表，格式参考统一响应格式。

#### 踢出成员
```http
DELETE /api/company/users/{user_id}
Authorization: Bearer <token>
```

#### 管理成员权限
```http
PATCH /api/company/users/{user_id}/permissions
Authorization: Bearer <token>
Content-Type: application/json

{
  "disabled_api_prefixes": ["/api/wireguard", "/api/mqtt"]
}
```

### WireGuard VPN 接口

#### 获取我的流量统计
```http
GET /api/wireguard/traffic
Authorization: Bearer <token>
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "Traffic stats retrieved successfully",
  "data": {
    "server_info": {
      "namespace": "wg-user-1",
      "wg_interface": "wg0",
      "wg_port": 51820,
      "wg_address": "10.200.1.1/24",
      "wg_public_key": "server_public_key..."
    },
    "peers": [
      {
        "public_key": "peer_public_key...",
        "endpoint": "1.2.3.4:12345",
        "latest_handshake": "2024-01-01T10:00:00Z",
        "transfer_rx": 1048576,
        "transfer_tx": 2097152,
        "comment": "我的笔记本"
      }
    ]
  }
}
```

#### 获取我的 Peers
```http
GET /api/wireguard/peers
Authorization: Bearer <token>
```

#### 添加 Peer
```http
POST /api/wireguard/peers
Authorization: Bearer <token>
Content-Type: application/json

{
  "allowed_ips": "192.168.1.0/24",
  "persistent_keepalive": 25,
  "comment": "我的手机",
  "enable_forwarding": false,
  "forward_interface": ""
}
```

**成功响应 (201):**
```json
{
  "success": true,
  "message": "Peer added successfully",
  "data": {
    "id": 1,
    "public_key": "auto_generated_public_key...",
    "peer_address": "10.200.1.2",
    "allowed_ips": "192.168.1.0/24",
    "persistent_keepalive": 25,
    "comment": "我的手机",
    "enable_forwarding": false
  }
}
```

#### 更新 Peer
```http
PATCH /api/wireguard/peers/{id}
Authorization: Bearer <token>
Content-Type: application/json

{
  "allowed_ips": "192.168.1.0/24,10.0.0.0/24",
  "persistent_keepalive": 30,
  "comment": "更新后的备注",
  "enable_forwarding": true,
  "forward_interface": "eth0"
}
```

#### 删除 Peer
```http
DELETE /api/wireguard/peers/{id}
Authorization: Bearer <token>
```

#### 获取 Peer 配置
```http
GET /api/wireguard/peers/{id}/config
Authorization: Bearer <token>
```

**成功响应 (200):**
```json
{
  "success": true,
  "message": "Config retrieved successfully",
  "data": {
    "config": "[Interface]\nPrivateKey = ...\nAddress = 10.200.1.2/32\n..."
  }
}
```

### 平台管理接口（平台管理员）

#### 查看所有企业
```http
GET /api/admin/companies
Authorization: Bearer <token>
```

#### 修改企业状态
```http
PATCH /api/admin/companies/{company_id}/status
Authorization: Bearer <token>
Content-Type: application/json

{
  "status": "disabled"
}
```

#### 查看所有用户
```http
GET /api/admin/users
Authorization: Bearer <token>
```

#### 删除用户
```http
DELETE /api/admin/users/{user_id}
Authorization: Bearer <token>
```

#### 修改用户信息
```http
PATCH /api/admin/users/{user_id}
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "新名称",
  "email": "new@example.com",
  "password": "新密码",
  "role": "user"
}
```

**注意：** 所有接口都遵循统一响应格式，成功时返回相应的数据，失败时返回对应的错误代码和消息。

## 数据库结构

### users 表
- `id` - 用户ID
- `email` - 邮箱（唯一）
- `password_hash` - 密码哈希
- `name` - 用户名
- `role` - 用户角色
- `company_id` - 所属企业ID
- `disabled_api_prefixes` - 禁用的API前缀（JSON）
- `created_at` - 创建时间
- `updated_at` - 更新时间

### companies 表
- `id` - 企业ID
- `name` - 企业名称
- `status` - 企业状态
- `created_at` - 创建时间
- `updated_at` - 更新时间

### company_codes 表
- `id` - 码ID
- `code` - 邀请码
- `company_id` - 企业ID
- `created_by` - 创建者ID
- `code_type` - 码类型（create/join）
- `used` - 是否已使用
- `expire_at` - 过期时间
- `max_uses` - 最大使用次数
- `uses` - 已使用次数
- `created_at` - 创建时间
- `updated_at` - 更新时间

### code_usage_logs 表
- `id` - 日志ID
- `code_id` - 码ID
- `code` - 邀请码
- `user_id` - 使用者ID
- `used_at` - 使用时间

### wireguard_servers 表
- `id` - 服务器ID
- `user_id` - 用户ID
- `namespace` - 网络命名空间名称
- `wg_interface` - WireGuard 接口名称
- `wg_port` - WireGuard 端口
- `wg_address` - WireGuard 地址
- `wg_public_key` - 服务器公钥
- `wg_private_key` - 服务器私钥
- `enabled` - 是否启用
- `created_at` - 创建时间
- `updated_at` - 更新时间

### wireguard_peers 表
- `id` - Peer ID
- `server_id` - 服务器ID
- `public_key` - Peer 公钥
- `private_key` - Peer 私钥
- `peer_address` - Peer IP 地址
- `allowed_ips` - 允许访问的 IP 段
- `persistent_keepalive` - 保活间隔
- `comment` - 备注
- `enable_forwarding` - 是否启用转发
- `forward_interface` - 转发接口
- `created_at` - 创建时间
- `updated_at` - 更新时间

## 用户角色

- `admin` - 管理员：拥有所有权限，可管理所有用户和系统
- `user` - 普通用户：可管理自己的 WireGuard VPN

## 配置文件

`config.yaml` 配置文件示例：

```yaml
server:
  port: 8080

database:
  host: localhost
  port: 5432
  user: root
  password: root123
  name: cloud_platform

jwt:
  secret: "supersecretkey"
  expire_hours: 24

network:
  config_dir: "/etc/wg_config"        # WireGuard配置文件目录
  base_subnet: "10.200"               # 基础子网
  base_port: 51820                    # WireGuard起始端口
  out_interface: "eth0"               # 外网接口名称
  server_ip: "1.2.3.4"                # 服务器公网IP地址（必填）

default:
  username: admin@platform.com
  password: password
```

### 配置说明

- **server_ip**: 服务器的公网IP地址，用于生成WireGuard客户端配置文件。客户端将通过此IP连接到服务器。
- **config_dir**: WireGuard配置文件存储目录
- **base_subnet**: WireGuard虚拟网络的基础子网
- **base_port**: WireGuard服务的起始端口号
- **out_interface**: 服务器的外网网卡接口名称

## 📁 项目结构

```
cloud/
├── main.go                           # 后端主程序入口
├── config.yaml                       # 配置文件
├── go.mod / go.sum                   # Go 依赖管理
├── Dockerfile                        # Docker 构建文件
├── docker-compose.yml                # Docker Compose 配置
├── README.md                         # 项目文档
│
├── internal/                         # 后端源码
│   ├── config/                       # 配置管理
│   ├── database/                     # 数据库初始化
│   ├── models/                       # 数据模型
│   │   ├── user.go                   # 用户模型
│   │   ├── wireguard.go              # WireGuard 模型
│   │   └── ...
│   ├── auth/                         # JWT 认证
│   ├── middleware/                   # 中间件
│   ├── handlers/                     # API 处理器
│   │   ├── auth.go                   # 认证接口
│   │   ├── wireguard.go              # WireGuard 接口
│   │   └── ...
│   ├── services/                     # 业务逻辑服务
│   │   ├── wireguard.go              # WireGuard 服务
│   │   ├── netns.go                  # 网络命名空间服务
│   │   └── user_network.go           # 用户网络服务
│   ├── response/                     # 统一响应格式
│   └── routes/                       # 路由配置
│
└── frontend/                         # 前端源码
    ├── src/
    │   ├── app/                      # Next.js 应用路由
    │   │   ├── (auth)/               # 认证页面
    │   │   ├── (dashboard)/          # 仪表板页面
    │   │   │   ├── wireguard/        # WireGuard 管理
    │   │   │   ├── admin-wireguard/  # 管理员 WireGuard
    │   │   │   └── monitoring/       # 系统监控
    │   │   └── layout.tsx
    │   ├── components/               # React 组件
    │   │   ├── ui/                   # UI 组件库
    │   │   ├── admin-panel/          # 管理面板组件
    │   │   ├── auth/                 # 认证组件
    │   │   └── providers/            # Context Providers
    │   ├── services/                 # API 服务
    │   ├── stores/                   # Zustand 状态管理
    │   ├── types/                    # TypeScript 类型定义
    │   ├── hooks/                    # 自定义 Hooks
    │   ├── lib/                      # 工具函数
    │   └── locales/                  # 国际化文件
    │       ├── en.json               # 英文
    │       └── zh.json               # 中文
    ├── public/                       # 静态资源
    ├── package.json                  # 前端依赖
    └── next.config.mjs               # Next.js 配置
```

## 🔧 开发说明

### 后端开发

#### 添加新的 API 端点
1. 在 `internal/handlers/` 中创建处理函数
2. 在 `internal/routes/routes.go` 中添加路由
3. 根据需要添加中间件验证

#### 添加新的数据模型
1. 在 `internal/models/` 中创建模型文件
2. 在 `internal/database/database.go` 中添加自动迁移

#### 权限控制
系统使用中间件进行权限控制：
- `RequireAuth()` - 要求用户登录
- `RequireAdmin()` - 要求管理员权限

### 前端开发

#### 添加新页面
1. 在 `src/app/(dashboard)/` 中创建新路由目录
2. 创建 `page.tsx` 文件
3. 使用 `ContentLayout` 包装页面内容

#### 添加新的 API 服务
1. 在 `src/services/` 中创建服务文件
2. 使用统一的 `api` 实例发送请求
3. 定义相应的 TypeScript 类型

#### 国际化
1. 在 `src/locales/zh.json` 和 `en.json` 中添加翻译
2. 使用 `useI18n()` hook 获取翻译函数
3. 使用 `t('key.path')` 获取翻译文本

## 🐛 故障排查

### WireGuard 无法启动
- 检查 Linux 内核是否加载了 WireGuard 模块：`lsmod | grep wireguard`
- 确保有 root 权限
- 检查 `config.yaml` 中的 `server_ip` 配置

### 前端无法连接后端
- 检查 `.env.local` 中的 `NEXT_PUBLIC_API_URL`
- 确保后端服务正在运行
- 检查防火墙设置

### 数据库连接失败
- 检查 PostgreSQL 是否运行
- 验证 `config.yaml` 中的数据库配置
- 确保数据库已创建

## 📝 许可证

MIT License

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📧 联系方式

如有问题，请提交 Issue 或联系项目维护者。