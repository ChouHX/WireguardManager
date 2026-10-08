import { useEffect, useRef, useState } from "react";
import { api, type Profile, type Adapter, type Status } from "./api";
const initial: Status = {
  profileID: "",
  state: "disconnected",
  rxBytes: 0,
  txBytes: 0,
  rxBps: 0,
  txBps: 0,
  latencyMS: -1,
  handshake: "",
  error: "",
};
const labels: Record<string, string> = {
  disconnected: "未连接",
  handshaking: "等待握手",
  connected: "已连接",
  stale: "握手已过期",
  error: "连接异常",
};
const bytes = (n: number) =>
  n < 1024
    ? `${n.toFixed(0)} B`
    : n < 1048576
      ? `${(n / 1024).toFixed(1)} KB`
      : `${(n / 1048576).toFixed(1)} MB`;
function Chart({
  data,
  unit,
  color,
}: {
  data: number[];
  unit: string;
  color: string;
}) {
  const max = Math.max(...data.filter((n) => n >= 0), 1);
  const segments: string[] = [];
  let current: string[] = [];
  data.forEach((n, i) => {
    if (n < 0) {
      if (current.length) segments.push(current.join(" "));
      current = [];
    } else current.push(`${(i * 100) / 59},${36 - (n / max) * 32}`);
  });
  if (current.length) segments.push(current.join(" "));
  return (
    <div className="chart">
      <span>
        {max.toFixed(1)} {unit}
      </span>
      <svg
        viewBox="0 0 100 40"
        preserveAspectRatio="none"
        role="img"
        aria-label={`最近 60 次采样，单位 ${unit}`}
      >
        <path
          d="M0 36 H100 M0 20 H100 M0 4 H100"
          stroke="#243448"
          strokeWidth=".25"
        />
        {segments.map((s, i) => (
          <polyline
            key={i}
            points={s}
            fill="none"
            stroke={color}
            strokeWidth=".7"
          />
        ))}
      </svg>
      <small>最近 60 次采样</small>
    </div>
  );
}
export default function App() {
  const [profiles, setProfiles] = useState<Profile[]>([]),
    [adapters, setAdapters] = useState<Adapter[]>([]),
    [selected, setSelected] = useState(""),
    [draft, setDraft] = useState<Profile | null>(null);
  const [status, setStatus] = useState(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [history, setHistory] = useState<Status[]>([]);
  const activeRef = useRef("");
  const profile = profiles.find((p) => p.id === selected);
  const active = !!status.profileID;
  const locked = busy || selected === status.profileID;
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
      setStatus(await api().Status());
    } catch (e) {
      setError(String(e));
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
          if (activeRef.current !== s.profileID) {
            activeRef.current = s.profileID;
            setHistory([]);
          }
          if (s.profileID) setHistory((h) => [...h, s].slice(-60));
        }
      } catch (e) {
        if (!disposed) setError(String(e));
      } finally {
        if (!disposed) timer = setTimeout(poll, 1000);
      }
    };
    Promise.resolve()
      .then(() => Promise.all([api().Profiles(), api().Adapters()]))
      .then(([p, a]) => {
        if (!disposed) {
          setProfiles(p);
          setAdapters(a);
          if (p.length) setSelected(p[0].id);
        }
      })
      .catch((e) => {
        if (!disposed) setError(String(e));
      });
    void poll();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, []);
  useEffect(() => {
    setDraft(profiles.find((p) => p.id === selected) ?? null);
  }, [profiles, selected]);
  const save = async () => {
    if (!draft) return;
    const p = await api().SaveProfile(
      draft.id,
      draft.name,
      draft.targets,
      draft.adapterID,
    );
    setProfiles((ps) => ps.map((x) => (x.id === p.id ? p : x)));
    setNotice("已保存到本机");
  };
  const changeSite = (id: string) =>
    void run(async () => {
      if (active) await api().Disconnect();
      setSelected(id);
      setHistory([]);
    });
  const connect = () =>
    void run(async () => {
      await save();
      await api().Connect(selected);
      setNotice("已启动现场连接");
    });
  const imported = () =>
    void run(async () => {
      const p = await api().ImportConfig();
      setProfiles(p);
      if (p.length && !selected) setSelected(p[0].id);
    });
  return (
    <main>
      <header>
        <div className="brand">
          <span className="logo">W</span>
          <div>
            <strong>WireGuard Manager</strong>
            <small>现场连接 · Windows</small>
          </div>
        </div>
        <button className="secondary" onClick={imported} disabled={busy}>
          ＋ 导入配置
        </button>
      </header>
      <section className="intro">
        <span className="eyebrow">WORKSPACE / 安全远程访问</span>
        <h1>连接你的现场</h1>
        <p>选择客户，在本机填写目标地址，即可访问现场设备。</p>
      </section>
      {error && (
        <div role="alert" className="alert error">
          {error}
        </div>
      )}
      {status.error && (
        <div role="alert" className="alert error">
          {status.error}
        </div>
      )}
      {notice && (
        <div role="status" className="alert success">
          {notice}
        </div>
      )}
      <div className="layout">
        <section className="panel setup">
          <div className="section-title">
            <h2>客户 / 现场</h2>
            <span className="tag">{profiles.length} 个配置</span>
          </div>
          <label>
            当前现场
            <select
              value={selected}
              disabled={busy || profiles.length === 0}
              onChange={(e) => changeSite(e.target.value)}
            >
              <option value="" disabled>
                导入平台下载的 .conf 配置
              </option>
              {profiles.map((p) => (
                <option value={p.id} key={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          {draft ? (
            <>
              <div className="endpoint">
                <span>云端入口</span>
                <code>{profile?.endpoint}</code>
                <span>隧道地址</span>
                <code>{profile?.address}</code>
              </div>
              <label>
                现场名称
                <input
                  value={draft.name}
                  disabled={locked}
                  maxLength={80}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                />
              </label>
              <label>
                访问目标 IP / 网段
                <textarea
                  value={draft.targets}
                  disabled={locked}
                  rows={3}
                  placeholder={"192.168.0.100\n192.168.2.0/24"}
                  onChange={(e) =>
                    setDraft({ ...draft, targets: e.target.value })
                  }
                />
                <small>
                  只影响本机。单个 IP 自动转为 /32；换行或逗号分隔。
                </small>
              </label>
              <label>
                本机局域网网卡
                <div className="adapter">
                  <select
                    value={draft.adapterID}
                    disabled={locked}
                    onChange={(e) =>
                      setDraft({ ...draft, adapterID: e.target.value })
                    }
                  >
                    <option value="">
                      {draft.deviceLANs
                        ? "请选择现场局域网网卡"
                        : "仅访问远端（没有本机局域网）"}
                    </option>
                    {adapters.map((a) => (
                      <option value={a.id} key={a.id}>
                        {a.name} · {a.addresses.join(", ")}
                      </option>
                    ))}
                  </select>
                  <button
                    className="secondary"
                    title="刷新网卡"
                    aria-label="刷新网卡"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => setAdapters(await api().Adapters()))
                    }
                  >
                    ↻
                  </button>
                </div>
                <small>
                  声明局域网的设备需要选择网卡，连接时自动启用转发。
                </small>
              </label>
              {draft.deviceLANs && (
                <div className="lan-note">
                  <strong>设备局域网 · {draft.deviceLANs}</strong>
                  <p>
                    现场路由器或设备需将 {draft.tunnelNetwork}{" "}
                    的返回路由指向此网卡的局域网
                    IP。客户端启用转发，不自动修改现场路由或配置 NAT。
                  </p>
                </div>
              )}
              <div className="actions">
                <button
                  className="secondary"
                  disabled={locked}
                  onClick={() => void run(save)}
                >
                  保存设置
                </button>
                <button
                  className="text-button"
                  disabled={locked}
                  onClick={() =>
                    void run(async () => {
                      await api().RemoveProfile(selected);
                      const next = profiles.filter((p) => p.id !== selected);
                      setProfiles(next);
                      setSelected(next[0]?.id ?? "");
                    })
                  }
                >
                  移除配置
                </button>
              </div>
            </>
          ) : (
            <div className="empty">
              <span>↗</span>
              <h3>添加第一个现场</h3>
              <p>
                从管理平台下载本设备的 WireGuard
                配置，点击右上角导入。每台设备使用独立配置。
              </p>
            </div>
          )}
        </section>
        <aside>
          <section className="panel connection">
            <div className="section-title">
              <h2>连接状态</h2>
              <span className={`dot ${status.state}`} />
            </div>
            <div className="state">
              {busy ? "正在处理…" : (labels[status.state] ?? status.state)}
            </div>
            <p>
              {active
                ? profiles.find((p) => p.id === status.profileID)?.name
                : "每次连接一个现场"}
            </p>
            <button
              className={active ? "disconnect" : "primary"}
              disabled={busy || !draft}
              onClick={() =>
                active
                  ? void run(async () => {
                      await api().Disconnect();
                      setHistory([]);
                    })
                  : connect()
              }
            >
              {active ? "断开连接" : "连接现场"}
            </button>
            <small>
              {status.handshake
                ? `最近握手 ${new Date(status.handshake).toLocaleTimeString()}`
                : "连接成功以云端握手为准"}
            </small>
          </section>
          <section className="panel telemetry">
            <div className="section-title">
              <h2>隧道延迟</h2>
              <strong>
                {status.latencyMS >= 0
                  ? `${status.latencyMS.toFixed(0)} ms`
                  : "—"}
              </strong>
            </div>
            <Chart
              data={history.map((h) => h.latencyMS)}
              unit="ms"
              color="#52d4ba"
            />
            <small>到云端的 ICMP 往返时间；无响应时显示 —</small>
          </section>
          <section className="panel telemetry">
            <div className="section-title">
              <h2>实时吞吐</h2>
              <span className="tag">KB/s</span>
            </div>
            <div className="rates">
              <span>↓ {(status.rxBps / 1024).toFixed(1)}</span>
              <span>↑ {(status.txBps / 1024).toFixed(1)}</span>
            </div>
            <Chart
              data={history.map((h) => h.rxBps / 1024)}
              unit="KB/s ↓"
              color="#69adff"
            />
            <Chart
              data={history.map((h) => h.txBps / 1024)}
              unit="KB/s ↑"
              color="#c4a1ff"
            />
            <div className="totals">
              <span>接收 {bytes(status.rxBytes)}</span>
              <span>发送 {bytes(status.txBytes)}</span>
            </div>
          </section>
        </aside>
      </div>
      <footer>
        <span>配置使用 Windows 账户加密保存在本机</span>
        <span>切换现场会断开旧连接 · 关闭窗口会断开</span>
      </footer>
    </main>
  );
}
