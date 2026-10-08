# WireGuard Manager Desktop

Windows 10/11 x64 客户端，Go + Wails v2 + React。使用官方 WireGuardNT 内核驱动和 `winipcfg`，隧道、IP 地址、路由、网卡转发和 ICMP 探测均通过原生 API 完成，不调用外部 `wireguard.exe`、`route.exe` 或 PowerShell。

## 使用

1. 在云端为每个客户分配独立账号 / 租户。每台设备创建独立的设备配置，同一份私钥不要同时用于两台机器。
2. 现场网关设备在平台填写 **设备局域网**，如 `192.168.0.0/24`；远程操作电脑的设备局域网留空。转发默认启用，平台不再保存客户端网卡和访问目标。
3. 下载对应设备的新 `.conf` 配置。在客户端点击“导入配置”，可同时导入多个客户的配置，然后设置现场名称。
4. 现场网关：选择处于其设备局域网中的本机网卡。访问远端的电脑：填写目标 `192.168.0.100`（自动变为 `/32`）或网段。目标只存本机；平台导出仅包含租户隧道网段。
5. 点击“连接现场”。等待握手成功后访问目标。切换下拉框会先断开旧现场，再点击“连接现场”建立新隧道。关闭窗口会断开连接。

若客户 A 和 B 都有 `192.168.0.100`，分别导入 A 和 B 下为此操作电脑创建的设备配置，两份配置各自填写相同目标地址。不同客户有不同云端端口和隧道子网；同一时刻只有一个现场连接。云端仍需专用策略路由表和防火墙隔离，单靠多个 WireGuard 网卡不能解决重叠 LAN 路由。

## 现场网络要求

IP 转发 **不等于 NAT**。此版本默认打开隧道接口以及所选 LAN 网卡的 IPv4 转发，不自动启用 ICS / WinNAT，也不修改局域网设备地址、默认网关或其他防火墙规则。

例如租户隧道为 `10.100.1.0/24`，现场 Windows 网关 LAN IP 为 `192.168.0.10`，PLC 为 `192.168.0.100`：PLC 或其默认路由器需要一条 `10.100.1.0/24 → 192.168.0.10` 的返回路由。若设备不能配置路由，可在现场路由器设置，或使用有 NAT 功能的现场网关。按现场策略允许必要的转发流量和目标端口。没有返回路由且没有 NAT 时，握手正常也无法访问 PLC。

访问电脑的本地网络若也与现场重叠，优先填写具体 `/32` 目标。目标若恰好是本机自身 IP，Windows 会本地接收，不能用隧道路由覆盖；客户端会拒绝该配置。也拒绝覆盖云端 Endpoint、隧道子网、本设备 LAN 的目标。IPv6、全流量代理、多云端 Peer、脚本钩子和任意第三方 WireGuard 配置暂不支持。

## 权限与本地数据

- 使用系统 WebView2，通常 Windows 11 已安装；缺失时须先安装 Microsoft Edge WebView2 Runtime。不是每台 Windows 10 都预装。
- 可执行文件带 `requireAdministrator` 清单，启动触发 UAC。只支持一个运行实例；连接期间需保持程序运行，不提供后台 Windows 服务。
- 正式打包使用官方 `wireguard.dll`（WireGuardNT），不是 `wintun.dll`。DLL 原样嵌入 PE 资源，通过官方 `load_wgnt_from_rsrc` 加载，无需旁置驱动 DLL 或安装官方 WireGuard 客户端。
- 配置保存在 `%APPDATA%\WireguardManagerDesktop\profiles.dpapi`，用当前 Windows 用户的 DPAPI 加密。私钥不返回 React，不写浏览器存储。导入的原始 `.conf` 仍由用户保管；客户端不删除源文件。
- 网卡转发原值写入恢复记录，断开时恢复；异常退出后，下次用原 Windows 用户启动时恢复。异常退出后请尽快重新打开程序，保持原网卡存在且启用；不要手工删恢复记录。程序不保证进程被强制终止后能立即恢复物理网卡状态。
- 连接过程中创建的隧道由本进程持有，关闭时移除该接口及路由；清理失败会阻止切换，允许重试断开。
- DNS 保持本机设置。图表显示驱动字节计数差值与到云端隧道 IP 的 ICMP 延迟；握手过期、ICMP 超时会分别显示，无模拟数据。

## 构建

需要 Go 1.25+、Node.js 22.12+ / npm，Linux 和 Windows 均可构建 Windows x64 包。仓库根目录执行：

```sh
npm --prefix client/frontend ci
npm --prefix client/frontend run build
cd client
go test ./service
go run ./tools/package
```

打包工具校验官方 WireGuardNT 1.1 压缩包 SHA-256，生成包含驱动和管理员清单的 `.syso`，交叉编译 GUI exe，并检查最终 PE 的驱动和清单内容。无需 CGO 或全局 Wails CLI。离线驱动包可用 `go run ./tools/package -driver-zip /path/to/wireguard-nt-1.1.zip` 指定，仍会验证哈希。

产物：`client/build/bin/WireguardManagerDesktop-windows-amd64.zip`（含 exe、使用说明和许可）及 `SHA256SUMS`。GitHub Actions 的 **Build Windows Client** 工作流会上传同样的包；exe 未做应用代码签名，正式分发可另加签名步骤。

测试覆盖配置解析、目标校验、现场切换顺序、失败清理、统计重置和加密保存失败保护。Windows CI 还运行 DPAPI 往返测试。Linux 上的交叉编译不能替代 Windows 真机的驱动安装、UAC、路由、转发和 WebView2 验收。现场验收请依次确认握手、目标访问、重叠地址现场切换、断开后原路由和网卡转发状态恢复。
