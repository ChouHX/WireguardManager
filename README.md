# WireGuard Manager

> 多用户 WireGuard 管理平台：账号级网络命名空间隔离，设备配置一键下发，流量与在线状态实时可见。

[![Build and Push Images](https://github.com/ChouHX/WireguardManager/actions/workflows/build-images.yml/badge.svg)](https://github.com/ChouHX/WireguardManager/actions/workflows/build-images.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![Mantine](https://img.shields.io/badge/Mantine-8-339AF0)](https://mantine.dev)
[![GHCR](https://img.shields.io/badge/ghcr.io-ready-2496ED?logo=docker&logoColor=white)](https://github.com/ChouHX/WireguardManager/pkgs/container/wireguardmanager-backend)

## 目录

- [特性](#特性)
- [架构](#架构)
  - [网络资源模型](#网络资源模型)
- [快速开始](#快速开始)
- [配置参考](#配置参考)
  - [配置项](#配置项)
  - [环境变量](#环境变量)
  - [客户端配置](#客户端配置)
- [API](#api)
- [目录结构](#目录结构)
- [本地开发](#本地开发)
- [常用命令](#常用命令)
- [常见问题](#常见问题)
- [安全提示](#安全提示)
- [许可证](#许可证)

## 特性

- **账号级隔离** —— 每个账号独享一个 network namespace、独立的 WireGuard 接口与监听端口，互不可见、互不干扰。
- **自动化编排** —— 注册即自动创建命名空间、veth 对、wg 接口、路由与 iptables 规则；任一步失败自动回滚，不留半成品。
- **设备即开即用** —— 一键生成客户端配置，支持 `.conf` 下载与二维码扫码导入，转发出口自动探测。
- **秒级在线感知** —— 服务端对设备做 TCP 探测，客户端零 Agent：收到对端内核响应即刻上线（毫秒级），连续超时才判离线，规避单次丢包误报。
- **流量与资源监控** —— 设备握手状态、收发流量、系统 CPU / 内存 / 磁盘 / 网络趋势一屏掌握。
- **精细管控** —— 设备粒度限速、启用禁用、网关转发模式、AllowedIPs 网段自定义。
- **现代控制台** —— React 19 + Mantine，紧凑式布局、明暗主题、中英文双语、移动端自适应。
- **轻量部署** —— 嵌入式 SQLite（纯 Go 驱动，无需 CGO、无需独立数据库服务），镜像由 CI 构建并推送 GHCR。

## 架构

```mermaid
flowchart LR
  subgraph clients["客户端设备"]
    D1["设备 A<br/>10.200.1.2"]
    D2["设备 B<br/>10.200.2.2"]
  end

  subgraph host["宿主机"]
    direction TB
    subgraph ns1["netns · wg_a1b2c3d4"]
      W1["wg0 · UDP 51821<br/>10.200.1.1/24"]
    end
    subgraph ns2["netns · wg_e5f6a7b8"]
      W2["wg0 · UDP 51822<br/>10.200.2.1/24"]
    end
    API["Gin API :8080<br/>嵌入式 SQLite"]
  end

  WEB["控制台 · Nginx :3000"]
  INET((互联网))

  D1 -- "WireGuard UDP" --> W1
  D2 -- "WireGuard UDP" --> W2
  W1 -- "veth + NAT" --> INET
  W2 -- "veth + NAT" --> INET
  WEB -- "/api 反向代理" --> API
  API -. "netns / wg 编排与统计" .-> ns1
  API -. "netns / wg 编排与统计" .-> ns2
```

- **后端**（Go + Gin + GORM）：以 host 网络运行，负责 WireGuard 操作、命名空间编排与数据持久化。
- **前端**（React + Vite + Mantine）：构建为静态产物，由 Nginx 托管并反向代理 `/api`。
- **数据库**（嵌入式 SQLite）：单文件持久化，随镜像一起部署，无需外部服务。
- **共享命名空间**（`/var/run/netns`）：宿主可直接管理容器创建的 netns。

### 网络资源模型

每个账号注册后自动获得以下资源，均由平台负责创建与回收：

| 资源 | 说明 |
| --- | --- |
| network namespace | `wg_<user_uid>`，账号独占，与其他账号完全隔离 |
| WireGuard 接口 | 命名空间内 `wg0`，监听端口从 `network.base_port` 起递增分配 |
| 隧道网段 | `10.<base_subnet>.<序号>.0/24`，服务端占 `.1`，设备从 `.2` 起顺序分配 |
| veth 对 | 宿主侧 ↔ 命名空间侧，承载隧道出入流量 |
| NAT / 转发规则 | 命名空间内 iptables `MASQUERADE` 与 `FORWARD` 规则 |
| 配置文件 | `/etc/wg_config/<user_uid>/wg0.conf` |

## 快速开始

前置条件：Linux 主机、root 或 sudo 权限、Docker 24+ 与 Docker Compose v2、`wireguard` 内核模块。

```bash
git clone https://github.com/ChouHX/WireguardManager.git
cd WireguardManager

cp .env.example .env
cp config.yaml.example config.yaml
# 至少需要修改 config.yaml 中的：
#   jwt.secret        换成随机长字符串
#   network.server_ip 服务器公网 IP
#   network.out_interface 网卡名（可用 ip route show default 查看）

./deploy.sh          # 拉取 GHCR 预构建镜像并启动（默认）
```

需要在本机从源码构建（改过代码，或访问不了 GHCR）：

```bash
./deploy.sh --build
# 等价于
docker compose -f docker-compose.build.yml up -d --build
```

访问 `http://<SERVER_IP>:3000`，默认账号 `admin@platform.com` / `password`（**首次登录后请立即修改**）。

镜像标签可用 `WM_IMAGE_TAG` 指定，默认 `latest`，也可用 `sha-<短哈希>` 回滚到某次构建：

```bash
WM_IMAGE_TAG=sha-2df1b06 docker compose up -d
```

## 配置参考

### 配置项

`config.yaml` 按段组织，完整示例见 [`config.yaml.example`](config.yaml.example)：

| 段 | 键 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `server` | `port` | `8080` | 后端监听端口 |
| | `mode` | `release` | Gin 运行模式：`debug` / `release` / `test` |
| | `cors_origins` | `["*"]` | 允许的跨域来源 |
| `database` | `path` | `./data/cloud_platform.db` | SQLite 文件路径 |
| | `max_open_conns` | `1` | 连接数，1 表示串行访问，规避 `database is locked` |
| | `busy_timeout_ms` | `5000` | 写锁等待超时 |
| | `wal` | `true` | 是否启用 WAL 日志模式 |
| `jwt` | `secret` | 无 | **必填**，签名密钥，少于 8 字符会拒绝启动 |
| | `expire_hours` | `24` | Token 有效期 |
| `network` | `config_dir` | `/etc/wg_config` | WireGuard 配置存放目录 |
| | `base_subnet` | `10.200` | 隧道网段的基础前缀 |
| | `base_port` | `51820` | 端口起始值 |
| | `out_interface` | `eth0` | 宿主出口网卡（探测失败时的回退值） |
| | `server_ip` | 无 | 服务器公网 IP，下发到客户端配置的 Endpoint |
| | `dns` | `1.1.1.1, 8.8.8.8` | 下发给客户端的 DNS |
| | `client_allowed_ips` | 空 | 客户端 AllowedIPs，留空按设备所在网段推导；全局代理填 `0.0.0.0/0, ::/0` |
| `monitoring` | `interval_seconds` | `10` | 系统指标采样间隔 |
| | `retention_hours` | `168` | 监控记录保留时长（7 天） |
| | `cleanup_interval_hours` | `24` | 过期记录清理周期 |
| `liveness` | `enabled` | `true` | 是否启用设备在线探测 |
| | `interval_seconds` | `1` | 探测间隔 |
| | `timeout_ms` | `800` | 单次探测超时 |
| | `offline_threshold` | `2` | 连续失败多少次判离线 |
| | `probe_port` | `49151` | 探测端口，避开 22/80/443 等常用端口 |
| | `max_concurrency` | `32` | 并发探测上限 |
| `default` | `admin_email` | `admin@platform.com` | 首次启动创建的管理员邮箱 |
| | `admin_password` | `password` | 初始密码 |

### 环境变量

所有配置项都可用 `WM_*` 环境变量覆盖，优先级高于 YAML，便于容器化与密钥注入。常用项：

```bash
WM_JWT_SECRET=...                    # 签名密钥
WM_SERVER_PORT=8080                  # 监听端口
WM_NETWORK_SERVER_IP=1.2.3.4         # 公网 IP
WM_NETWORK_OUT_INTERFACE=eth0        # 出口网卡
WM_DB_PATH=/root/data/cloud_platform.db
WM_LIVENESS_ENABLED=false            # 关闭在线探测
```

完整清单见 [`.env.example`](.env.example)。

### 客户端配置

下载的 `AllowedIPs` 默认按设备所在网段下发（服务端接口 `10.100.0.1/24`、设备分配到 `10.100.0.2` 时下发 `10.100.0.0/24`），只把 VPN 网段流量送进隧道。需要全局代理时：

```yaml
network:
  client_allowed_ips: "0.0.0.0/0, ::/0"
```

开启「网关转发」后，该设备可作为网关，让其他设备经它访问 AllowedIPs 中配置的网段；转发出口接口会自动探测默认路由，也可从下拉列表中改选。配置值支持逗号分隔的多个网段，启动时会校验格式。

## API

所有业务接口挂载在 `/api` 下（`/health`、`/ready` 除外），使用 `Authorization: Bearer <token>` 鉴权。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/register` | 注册账号（自动创建网络环境，耗时数秒） |
| `POST` | `/api/login` | 登录并获取 Token |
| `GET` `PATCH` | `/api/me` | 查询 / 更新当前账号信息 |
| `GET` | `/api/wireguard/traffic` | 当前账号流量摘要（轮询接口，服务端带缓存） |
| `GET` `POST` | `/api/wireguard/peers` | 列出 / 新增设备 |
| `PATCH` `DELETE` | `/api/wireguard/peers/{id}` | 更新 / 删除设备 |
| `GET` | `/api/wireguard/peers/{id}/config` | 获取设备客户端配置 |
| `GET` | `/api/wireguard/interfaces` | 可用网络接口列表（含探测到的默认出口） |
| `GET` | `/api/wireguard/liveness` | 当前账号设备在线状态 |
| `GET` | `/api/admin/users` | 用户列表 |
| `PATCH` `DELETE` | `/api/admin/users/{id}` | 更新 / 删除用户 |
| `GET` | `/api/admin/wireguard/traffic` | 全部账号流量汇总 |
| `GET` | `/api/admin/wireguard/traffic/{id}` | 单个账号流量详情 |
| `GET` | `/api/admin/wireguard/liveness` | 各账号在线设备统计 |
| `PATCH` | `/api/admin/wireguard/servers/{id}/toggle` | 启用 / 禁用账号网络 |
| `PATCH` | `/api/admin/wireguard/servers/{id}/ratelimit` | 设置限速 |
| `DELETE` | `/api/admin/wireguard/servers/{id}` | 删除账号网络环境 |
| `GET` | `/api/admin/monitoring/*` | 系统监控（`system` `cpu` `memory` `disk` `network` `chart` `history` `stats`） |
| `GET` | `/health` `/ready` | 存活 / 就绪探针 |

## 目录结构

```
.
├── main.go                     # 入口：启动、路由装配、优雅关闭
├── config.yaml.example         # 配置样例
├── internal/
│   ├── config/                 # 配置加载、校验、环境变量覆盖
│   ├── database/               # SQLite 初始化与默认管理员
│   ├── handlers/               # HTTP 处理层
│   ├── middleware/             # 鉴权与用户缓存
│   ├── models/                 # 数据模型
│   ├── routes/                 # 路由注册
│   └── services/               # netns / WireGuard / 监控采样 / 存活探测
├── frontend/                   # React + Vite + MantineUI 源码
├── docker-compose.yml          # 默认：使用 GHCR 预构建镜像
├── docker-compose.build.yml    # 本地构建编排
├── wg_config/                  # 挂载至 /etc/wg_config
└── data/                       # SQLite 数据目录（容器内 /root/data）
```

## 本地开发

```bash
# 后端（涉及 netns / iptables，需要 root）
go mod tidy
sudo go run main.go

# 前端
cd frontend
npm install
npm run dev        # http://localhost:3000，自动代理 /api 到 localhost:8080

# 前端校验
npm run typecheck       # tsc --noEmit
npm run check:locales   # 校验中英文文案键一致

# 重置环境（删除全部 netns、WireGuard 配置与 SQLite 数据）
sudo ./scripts/cleanup_all.sh
```

后端测试：

```bash
go test ./internal/...
```

## 常用命令

```bash
docker compose ps                     # 查看服务状态
docker compose logs -f backend        # 跟踪后端日志
docker compose logs -f frontend       # 跟踪前端日志
docker compose pull && docker compose up -d   # 更新到最新镜像
docker compose down                   # 停止服务
```

## 常见问题

**注册 / 首次启动比较慢？**
注册需要创建命名空间、veth 对、wg 接口并拉起路由与 NAT 规则，通常需要数秒；任一步失败会整体回滚，不会留下残留资源。

**设备实际在线，面板却显示离线？**
在线判定依赖对端回包。若客户端在隧道内对探测端口做了 `DROP`（而不是默认的 `REJECT`/RST），探测会一直超时。放行隧道内到 `liveness.probe_port` 的流量即可，或把探测端口改成一个已放行的端口。

**为什么探测端口用 49151 这类高位端口？**
避免与 22/80/443 等常用端口冲突，也避免对端未来启动真实服务时产生误判。高位端口绝大多数时间处于关闭状态，内核稳定返回 RST。

**忘记管理员密码？**
停掉服务，删除 `data/cloud_platform.db` 后重启，会重新创建默认管理员（同时也会清空所有数据）；或在数据库中直接更新 `password_hash`。

**如何备份数据？**
SQLite 数据都在 `data/` 目录，停服务后整体复制即可（建议连 `-wal`、`-shm` 一起复制）。

**可以多个后端副本共享同一数据库吗？**
SQLite 面向单实例部署设计。需要横向扩容时应改用支持并发的数据库，并重新评估 netns 的归属。

## 安全提示

- `jwt.secret` 必须替换为随机长字符串，默认示例值会被拒绝或告警。
- 默认管理员密码请在首次登录后立即修改。
- 后端需要 `privileged` 与 host 网络才能管理 netns、iptables，请仅在受控主机上部署，并限制控制台的网络暴露面（建议置于 TLS 反向代理之后）。
- 设备配置中包含私钥，下载链路应确保可信。

## 许可证

[Apache License 2.0](LICENSE)
