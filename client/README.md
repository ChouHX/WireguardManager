# WireGuard Manager Desktop

Windows 10/11 x64 客户端，使用 Go + Wails v2 + React + Mantine。沿用 Web 端的品牌红、深色侧栏、小圆角和紧凑间距：顶栏显示品牌与服务端，左侧为账号和设备列表，右侧为网络设置、连接状态和实时图表。登录页使用无背景装饰的 420×480 窗口（最小 380×440）；登录后展开至 1000×660（最小 850×600），退出登录或会话过期后缩回。内置官方 WireGuardNT 驱动；隧道、路由、转发、防火墙和 NAT 通过原生 API 完成，不调用外部 `wireguard.exe`、`route.exe`、PowerShell 或 netsh。

## 使用

1. 管理员在构建时指定管理平台地址。本次部署为 **https://remote.opcuu.com**。打开客户端，直接使用平台账号密码登录，无需手动导入 `.conf`。
2. 左侧选择**当前电脑要使用的设备配置**。每台机器必须有自己的配置；不要与正在运行的现场网关复用私钥。列表属于当前登录账号 / 租户，跨客户时退出登录后使用对应账号。
3. 现场网关在“设备局域网 · 同步云端”填写 `192.168.0.100` 或 `192.168.0.0/24`，也可点击“探测本机局域网”。点击保存或连接后，服务端将局域网与不可编辑的设备虚拟地址 `/32` 合并后保存到 `allowed_ips`，云端据此转发。API 的 `allowed_ips` 与实际 WireGuard 配置均保留设备自身 `/32`，另以 `device_lan` 返回可编辑的局域网；清空局域网也不会移除设备自身地址。仅访问远端的电脑可留空。
4. “本机访问目标 · 仅本机”留空时，自动使用同账号下其他设备声明的局域网。填写时覆盖自动目标，单个 IP 自动转成 `/32`。这些目标仅存本机，不写服务端；仅修改此字段时不会更新云端局域网。保存结果会说明是否更新了云端。同一租户中设备局域网声明不能重叠；不同租户可以重复。
5. 点击连接，客户端重新获取配置、自动选择 LAN 网卡并配置网络，等待握手后访问目标。选择其他设备再点击“切换到此设备”，会先断开旧连接、恢复网络后建立新连接。已连接配置需先断开才能编辑。
6. “隧道详情”显示接口公钥、地址、监听端口、MTU，以及云端 Peer 的公钥、端点、有效路由、保活和最近握手；不显示私钥。右侧“流量信息”集中显示上下行实时速率与累计值。“网络设置”使用标签输入，支持粘贴多个 IP / 网段、去重、校验及单个删除；尚未按回车的内容也会随保存或连接提交。输入说明位于标签旁的信息图标，鼠标悬停或键盘聚焦时显示。
7. 关闭窗口会隐藏到右下角系统托盘并保持连接；双击图标恢复窗口，右键可打开、断开或退出。点击“退出程序”或托盘“退出”会先恢复网络；清理失败会显示错误并保留程序供重试。退出登录会断开连接。
8. 操作错误与网络提示使用 Mantine toast，不挤压设备面板。底部保留最新一条消息，toast 消失后仍可点击底栏查看完整内容并复制。

两端现场网段相同，仍需要云端独立接口 / 端口、专用策略路由表和防火墙共同隔离，单靠多网卡不能解决 Linux 的重叠路由。操作电脑本地与远端网段重叠时，访问目标请填写具体 `/32`。目标不能恰好是本机自身 IP，也不能覆盖云端 Endpoint、隧道网段或本设备声明的 LAN。

## 自动配置与现场网络

- 自动探测已启用的物理 Ethernet / Wi-Fi 网卡，通过下挂 IP 的最长直连前缀及接口 metric 选择，无需网卡下拉框。支持多 LAN 网卡。不会将默认路由或 Docker / WSL / VPN 虚拟网卡作为现场出口。优先级完全相同时会提示断开不使用的网络，避免静默选错。
- 默认启用隧道和匹配 LAN 接口的 IPv4 转发；添加仅作用于这些接口、VPN / 声明网段的入站与出站允许规则，不关闭 Windows 防火墙。企业策略中的强制阻断仍可能优先于这些规则。
- 多网段在写入 Windows 防火墙时使用专用的无空格逗号列表；界面的显示格式保持不变。旧版传入的逗号后空格会触发 `RemoteAddresses: Exception occurred`，提高管理员权限不能解决此格式错误。
- 系统支持 WinNAT 且没有现有 NAT 时，自动为租户 VPN 网段创建 NAT。VPN 电脑访问下挂设备时，现场设备通过网关 LAN 地址回复，通常无需单独配置返回路由。
- 系统已有 NAT（例如 Docker / WSL）或没有可用的 WinNAT provider 时，不改动既有 NAT；界面明确显示路由模式和原因。此时 PLC 或现场路由器仍需返回路由，例如 `10.100.1.0/24 → 192.168.0.10`（Windows 网关 LAN IP）。
- **LAN 设备主动连接 VPN**仍需把流量送到这台 Windows 网关，例如设置其为默认网关或添加 VPN / 远端网段路由。客户端不能替另一台 PLC 或现场路由器改默认网关；跨现场 LAN 到 LAN 访问也需正确的双向路由。握手成功不代表所有现场设备已具备路由。

## 权限、本地数据与恢复

客户端由同一个 EXE 内的 Go 模块直接请求云端 HTTPS API，Wails 负责界面和 Go 方法之间的调用，不额外运行本地代理中转。连接时并行获取设备列表与配置；局域网未修改时不重复下载配置。失败后界面仅同步本机状态，不再自动追加后台刷新。只读请求遇到传输中断最多重试一次，共用原来的 15 秒请求时限；保存操作不自动重放，响应中断时应刷新确认是否已经保存。错误会显示失败的操作、超时 / 传输中断及实际耗时。

- 使用系统 WebView2；缺失时先安装 Microsoft Edge WebView2 Runtime。可执行文件带 `requireAdministrator` 清单，启动触发 UAC。
- 仅允许单实例，支持系统托盘后台运行；不提供 Windows 服务，退出进程后不保持隧道。
- 密码不保存。勾选保持登录后，令牌使用当前 Windows 用户 DPAPI 加密保存在 `%APPDATA%\WireguardManagerDesktop\cloud.dpapi`；本机访问目标按服务端 / 账号 / 设备隔离。JWT 和 WireGuard 私钥不返回 React，不写浏览器存储；每次连接重新鉴权并从服务器取配置。旧版 `profiles.dpapi` 保留但不再用于新界面。
- 正式包将官方 `wireguard.dll`（WireGuardNT，非 `wintun.dll`）嵌入 PE 资源，通过官方 `load_wgnt_from_rsrc` 加载，无需旁置 DLL 或安装官方客户端。
- 修改网络前先写恢复记录。断开时移除本程序创建的防火墙规则、NAT、隧道和路由，恢复网卡转发原值；清理失败会阻止切换并允许重试。异常退出后，下次用原 Windows 用户启动时恢复。请保持原网卡存在且启用，不要手工删除恢复记录。进程被强制终止后，物理接口和防火墙恢复需要重新打开程序。
- DNS 保持本机设置。图表使用实际驱动字节计数和到云端隧道 IP 的 ICMP 延迟；握手超时和 ICMP 超时分别显示。当前仅支持 IPv4 分流，不支持全流量代理、脚本钩子和任意第三方配置。

## 构建

需要 Go 1.25+、Node.js 22.12+ / npm。Linux 和 Windows 均可生成 Windows x64 包。仓库根目录执行：

```sh
npm --prefix client/frontend ci
npm --prefix client/frontend run build
cd client
go test ./cloud ./service
go run ./tools/package -server-url https://remote.opcuu.com
```

也可通过 `WGM_SERVER_URL` 环境变量指定地址。打包工具要求完整 http/https URL，支持反向代理子路径；构建值嵌入程序，不可在登录界面修改。HTTPS 部署推荐使用受系统信任的证书。

工具校验官方 WireGuardNT 1.1 压缩包 SHA-256，生成内嵌驱动、应用图标与管理员清单的 `.syso`，编译 GUI exe，检查最终 PE 内容并收集 Go / 前端运行依赖许可。窗口、任务栏与托盘使用同一品牌图标。离线驱动可额外指定 `-driver-zip /path/to/wireguard-nt-1.1.zip`，仍校验哈希。无需 CGO 或全局 Wails CLI。

产物为 `client/build/bin/WireguardManagerDesktop-windows-amd64.zip`（exe、说明、许可）及 `SHA256SUMS`。GitHub **Build Windows Client** 使用仓库变量 `WGM_SERVER_URL`，手动触发时可用 `server_url` 输入覆盖。exe 未做应用代码签名。

## 验证

连接时若提示 `Endpoint` 缺少公网地址，管理员需在平台「系统设置 → WireGuard 公网 IP / 域名」填写中继直连地址。管理平台 HTTPS 地址和 WireGuard UDP 地址可以不同；普通 Cloudflare / CDN 代理域名不能作为 WireGuard UDP 中继地址。修正设置后重新连接即可拉取新配置。

测试覆盖 API 鉴权与拒绝重定向、登录过期、令牌加密、跨账号 / 服务端数据隔离、LAN 自动探测、云端声明与配置重新拉取、切换顺序、并发断开和失败清理。Windows CI 额外执行 DPAPI、防火墙创建 / 删除、NAT 创建 / 删除（provider 可用且没有现有 NAT 时），以及**最终发布 exe 的驱动资源加载、应用图标加载和原生托盘创建 / 退出自检**，通过后才上传包。

原生网络集成测试仅在隔离、已提权的 Windows runner 设置 `WGM_TEST_WINDOWS_NETWORK=1` 时启用，普通 `go test` 不修改机器网络。CI 的创建 / 删除测试不等同于真实 PLC 双向流量验收。现场还应验证登录、握手、目标访问、重叠地址切换，以及断开后的网络恢复。

连接前会回读驱动配置，在 Go 内核对接口公钥、Peer 公钥、预共享密钥和端点，发现不一致即停止连接。Windows CI 还会让最终 EXE 使用正式客户端后端，与另一张临时 WireGuardNT 网卡通过本机 UDP 完成双向握手（启用 PSK），然后清理网络资源；此项不使用线上账号，也不能替代现场公网连通性验收。

若一直“等待握手”，先确认云端 `latest-handshakes` 是否为 `0`，不要仅凭云端收发计数判断隧道已连通。退出客户端后，可用管理员 PowerShell 运行本机握手自检：

```powershell
Start-Process .\WireguardManagerDesktop.exe -ArgumentList '--check-transport', 'transport-check.json' -Wait
Get-Content .\transport-check.json
```

自检会临时使用 `10.254.253.0/24`，通过报告 `handshakeConfirmed` 给出结果，不输出密钥；如果自检通过但实际连接失败，需要继续核对两端抓包、配置一致性和网络环境。

若启动时仍报告驱动加载错误，可在 PowerShell 执行：

```powershell
Start-Process .\WireguardManagerDesktop.exe -ArgumentList '--check-driver', 'driver-check.json' -Wait
```

报告包含内置服务端、DLL 版本、资源大小和加载错误，不含私钥；此检查不创建隧道，不要求预先安装内核驱动。旧版 `8434926` 的资源名大小写错误已修复，请使用最新完整包。

升级时同时更新服务端镜像和桌面客户端。新版客户端兼容旧服务端返回的局域网字段；旧客户端未识别独立 `device_lan` 字段，应更新到 `client-v0.1.3` 或更新版本。
