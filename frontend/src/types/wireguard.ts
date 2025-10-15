// WireGuard Server Info
export interface WireguardServerInfo {
  id: number;
  namespace: string;
  wg_interface: string;
  wg_port: number;
  wg_public_key: string;
  wg_address: string;
  server_endpoint: string;
  created_at: string;
}

// WireGuard Peer
export interface WireguardPeer {
  id: number;
  server_id: number;
  public_key: string;
  private_key: string;
  peer_address: string; // peer在WireGuard网段中的IP地址
  allowed_ips: string; // peer可以访问的IP地址或网段
  endpoint?: string;
  persistent_keepalive: number;
  comment?: string;
  enable_forwarding: boolean;
  forward_interface?: string;
  created_at: string;
  updated_at: string;
}

// WireGuard Peer Stats (from server stats)
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

// WireGuard Server Stats
export interface WireguardServerStats {
  interface: string;
  public_key: string;
  listen_port: number;
  peer_count: number;
  total_rx: number;
  total_tx: number;
  peers: WireguardPeerStats[];
}

// User Traffic Stats
export interface UserTrafficStats {
  user_id: number;
  user_uid: string;
  email: string;
  server_info: WireguardServerInfo;
  server_stats: WireguardServerStats;
}

// User Traffic Summary (for polling)
export interface UserTrafficSummary {
  peer_count: number;
  total_rx: number;
  total_tx: number;
  peers: PeerTrafficSummary[];
}

export interface PeerTrafficSummary {
  public_key: string;
  latest_handshake?: string;
  transfer_rx: number;
  transfer_tx: number;
  comment?: string;
}

// Admin User Traffic (simplified for admin view)
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
  download_rate: number; // Mbps
  upload_rate: number;   // Mbps
}

// Add Peer Request
export interface AddPeerRequest {
  allowed_ips?: string; // peer可以访问的IP地址或网段，留空则默认为peer自己的IP
  persistent_keepalive?: number;
  comment?: string;
  enable_forwarding?: boolean;
  forward_interface?: string;
}

// Update Peer Request
export interface UpdatePeerRequest {
  allowed_ips?: string;
  persistent_keepalive?: number;
  comment?: string;
  enable_forwarding?: boolean;
  forward_interface?: string;
}
