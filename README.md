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
- **秒级在线感知** —— 每 2 秒进入设备所属命名空间，向其隧道地址的高位端口发起一次 TCP 探测（纯 Go `setns`，无外部进程）：对端内核回 RST 或完成握手即为在线，连续无响应即离线，约 4 秒完成，不受 WireGuard 握手周期拖累。
- **流量与资源监控** —— 设备握手状态、收发流量、系统 CPU / 内存 / 磁盘 / 网络趋势一屏掌握。
- **精细管控** —— 设备粒度限速、启用禁用、网关转发（客户端侧 NAT，网卡按设备指定）、AllowedIPs 网段自定义，支持为单个设备启用预共享密钥（PSK）以增强抗中间人与抗量子能力。
- **零配置部署** —— 开箱即用：JWT 密钥首次启动自动生成并随数据一起持久化，网络与监控等参数全部在管理界面「系统设置」中调整；部署侧无需任何配置文件。
- **现代控制台** —— React 19 + Mantine，紧凑式布局、明暗主题、中英文双语、移动端自适应。
- **轻量部署** —— 嵌入式 SQLite（纯 Go 驱动，无需 CGO、无需独立数据库服务）；支持单容器部署，一个进程同时提供控制台与 API。镜像由 CI 构建并推送 GHCR。

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

  WEB["控制台 · :3000"]
  INET((互联网))

  D1 -- "WireGuard UDP" --> W1
  D2 -- "WireGuard UDP" --> W2
  W1 -- "veth + NAT" --> INET
  W2 -- "veth + NAT" --> INET
  WEB -- "同进程提供页面与 /api" --> API
  API -. "netns / wg 编排与统计" .-> ns1
  API -. "netns / wg 编排与统计" .-> ns2
```

- **后端**（Go + Gin + GORM）：以 host 网络运行，负责 WireGuard 操作、命名空间编排与数据持久化。
- **前端**（React + Vite + Mantine）：构建为静态产物，由后端一并托管（`WEB_ROOT` 模式），无需单独的 Web 容器。
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

docker compose up -d          # 无需任何配置
# 或使用带环境检查与部署自检的脚本
./deploy.sh
# 本地从源码构建
docker compose -f docker-compose.build.yml up -d --build
```

启动后使用内置默认值：端口 `3000`，数据写入 `./data`，JWT 密钥首次启动自动生成并保存到 `./data/jwt.secret`。

指定端口（三选一）：

```bash
WM_SERVER_PORT=8080 docker compose up -d    # 临时指定
echo "WM_SERVER_PORT=8080" >> .env          # 写进 .env（自动读取）
# 或直接改 docker-compose.yml 里的默认值
```

访问 `http://<SERVER_IP>:3000`，默认账号 `admin@platform.com` / `password`（**首次登录后请立即修改**）。

镜像标签可用 `WM_IMAGE_TAG` 指定，默认 `latest`，也可用 `sha-<短哈希>` 回滚到某次构建：

```bash
WM_IMAGE_TAG=sha-2df1b06 docker compose up -d
```

## 配置参考

### 配置项

全部参数都有内置默认值，**开箱即用、无需配置文件**。需要调整时：**部署相关**直接改 `docker-compose.yml`（或设环境变量），**运行相关**在管理界面「系统设置」中修改；非容器部署仍可选用 `config.yaml`。

**部署参数**（内置默认值，需要时再覆盖）：

| 变量 | 默认值 | 覆盖方式 |
| --- | --- | --- |
| 端口 | `3000` | `WM_SERVER_PORT=8080 docker compose up -d`，或写进 `.env`，或改 compose 默认值 |
| 数据目录 | `./data` | 改 `docker-compose.yml` 中 `./data:/root/data` 的左侧 |
| 配置目录 | `./wg_config` | 改 `docker-compose.yml` 中 `./wg_config:/etc/wg_config` 的左侧 |
| JWT 密钥 | 自动生成 | 首次启动写入 `data/jwt.secret`；也可用 `WM_JWT_SECRET` 显式指定 |
| 数据库路径 | `/root/data/cloud_platform.db` | 后端默认值，随数据目录挂载自动生效 |
| 管理员账号 | `admin@platform.com` / `password` | 首次启动创建，可用 `WM_DEFAULT_ADMIN_*` 覆盖 |
| 镜像版本 | `latest` | 改 `docker-compose.yml` 中 `image` 的标签，如 `sha-2648b94` |

**管理界面可调项**（「系统设置」页面，存库即时生效）：

| 分组 | 键 | 说明 |
| --- | --- | --- |
| 网络 | `network.server_ip` | 服务器公网 IP，下发到客户端 Endpoint |
| | `network.out_interface` | 出口网卡（界面可自动探测并改选） |
| | `network.base_subnet` / `base_port` | 隧道网段前缀与端口起始值 |
| | `network.dns` | 下发给客户端的 DNS |
| | `network.client_allowed_ips` | 客户端 AllowedIPs，留空按设备所在网段推导 |
| 监控 | `monitoring.interval_seconds` / `retention_hours` | 采样间隔与记录保留时长 |
| 在线判定 | `liveness.probe_port` | 探测端口（唯一需要人工介入的参数） |
| 鉴权 | `jwt.expire_hours` | 登录有效期 |
| 设备默认值 | `wireguard.default_preshared_key` | 新建设备是否默认启用预共享密钥 |

> 同名的 `WM_*` 环境变量仍可覆盖启动默认值；数据库中一旦存在该键，以界面上的值为准。

### 环境变量

所有参数都可选用 `WM_*` 环境变量覆盖（优先级高于配置文件与内置默认值）。容器部署时最常用的是端口，compose 已默认透传这一项：

```bash
WM_SERVER_PORT=8080 docker compose up -d
```

其余变量未在 compose 中声明，需要时按同样格式加进 `environment:` 即可。

其余变量与其内置默认值可在 [`internal/config/config.go`](internal/config/config.go) 的 `defaultConfig()` 中查到；管理界面「系统设置」里的各项也都能用同名环境变量设定初始值（首次启动写入数据库后即以界面配置为准）。

### 客户端配置

下载的 `AllowedIPs` 默认按设备所在网段下发（服务端接口 `10.100.0.1/24`、设备分配到 `10.100.0.2` 时下发 `10.100.0.0/24`），只把 VPN 网段流量送进隧道。需要全局代理时：

```yaml
network:
  client_allowed_ips: "0.0.0.0/0, ::/0"
```

开启「网关转发」后，该设备可作为网关，让其他设备经它访问 VPN。生成的客户端配置形如：

```
PostUp = iptables -t nat -A POSTROUTING ! -o %i -j MASQUERADE; iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT
```

`! -o %i` 由 `wg-quick` 把 `%i` 展开为隧道接口名，含义是「只要不是从隧道出去的流量就做 NAT」——**无需知道客户端网卡叫什么**，`eth0`、`wlan0`、`enp3s0` 都能正确工作，因此客户端网卡一栏默认留空即可。

个别场景需要限定具体网卡时（例如不希望所有出站流量都被 NAT），才在设备表单里填写客户端自己的物理网卡名，此时配置会改为 `-o <网卡>`。注意感叹号必须写在选项**之前**（`! -o wg0` 合法，`-o ! wg0` 会被 iptables 拒绝）。

> 这些指令由 Linux 客户端的 `wg-quick` 执行；Windows/macOS/Android/iOS 的官方客户端会忽略 `PostUp`/`PreDown`，需要在该设备上另行配置系统路由与 NAT。

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
├── internal/
│   ├── config/                 # 配置加载、校验、环境变量覆盖
│   ├── database/               # SQLite 初始化与默认管理员
│   ├── handlers/               # HTTP 处理层
│   ├── middleware/             # 鉴权与用户缓存
│   ├── models/                 # 数据模型
│   ├── routes/                 # 路由注册
│   └── services/               # netns / WireGuard / 监控采样 / 存活探测
├── frontend/                   # React + Vite + MantineUI 源码
├── Dockerfile                  # 单容器镜像：Go 后端 + 前端产物 + wg 工具链
├── docker-compose.yml          # 默认编排：拉取 GHCR 镜像
├── docker-compose.build.yml    # 本地源码构建编排
├── wg_config/                  # 挂载至 /etc/wg_config
└── data/                       # 数据目录：SQLite 数据库 + 自动生成的 jwt.secret
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
docker compose ps                             # 查看服务状态
docker compose logs -f app                    # 跟踪服务日志
docker compose pull && docker compose up -d   # 更新到最新镜像
docker compose down                           # 停止服务
```

## 常见问题

**注册 / 首次启动比较慢？**
注册需要创建命名空间、veth 对、wg 接口并拉起路由与 NAT 规则，通常需要数秒；任一步失败会整体回滚，不会留下残留资源。

**在线状态是怎么判定的？**
服务端每 2 秒进入设备所属的网络命名空间，向它的隧道地址加探测端口（`liveness.probe_port`，默认 49151）发起一次 TCP 连接，判定顺序为：

1. 完成握手，或收到 `connection refused`（对端内核回 RST）→ **在线**，并显示往返耗时；
2. 探测无响应，但近 30 秒内隧道仍有流量（保活包即可）→ **在线**；
3. 两者都不满足 → 计一次失败，连续 2 次（约 4 秒）后判**离线**。

节奏、超时、确认次数与流量窗口都与实现强相关，由服务端内部决定，不开放配置——用户只需关心探测端口这一个参数。设备列表里会显示判定依据（探测有响应 / 隧道有流量 / 近期有流量 / 探测无响应），便于排查。

**为什么必须在设备的命名空间里探测？**
每台设备的隧道地址（如 `10.100.0.2`）只存在于它所属账号的 netns 内，宿主机路由表里没有该网段——从宿主命名空间发包会落到默认路由上，无论对端是否在线都只会超时。后端在容器内以特权模式运行并共享 `/var/run/netns`，用 `setns` 切入目标命名空间，探测完再切回，整个过程是纯 Go 的，不 fork 任何外部命令。

**设备 A 访问不到设备 B 背后的局域网？**
跨网段转发需要三件事同时成立，本平台已自动处理前两项：① 服务端允许隧道内互转（发夹转发 `-i %i -o %i`）；② 服务端把对端网段写进该设备的 `allowed-ips`（WireGuard 的加密路由表，决定包发给谁，并在命名空间内自动生成路由）；③ 设备 A 的客户端 `AllowedIPs` 要包含目标网段，否则 A 根本不会把这些包送进隧道——这一项需要为 A 单独配置。

**为什么判定不参考握手时间？**
`last handshake` 在客户端断开后**不会被清空，只是停住**，而它又只在密钥重协商时更新（默认 120 秒）。拿它当在线依据，会让离线判定滞后一个重协商周期——这是早期版本"设备断开很久仍显示在线"的根因，现已完全移除该依据，只依赖实时探测与流量增量。

**设备明明连着，为什么显示离线？**
看设备列表里的探测时延：能显示出毫秒数就是在线的。持续离线通常是隧道本身中断（客户端退出、网络切换、NAT 映射失效），也可能是客户端防火墙丢弃了隧道内的探测包——后者可把 `traffic_stale_seconds` 调大，让保活流量继续维持在线状态。

**忘记管理员密码？**
停掉服务，删除 `data/cloud_platform.db` 后重启，会重新创建默认管理员（同时也会清空所有数据）；或在数据库中直接更新 `password_hash`。

**JWT 密钥放在哪？**
未显式配置时，后端首次启动会生成一个随机密钥并写入 `data/jwt.secret`（权限 0600），之后每次启动复用同一个密钥，因此重启不会导致登录失效。**备份数据时请连同这个文件一起复制**；若它丢失，所有已签发的 Token 会失效，用户需要重新登录。想自己掌控密钥时，设置 `WM_JWT_SECRET` 即可覆盖（优先级高于文件）。若数据目录不可写，后端会退回进程内临时密钥并打印告警，服务仍可启动，但重启后需要重新登录。

**如何备份数据？**
SQLite 数据与 JWT 密钥都在 `data/` 目录，停服务后整体复制即可（建议连 `-wal`、`-shm` 一起复制）。

**可以多个后端副本共享同一数据库吗？**
SQLite 面向单实例部署设计。需要横向扩容时应改用支持并发的数据库，并重新评估 netns 的归属。

## 安全提示

- JWT 密钥默认自动生成并持久化到 `data/jwt.secret`（0600）；若通过 `WM_JWT_SECRET` 显式指定，请使用足够长的随机字符串。
- 默认管理员密码请在首次登录后立即修改。
- 后端需要 `privileged` 与 host 网络才能管理 netns、iptables，请仅在受控主机上部署，并限制控制台的网络暴露面（建议置于 TLS 反向代理之后）。
- 设备配置中包含私钥，下载链路应确保可信。

## 许可证

[Apache License 2.0](LICENSE)
