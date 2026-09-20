// WireGuard 相关类型定义（与后端 models 保持一致）

export interface WireguardServerInfo {
  id: number;
  user_id: number;
  namespace: string;
  wg_interface: string;
  wg_port: number;
  wg_public_key: string;
  wg_address: string;
  server_endpoint?: string;
  created_at: string;
}

export interface WireguardPeer {
  id: number;
  public_key: string;
  private_key: string;
  peer_address: string;
  allowed_ips: string;
  endpoint?: string;
  persistent_keepalive: number;
  comment?: string;
  enable_forwarding: boolean;
  forward_interface?: string;
  created_at: string;
}

export interface WireguardPeerStats {
  public_key: string;
  endpoint?: string;
  allowed_ips: string;
  latest_handshake?: string;
  transfer_rx: number;
  transfer_tx: number;
  persistent_keepalive: number;
  comment?: string;
}

export interface WireguardServerStats {
  interface: string;
  public_key: string;
  listen_port: number;
  peer_count: number;
  total_rx: number;
  total_tx: number;
  /**
   * peer 列表。后端正常情况下返回数组（空列表为 []），
   * 但在解析异常等边界情况下可能为 null，消费方需按空列表处理。
   */
  peers: WireguardPeerStats[] | null;
}

export interface UserTrafficStats {
  user_id: number;
  user_uid: string;
  email: string;
  /** 用户尚未分配 WireGuard server 时后端可能返回 null */
  server_info: WireguardServerInfo | null;
  /** 采集失败或未分配网络时后端可能返回 null */
  server_stats: WireguardServerStats | null;
}

export interface PeerTrafficSummary {
  public_key: string;
  latest_handshake?: string;
  transfer_rx: number;
  transfer_tx: number;
  comment?: string;
}

export interface UserTrafficSummary {
  peer_count: number;
  total_rx: number;
  total_tx: number;
  peers: PeerTrafficSummary[];
}

export interface AdminUserTraffic {
  server_id: number;
  user_id: number;
  user_uid: string;
  email: string;
  peer_count: number;
  total_rx: number;
  total_tx: number;
  wg_port: number;
  wg_address: string;
  namespace: string;
  enabled: boolean;
  /** Mbps，0 表示不限速 */
  download_rate: number;
  /** Mbps，0 表示不限速 */
  upload_rate: number;
}

// 可用网络接口（用于转发出口选择）
export interface NetworkInterfaceInfo {
  name: string;
  addresses: string[];
  is_up: boolean;
  is_loopback: boolean;
  /** 是否系统默认路由的出口接口 */
  is_default: boolean;
  /** 隧道/容器/网桥等虚拟接口，一般不建议作为转发出口 */
  is_virtual: boolean;
}

export interface NetworkInterfacesResponse {
  /** 探测到的默认出口接口；探测失败时回退为配置的 out_interface */
  default: string;
  /** 是否来自系统默认路由探测 */
  detected: boolean;
  interfaces: NetworkInterfaceInfo[];
}

// 设备实时在线状态（服务端 TCP 探测）
export type LivenessState = 'unknown' | 'online' | 'offline';

export interface LivenessResult {
  state: LivenessState;
  /** 最近一次 WireGuard 握手时间 */
  last_handshake_at?: string;
  /** 距最近一次握手的秒数；从未握手为 -1 */
  handshake_age_seconds: number;
  last_online_at?: string;
  checked_at: string;
  /** 当前连续判定为握手过期的次数 */
  failures: number;
  checks: number;
}

export interface LivenessResponse {
  enabled: boolean;
  online: number;
  total: number;
  /** 判定在线所用的握手时效阈值（秒） */
  handshake_timeout_seconds: number;
  /** key 为 peer 公钥 */
  peers: Record<string, LivenessResult>;
}

export interface ServerLivenessAggregate {
  online: number;
  offline: number;
  unknown: number;
  total: number;
}

export interface AdminLivenessResponse {
  enabled: boolean;
  summary: { online: number; offline: number; unknown: number };
  /** key 为 server_id 的字符串形式 */
  servers: Record<string, ServerLivenessAggregate>;
}

export interface AddPeerRequest {  allowed_ips?: string;
  persistent_keepalive?: number;
  comment?: string;
  enable_forwarding?: boolean;
  forward_interface?: string;
}

export interface UpdatePeerRequest {
  allowed_ips?: string;
  persistent_keepalive?: number;
  comment?: string;
  enable_forwarding?: boolean;
  forward_interface?: string;
}
