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

export interface AddPeerRequest {
  allowed_ips?: string;
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
