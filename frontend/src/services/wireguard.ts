import api from './api';
import type { ApiResponse } from '@/types/auth';
import type {
  AddPeerRequest,
  AdminLivenessResponse,
  AdminUserTraffic,
  LivenessResponse,
  NetworkInterfacesResponse,
  UpdatePeerRequest,
  UserTrafficStats,
  UserTrafficSummary,
  WireguardPeer,
} from '@/types/wireguard';

export const wireguardService = {
  // -------- 普通用户 --------

  /** 当前用户流量摘要（轮询接口，后端已做短路缓存） */
  async getMyTraffic(): Promise<ApiResponse<UserTrafficSummary>> {
    const res = await api.get<ApiResponse<UserTrafficSummary>>('/api/wireguard/traffic');
    return res.data;
  },

  async getMyPeers(): Promise<ApiResponse<WireguardPeer[]>> {
    const res = await api.get<ApiResponse<WireguardPeer[]>>('/api/wireguard/peers');
    return res.data;
  },

  /** 可作为转发出口的网络接口列表（含探测到的默认出口） */
  async getInterfaces(): Promise<ApiResponse<NetworkInterfacesResponse>> {
    const res = await api.get<ApiResponse<NetworkInterfacesResponse>>('/api/wireguard/interfaces');
    return res.data;
  },

  /** 当前用户设备的实时在线状态（服务端探测结果快照） */
  async getLiveness(): Promise<ApiResponse<LivenessResponse>> {
    const res = await api.get<ApiResponse<LivenessResponse>>('/api/wireguard/liveness');
    return res.data;
  },

  async addPeer(data: AddPeerRequest): Promise<ApiResponse<WireguardPeer>> {
    const res = await api.post<ApiResponse<WireguardPeer>>('/api/wireguard/peers', data);
    return res.data;
  },

  async updatePeer(peerId: number, data: UpdatePeerRequest): Promise<ApiResponse<WireguardPeer>> {
    const res = await api.patch<ApiResponse<WireguardPeer>>(`/api/wireguard/peers/${peerId}`, data);
    return res.data;
  },

  async deletePeer(peerId: number): Promise<ApiResponse<null>> {
    const res = await api.delete<ApiResponse<null>>(`/api/wireguard/peers/${peerId}`);
    return res.data;
  },

  /** 获取 peer 的客户端配置文本（下载与二维码共用） */
  async getPeerConfig(peerId: number): Promise<ApiResponse<{ config: string }>> {
    const res = await api.get<ApiResponse<{ config: string }>>(
      `/api/wireguard/peers/${peerId}/config`,
    );
    return res.data;
  },

  /** 触发浏览器下载 .conf 文件 */
  async downloadPeerConfig(peerId: number, peerName?: string): Promise<void> {
    const response = await this.getPeerConfig(peerId);
    if (!response.success || !response.data) return;

    const blob = new Blob([response.data.config], { type: 'text/plain;charset=utf-8' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = peerName ? `wg-${peerName}.conf` : `wg-peer-${peerId}.conf`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.URL.revokeObjectURL(url);
  },

  // -------- 管理员 --------

  async getAdminTraffic(): Promise<ApiResponse<AdminUserTraffic[]>> {
    const res = await api.get<ApiResponse<AdminUserTraffic[]>>('/api/admin/wireguard/traffic');
    return res.data;
  },

  async getUserTraffic(userId: number): Promise<ApiResponse<UserTrafficStats>> {
    const res = await api.get<ApiResponse<UserTrafficStats>>(
      `/api/admin/wireguard/traffic/${userId}`,
    );
    return res.data;
  },

  /** 管理端：各服务器在线设备统计 */
  async getAdminLiveness(): Promise<ApiResponse<AdminLivenessResponse>> {
    const res = await api.get<ApiResponse<AdminLivenessResponse>>('/api/admin/wireguard/liveness');
    return res.data;
  },

  async deleteServer(serverId: number): Promise<ApiResponse<null>> {
    const res = await api.delete<ApiResponse<null>>(`/api/admin/wireguard/servers/${serverId}`);
    return res.data;
  },

  async toggleServer(serverId: number, enabled: boolean): Promise<ApiResponse<null>> {
    const res = await api.patch<ApiResponse<null>>(
      `/api/admin/wireguard/servers/${serverId}/toggle`,
      { enabled },
    );
    return res.data;
  },

  async setRateLimit(
    serverId: number,
    downloadRate: number,
    uploadRate: number,
  ): Promise<ApiResponse<null>> {
    const res = await api.patch<ApiResponse<null>>(
      `/api/admin/wireguard/servers/${serverId}/ratelimit`,
      { download_rate: downloadRate, upload_rate: uploadRate },
    );
    return res.data;
  },
};
