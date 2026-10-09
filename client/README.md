# WireGuard Manager Desktop

Windows 10/11 x64 客户端，使用 Go + Wails v2 + React + Mantine。沿用 Web 端的品牌红、深色侧栏、小圆角和紧凑间距：顶栏显示品牌与服务端，左侧为账号和设备列表，右侧为网络设置、连接状态和实时图表。登录页使用无背景装饰的 420×480 窗口（最小 380×440）；登录后展开至 1000×660（最小 850×600），退出登录或会话过期后缩回。内置官方 WireGuardNT 驱动；Windows 隧道和访问路由通过原生 API 完成，不调用外部 `wireguard.exe`、`route.exe`、PowerShell 或 netsh。

## 使用

1. 打开客户端，用 **https://remote.opcuu.com** 的平台账号登录。
2. 左侧选择**远端网关**，例如 OpenWrt 路由器或 ARM Linux 设备。Windows 自动生成并注册自己的访问身份，不使用所选网关的私钥。访问终端在 Web 后台单独标记，不出现在客户端网关列表中。
3. 嵌入式网关由设备自身在接入前完成 WireGuard、系统转发和回程配置。客户端不安装脚本、不修改设备系统。
4. 在“转发目标 IP / 网段”填写如 `192.168.0.100`、`192.168.10.0/24`。保存后，云端把这些地址交给所选网关，始终保留该网关自己的 `/32`、密钥和现有连接。后续新增、修改、删除目标**无需改动或重启网关**。
5. 点击“连接”。Windows 获取自己的配置，并安装所选目标路由。保存当前正在访问的网关时，只重建 Windows 的访问连接以刷新本机路由；其他网关及嵌入式设备的隧道不重启。仅保存时不要求 Windows 连接现场 LAN。
6. “隧道详情”分别显示远端网关、本机独立接口和云端 Peer 的信息；右侧“流量信息”集中显示上下行速率与累计值。输入使用标签，支持多个 IP / 网段，说明位于标签旁的信息图标。错误使用 toast，底栏保留最新消息。
7. 关闭窗口会隐藏到系统托盘并保持连接。双击图标恢复，右键可打开、断开、退出；退出登录也会断开。

登录页和设备页底部均显示版本号，悬停可查看构建提交编号。`--check-driver` 报告也包含版本与构建编号，便于确认实际运行的 EXE。

跨租户使用独立云端接口、UDP 端口、策略路由表和防火墙隔离。不同租户可声明相同 LAN；同一租户内不同网关的目标不能重叠。切换租户需要重新登录对应账号。Windows 本地与远端网段重叠时，请使用具体目标 `/32`；不能覆盖本机自身 IP、云端 Endpoint 或隧道网段。

## 设备网络前提

网关应自行启用 IPv4 转发，并具备访问目标 LAN 的路由及正确回程（例如设备侧 NAT）。网关 WireGuard Peer 的 AllowedIPs 需允许本租户隧道网段，如 `10.100.1.0/24`。Windows 客户端只管理云端目标归属和本机访问路由；不负责设备操作系统初始化。

## 权限、本地数据与恢复

Go 在同一个 EXE 内直接请求 HTTPS API，Wails 仅负责界面绑定，不启动本地 HTTP 中转。只读请求遇到传输中断最多重试一次，沿用 15 秒时限。保存请求不自动重放；响应中断后刷新检查云端结果。

- 使用系统 WebView2 和官方 WireGuardNT，管理员清单触发 UAC。驱动嵌入 EXE，无需旁置 DLL、外部 WireGuard 程序或 `route.exe`。
- 本机访问私钥按服务端地址和账号隔离，以 Windows 用户 DPAPI 加密保存在 `%APPDATA%\WireguardManagerDesktop\cloud.dpapi`。私钥仅存在本机；云端注册公钥，返回本机地址、云端公钥、PSK 和 Endpoint。密码不保存，JWT 和私钥不返回 React。
- Windows 在此模式只创建访问隧道与路由，不开启本机 LAN 转发，不创建 Windows LAN 防火墙/NAT 规则。升级时仍会恢复旧版本留下的网络修改记录。
- 支持托盘单实例运行；退出进程后不保留隧道。断开或切换时清理本程序的接口和路由，失败会阻止切换并提示重试。
- DNS 跟随系统。图表使用实际驱动计数和到云端隧道 IP 的 ICMP 延迟。仅支持 IPv4 分流。

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

测试覆盖 API 鉴权与拒绝重定向、登录过期、令牌加密、跨账号 / 服务端数据隔离、本机独立身份、网关目标同步、网关配置保持不变、切换顺序、并发断开和失败清理。Windows CI 额外执行 DPAPI、防火墙创建 / 删除、NAT 创建 / 删除（provider 可用且没有现有 NAT 时），以及**最终发布 exe 的驱动资源加载、应用图标加载和原生托盘创建 / 退出自检**，通过后才上传包。

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

升级时先更新服务端，再使用 `client-v0.1.7` 或更新客户端。新客户端需要 `POST /api/wireguard/access`；旧服务端会收到明确的升级提示。已有网关身份不迁移、不换钥；Windows 原先复用的设备身份会作为远端网关保留，重新连接改用独立访问身份。

服务端 `scripts/test_network.sh` 在一次性容器中验证多租户隔离。模拟已自行完成初始化的嵌入式网关，依次仅修改云端 `/32` 和 `/24` 目标，验证回包、删除目标失效、网关配置及路由/规则保持不变。
