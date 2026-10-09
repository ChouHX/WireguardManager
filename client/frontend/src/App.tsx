import { useCallback, useEffect, useRef, useState } from "react";
import {
  ActionIcon,
  Avatar,
  Badge,
  Box,
  Button,
  Card,
  Checkbox,
  Divider,
  Group,
  Loader,
  Paper,
  PasswordInput,
  ScrollArea,
  Stack,
  Text,
  Tabs,
  TextInput,
  ThemeIcon,
  Title,
  Tooltip,
} from "@mantine/core";
import {
  IconArrowDown,
  IconArrowUp,
  IconChevronRight,
  IconCircleFilled,
  IconCloud,
  IconDeviceDesktop,
  IconInfoCircle,
  IconLogout,
  IconNetwork,
  IconPlugConnected,
  IconPlugConnectedX,
  IconRefresh,
  IconSearch,
  IconDownload,
} from "@tabler/icons-react";
import { notifications } from "@mantine/notifications";
import { AddressInput, addressDraft } from "./AddressInput";
import { TunnelOverview } from "./TunnelOverview";
import { formatBytes } from "./format";
import { LatestMessage, type ActivityMessage } from "./LatestMessage";
import { WireGuardLogo } from "./WireGuardLogo";
import {
  api,
  type Desktop,
  type Device,
  type Status,
} from "./api";
const empty: Desktop = { serverURL: "", user: null, devices: [], message: "" };
const offline: Status = {
  profileID: "",
  state: "disconnected",
  rxBytes: 0,
  txBytes: 0,
  rxBps: 0,
  txBps: 0,
  latencyMS: -1,
  handshake: "",
  error: "",
  network: {
    adapters: [],
    forwarding: false,
    firewall: false,
    nat: false,
    warning: "",
  },
};
const states: Record<string, string> = {
  disconnected: "未连接",
  handshaking: "等待握手",
  connected: "已连接",
  stale: "握手已过期",
  error: "连接异常",
};
function FieldLabel({
  id,
  label,
  help,
}: {
  id: string;
  label: string;
  help: string;
}) {
  return (
    <Group gap={4}>
      <Text component="label" htmlFor={id} size="xs" fw={600}>
        {label}
      </Text>
      <Tooltip
        label={help}
        multiline
        w={280}
        withArrow
        events={{ hover: true, focus: true, touch: true }}
      >
        <ActionIcon
          size="xs"
          color="gray"
          variant="subtle"
          aria-label={`${label}说明`}
        >
          <IconInfoCircle size={14} />
        </ActionIcon>
      </Tooltip>
    </Group>
  );
}
function Sparkline({
  data,
  color,
  label,
}: {
  data: number[];
  color: string;
  label: string;
}) {
  const max = Math.max(1, ...data.filter((n) => n >= 0));
  const paths: string[] = [];
  let path = "";
  data.forEach((n, i) => {
    if (n < 0) {
      if (path) paths.push(path);
      path = "";
      return;
    }
    const x = (i * 100) / 59,
      y = 36 - (n / max) * 31;
    path += `${path ? "L" : "M"}${x},${y} `;
  });
  if (path) paths.push(path);
  return (
    <svg
      className="sparkline"
      viewBox="0 0 100 40"
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
    >
      <path
        d="M0 36 H100 M0 20 H100 M0 4 H100"
        stroke="#e9eef3"
        strokeWidth=".3"
      />
      {paths.map((d, i) => (
        <path key={i} d={d} stroke={color} fill="none" strokeWidth=".8" />
      ))}
    </svg>
  );
}
export default function App() {
  const [state, setState] = useState<Desktop>(empty),
    [status, setStatus] = useState<Status>(offline),
    [selected, setSelected] = useState(""),
    [query, setQuery] = useState("");
  const [lans, setLANs] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true),
    [notice, setNotice] = useState("");
  const [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [remember, setRemember] = useState(true),
    [history, setHistory] = useState<Status[]>([]);
  const [lanSearch, setLANSearch] = useState("");
  const [latestMessage, setLatestMessage] = useState<ActivityMessage | null>(
    null,
  );
  const lastToast = useRef({ message: "", at: 0 });
  const report = useCallback(
    (message: string, level: ActivityMessage["level"]) => {
      if (!message) return;
      const text = message.replace(/^Error:\s*/, "");
      const now = Date.now();
      if (
        lastToast.current.message === text &&
        now - lastToast.current.at < 3000
      )
        return;
      lastToast.current = { message: text, at: now };
      setLatestMessage({
        message: text,
        level,
        time: new Date(now).toLocaleTimeString(),
      });
      notifications.show({
        title: level === "red" ? "操作未完成" : "提示",
        message: text,
        color: level,
        autoClose: level === "red" ? 8000 : 4500,
      });
    },
    [],
  );
  const activeID = useRef("");
  const selectedRef = useRef("");
  const device = state.devices.find((d) => d.id === selected);
  const active = state.devices.find((d) => d.id === status.profileID);
  const connectedHere = !!device && status.profileID === device.id;
  const locked = busy;
  const select = (d: Device | undefined) => {
    const id = d?.id ?? "";
    selectedRef.current = id;
    setSelected(id);
    setLANs(d?.lans ?? "");
    setLANSearch("");
    setNotice("");
  };
  const apply = (next: Desktop, resetDraft = false) => {
    setState(next);
    if (!next.user) {
      select(undefined);
      setPassword("");
      return;
    }
    const keep = next.devices.find((d) => d.id === selectedRef.current);
    if (!keep || resetDraft) select(keep ?? next.devices[0]);
  };
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
      setStatus(await api().Status());
    } catch (e) {
      setError(String(e));
      try {
        const next = await api().Snapshot();
        apply(next);
      } catch {
        /* Keep the original actionable error. */
      }
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const s = await api().Status();
        if (!disposed) {
          setStatus(s);
          if (activeID.current !== s.profileID) {
            activeID.current = s.profileID;
            setHistory([]);
          }
          if (s.profileID) setHistory((h) => [...h, s].slice(-60));
        }
      } catch {
        /* Startup and operation errors are shown by their own requests. */
      } finally {
        if (!disposed) timer = setTimeout(poll, 1500);
      }
    };
    Promise.resolve()
      .then(() => api().Bootstrap())
      .then((next) => {
        if (!disposed) {
          apply(next, true);
          setNotice(next.message);
        }
      })
      .catch((e) => {
        if (!disposed) setError(String(e));
      })
      .finally(() => {
        if (!disposed) {
          setLoading(false);
          void poll();
        }
      });
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, []);
  useEffect(() => {
    report(notice, "teal");
  }, [notice, report]);
  useEffect(() => {
    report(status.network.warning, "yellow");
  }, [status.network.warning, report]);
  useEffect(() => {
    report(status.error, "red");
  }, [status.error, report]);
  useEffect(() => {
    report(error, "red");
  }, [error, report]);
  useEffect(() => {
    const offError = window.runtime?.EventsOn("desktop:error", (message) =>
      report(message, "red"),
    );
    const offNotice = window.runtime?.EventsOn("desktop:notice", (message) =>
      report(message, "teal"),
    );
    return () => {
      offError?.();
      offNotice?.();
    };
  }, [report]);
  const refresh = () =>
    void run(async () => {
      const next = await api().Refresh();
      apply(next, true);
      setNotice("网关列表已更新");
    });
  const connect = () =>
    void run(async () => {
      const next = await api().Connect(
        selected,
        addressDraft(lans, lanSearch),
      );
      apply(next, true);
    });
  const disconnect = () =>
    void run(async () => {
      await api().Disconnect();
      setHistory([]);
    });
  const save = () =>
    void run(async () => {
      const next = await api().SaveDevice(
        selected,
        addressDraft(lans, lanSearch),
      );
      apply(next, true);
      setNotice(next.message);
    });
  const downloadSetup = () =>
    void run(async () => {
      const message = await api().DownloadGatewaySetup(selected);
      if (message) setNotice(message);
    });
  const authenticated = Boolean(state.user);
  useEffect(() => {
    void window.go?.main.App.SetWindowLayout(authenticated).catch((e) =>
      report(String(e), "yellow"),
    );
  }, [authenticated, report]);
  const login = () =>
    void run(async () => {
      const next = await api().Login(email, password, remember);
      setPassword("");
      apply(next, true);
    });
  const visible = state.devices.filter((d) =>
    `${d.name} ${d.address} ${d.lans}`
      .toLowerCase()
      .includes(query.toLowerCase()),
  );
  const color =
    status.state === "connected"
      ? "teal"
      : status.state === "error"
        ? "red"
        : status.state === "disconnected"
          ? "gray"
          : "yellow";
  if (!state.user)
    return (
      <main className="login-shell">
        <div className="login-main">
          <Group gap={10} mb="lg">
            <WireGuardLogo size={34} />
            <div>
              <Title order={3}>WireGuard Manager</Title>
              <Text size="xs" c="dimmed">
                登录管理平台
              </Text>
            </div>
          </Group>
          <Group gap={6} mb="lg" wrap="nowrap" c="dimmed">
            <IconCloud size={15} />
            <Text size="xs" truncate>
              {state.serverURL || "正在读取服务端…"}
            </Text>
          </Group>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              login();
            }}
          >
            <Stack gap="md">
              <TextInput
                label="邮箱"
                placeholder="you@example.com"
                type="email"
                autoComplete="username"
                required
                value={email}
                onChange={(e) => setEmail(e.currentTarget.value)}
                disabled={busy || loading}
              />
              <PasswordInput
                label="密码"
                placeholder="输入账号密码"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.currentTarget.value)}
                disabled={busy || loading}
              />
              <Checkbox
                label="保持登录"
                checked={remember}
                onChange={(e) => setRemember(e.currentTarget.checked)}
                disabled={busy}
              />
              <Button
                type="submit"
                size="sm"
                fullWidth
                loading={busy || loading}
                mt="sm"
              >
                登录并获取设备 <IconChevronRight size={16} />
              </Button>
            </Stack>
          </form>
        </div>
        <div className="login-activity">
          <LatestMessage entry={latestMessage} />
        </div>
      </main>
    );
  return (
    <main className="desktop-shell">
      <header className="topbar">
        <Group gap={9} wrap="nowrap">
          <WireGuardLogo size={26} />
          <Box>
            <Text fw={650} fz={13.5} lh={1.2}>
              WireGuard Manager
            </Text>
            <Text className="brand-subtitle" c="dimmed">
              CONTROL PLANE
            </Text>
          </Box>
          <span className="header-divider" />
          <Text size="xs" c="dimmed">
            远程访问
          </Text>
        </Group>
        <Group gap="sm" wrap="nowrap" className="server-address">
          <IconCloud size={15} />
          <Text size="xs" c="dimmed" truncate>
            {state.serverURL}
          </Text>
          <Badge color={color} variant="dot" size="sm">
            {states[status.state]}
          </Badge>
        </Group>
      </header>
      <aside className="master-pane">
        <div className="master-header">
          <TextInput
            aria-label="搜索设备"
            placeholder="搜索名称、IP 或局域网"
            leftSection={<IconSearch size={15} />}
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
            size="xs"
            className="device-search"
          />
          <Group justify="space-between" mt="sm">
            <Text size="xs" fw={600} c="#a9b1bd">
              远端网关{" "}
              <Text component="span" size="xs" c="#a9b1bd">
                {state.devices.length}
              </Text>
            </Text>
            <Tooltip label="刷新云端设备">
              <ActionIcon
                aria-label="刷新设备"
                variant="subtle"
                color="gray"
                disabled={busy}
                onClick={refresh}
              >
                <IconRefresh size={17} />
              </ActionIcon>
            </Tooltip>
          </Group>
        </div>
        <ScrollArea className="device-scroll" type="auto">
          <Stack gap={2} px={6}>
            {visible.map((d) => (
              <button
                key={d.id}
                className={`device-row ${selected === d.id ? "selected" : ""}`}
                disabled={busy}
                onClick={() => select(d)}
                aria-label={`选择设备 ${d.name}`}
                aria-pressed={selected === d.id}
              >
                <ThemeIcon
                  variant="light"
                  color={selected === d.id ? "wg" : "gray"}
                  size={28}
                >
                  <IconDeviceDesktop size={17} />
                </ThemeIcon>
                <div className="device-row-copy">
                  <Text fz={12.5} fw={600} truncate>
                    {d.name}
                  </Text>
                </div>
                {status.profileID === d.id ? (
                  <IconCircleFilled className="online-dot" size={8} />
                ) : (
                  <IconChevronRight size={14} color="#aab4bf" />
                )}
              </button>
            ))}
            {!visible.length && (
              <Text size="xs" c="#a9b1bd" ta="center" p="md">
                {state.devices.length
                  ? "没有匹配的设备"
                  : "暂无设备，请先在管理后台创建"}
              </Text>
            )}
          </Stack>
        </ScrollArea>
        <div className="master-footer">
          <Group wrap="nowrap" gap={8}>
            <Avatar color="wg" radius="50%" size={28}>
              {(state.user.name || state.user.email).slice(0, 1).toUpperCase()}
            </Avatar>
            <Box style={{ flex: 1, minWidth: 0 }}>
              <Text size="xs" fw={600} truncate>
                {state.user.name || "当前账号"}
              </Text>
              <Text fz={10.5} c="#a9b1bd" truncate>
                {state.user.email}
              </Text>
            </Box>
            <Tooltip label="退出登录并断开连接">
              <ActionIcon
                aria-label="退出登录"
                color="gray"
                variant="subtle"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    apply(await api().Logout());
                    setStatus(offline);
                    setHistory([]);
                  })
                }
              >
                <IconLogout size={18} />
              </ActionIcon>
            </Tooltip>
          </Group>
        </div>
      </aside>
      <section className="detail-pane">
        <ScrollArea className="detail-scroll" type="auto">
          <div className="detail-content">
            {device ? (
              <>
                <Group
                  justify="space-between"
                  align="center"
                  mb="md"
                  wrap="nowrap"
                  className="device-heading"
                >
                  <div>
                    <Title order={3}>{device.name}</Title>
                  </div>
                  {connectedHere ? (
                    <Button
                      color="red"
                      variant="light"
                      leftSection={<IconPlugConnectedX size={17} />}
                      disabled={busy}
                      onClick={disconnect}
                      size="xs"
                    >
                      断开连接
                    </Button>
                  ) : (
                    <Button
                      leftSection={<IconPlugConnected size={17} />}
                      loading={busy}
                      onClick={connect}
                      size="xs"
                    >
                      {status.profileID ? "切换访问网关" : "访问此网关"}
                    </Button>
                  )}
                </Group>
                <Tabs defaultValue="overview" keepMounted={false}>
                  <Tabs.List mb="sm">
                    <Tabs.Tab
                      value="overview"
                      leftSection={<IconInfoCircle size={14} />}
                    >
                      隧道详情
                    </Tabs.Tab>
                    <Tabs.Tab
                      value="network"
                      leftSection={<IconNetwork size={14} />}
                    >
                      网络设置
                    </Tabs.Tab>
                  </Tabs.List>
                  <div className="detail-grid">
                    <div className="configuration-column">
                      <Tabs.Panel value="overview">
                        <TunnelOverview device={device} status={status} />
                      </Tabs.Panel>
                      <Tabs.Panel value="network">
                        <Card className="settings-card">
                          <Group justify="space-between" mb="sm">
                            <Text fw={650} size="sm">云端转发</Text>
                            <Badge size="xs" variant="light">经所选网关</Badge>
                          </Group>
                          <div className="field-section">
                            <FieldLabel
                              id="gateway-targets"
                              label="转发目标 IP / 网段"
                              help="云端将这些地址交给所选网关转发，本机同时添加访问路由。网关首次运行接入脚本后，后续修改无需重新配置网关。单个 IP 自动转为 /32；与本机局域网重叠时请指定具体 IP。"
                            />
                            <AddressInput
                              id="gateway-targets"
                              placeholder="192.168.1.100 或 192.168.10.0/24"
                              value={lans}
                              onChange={setLANs}
                              search={lanSearch}
                              onSearchChange={setLANSearch}
                              disabled={locked}
                            />
                          </div>
                          <Group mt="sm" pt="sm" justify="space-between" className="settings-footer">
                            <Text size="xs" c="dimmed">保存后立即在云端生效</Text>
                            <Button variant="light" size="xs" disabled={locked} onClick={save}>保存转发目标</Button>
                          </Group>
                          <Divider my="sm" />
                          <Group justify="space-between" gap="xs">
                            <FieldLabel
                              id="gateway-setup"
                              label="网关首次接入"
                              help="在 OpenWrt 或 Linux 网关以 root 执行下载的脚本，完成 WireGuard、转发、回程 NAT 和开机启动。须预装 WireGuard 工具和防火墙组件。脚本含该网关密钥，请妥善保管。"
                            />
                            <Button aria-label="下载接入脚本" id="gateway-setup" size="compact-xs" variant="subtle" leftSection={<IconDownload size={14} />} disabled={busy} onClick={downloadSetup}>下载接入脚本</Button>
                          </Group>
                        </Card>
                      </Tabs.Panel>
                    </div>
                    <Stack gap="sm" className="status-column">
                      <Card>
                        <Group justify="space-between" mb="sm">
                          <Text size="sm" fw={650}>
                            当前连接
                          </Text>
                          {busy ? (
                            <Loader size="xs" />
                          ) : (
                            <Badge variant="dot" color={color} size="sm">
                              {states[status.state]}
                            </Badge>
                          )}
                        </Group>
                        <Text fw={600} size="sm" truncate>
                          {active?.name || "尚未访问网关"}
                        </Text>
                        <Text size="xs" c="dimmed" mt={5}>
                          {status.handshake
                            ? `最近握手 ${new Date(status.handshake).toLocaleTimeString()}`
                            : status.profileID
                              ? "尚未完成握手，隧道未连通"
                              : "尚未握手"}
                        </Text>
                        {status.profileID && (
                          <>
                            <Divider my="sm" />
                            <Text size="xs" c="dimmed">本机独立访问隧道</Text>
                            {!connectedHere && (
                              <Button
                                fullWidth
                                variant="subtle"
                                size="xs"
                                color="red"
                                mt="md"
                                onClick={disconnect}
                                disabled={busy}
                              >
                                断开 {active?.name}
                              </Button>
                            )}
                          </>
                        )}
                      </Card>
                      <Card>
                        <Group justify="space-between">
                          <Text size="sm" fw={650}>
                            隧道延迟
                          </Text>
                          <Text fw={650} size="lg" className="metric-value">
                            {status.latencyMS < 0
                              ? "—"
                              : status.latencyMS.toFixed(0)}{" "}
                            <Text component="span" size="xs" c="dimmed">
                              ms
                            </Text>
                          </Text>
                        </Group>
                        <Sparkline
                          data={history.map((h) => h.latencyMS)}
                          color="var(--mantine-color-teal-8)"
                          label="隧道延迟趋势"
                        />
                      </Card>
                      <Card>
                        <Text size="sm" fw={650} mb="xs">
                          流量信息
                        </Text>
                        <div className="traffic-grid">
                          <div>
                            <Group gap={4} c="#3984ce">
                              <IconArrowDown size={14} />
                              <Text size="xs">下行</Text>
                            </Group>
                            <Text size="sm" fw={650} className="metric-value">
                              {formatBytes(status.rxBps)}/s
                            </Text>
                            <Text size="xs" c="dimmed">
                              累计 {formatBytes(status.rxBytes)}
                            </Text>
                            <Sparkline
                              data={history.map((h) => h.rxBps)}
                              color="#3984ce"
                              label="下载吞吐趋势"
                            />
                          </div>
                          <div>
                            <Group gap={4} c="#8565cf">
                              <IconArrowUp size={14} />
                              <Text size="xs">上行</Text>
                            </Group>
                            <Text size="sm" fw={650} className="metric-value">
                              {formatBytes(status.txBps)}/s
                            </Text>
                            <Text size="xs" c="dimmed">
                              累计 {formatBytes(status.txBytes)}
                            </Text>
                            <Sparkline
                              data={history.map((h) => h.txBps)}
                              color="#8565cf"
                              label="上传吞吐趋势"
                            />
                          </div>
                        </div>
                      </Card>
                    </Stack>
                  </div>
                </Tabs>
              </>
            ) : (
              <Paper withBorder p="xl" ta="center">
                <ThemeIcon size={54} variant="light" color="gray">
                  <IconDeviceDesktop size={32} />
                </ThemeIcon>
                <Title order={3} mt="lg">
                  选择一台设备
                </Title>
                <Text size="sm" c="dimmed" mt="sm">
                  从左侧选择要访问的远端网关，或在管理后台创建网关后刷新。
                </Text>
              </Paper>
            )}
          </div>
        </ScrollArea>
        <footer className="detail-footer">
          <Group justify="space-between" wrap="nowrap" gap="xs">
            <LatestMessage entry={latestMessage} />
            <Button
              size="compact-xs"
              variant="subtle"
              color="gray"
              disabled={busy}
              onClick={() => {
                setBusy(true);
                void api()
                  .Quit()
                  .catch((e) => {
                    setBusy(false);
                    setError(String(e));
                  });
              }}
            >
              退出程序
            </Button>
          </Group>
        </footer>
      </section>
    </main>
  );
}
