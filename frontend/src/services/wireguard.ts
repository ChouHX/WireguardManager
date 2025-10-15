import api from '@/lib/api';
import { ApiResponse } from '@/types/auth';
import {
  UserTrafficStats,
  UserTrafficSummary,
  AdminUserTraffic,
  WireguardPeer,
  AddPeerRequest,
  UpdatePeerRequest
} from '@/types/wireguard';

export class WireguardService {
  // User endpoints
  
  // Get my traffic summary (for polling)
  static async getMyTraffic(): Promise<ApiResponse<UserTrafficSummary>> {
    const response = await api.get('/api/wireguard/traffic');
    return response.data;
  }

  // Get my peers
  static async getMyPeers(): Promise<ApiResponse<WireguardPeer[]>> {
    const response = await api.get('/api/wireguard/peers');
    return response.data;
  }

  // Add a peer
  static async addPeer(data: AddPeerRequest): Promise<ApiResponse<WireguardPeer>> {
    const response = await api.post('/api/wireguard/peers', data);
    return response.data;
  }

  // Update a peer
  static async updatePeer(peerId: number, data: UpdatePeerRequest): Promise<ApiResponse<WireguardPeer>> {
    const response = await api.patch(`/api/wireguard/peers/${peerId}`, data);
    return response.data;
  }

  // Delete a peer
  static async deletePeer(peerId: number): Promise<ApiResponse<null>> {
    const response = await api.delete(`/api/wireguard/peers/${peerId}`);
    return response.data;
  }

  // Get peer config (unified API for both download and QR code)
  static async getPeerConfig(peerId: number): Promise<ApiResponse<{ config: string }>> {
    const response = await api.get(`/api/wireguard/peers/${peerId}/config`);
    return response.data;
  }

  // Download peer config (uses getPeerConfig and triggers download)
  static async downloadPeerConfig(peerId: number, peerName?: string): Promise<void> {
    const response = await this.getPeerConfig(peerId);
    
    if (response.success && response.data) {
      // Create a blob from the config text
      const blob = new Blob([response.data.config], { type: 'text/plain' });
      const url = window.URL.createObjectURL(blob);
      
      // Create a temporary link and trigger download
      const link = document.createElement('a');
      link.href = url;
      link.download = peerName ? `wg-${peerName}.conf` : `wg-peer-${peerId}.conf`;
      document.body.appendChild(link);
      link.click();
      
      // Cleanup
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    }
  }

  // Admin endpoints
  
  // Get all users traffic statistics (admin, simplified)
  static async getAdminTraffic(): Promise<ApiResponse<AdminUserTraffic[]>> {
    const response = await api.get('/api/admin/wireguard/traffic');
    return response.data;
  }

  // Get specific user traffic statistics (admin, detailed)
  static async getUserTraffic(userId: number): Promise<ApiResponse<UserTrafficStats>> {
    const response = await api.get(`/api/admin/wireguard/traffic/${userId}`);
    return response.data;
  }

  // Delete user's WireGuard server (admin)
  static async deleteServer(serverId: number): Promise<ApiResponse<null>> {
    const response = await api.delete(`/api/admin/wireguard/servers/${serverId}`);
    return response.data;
  }

  // Toggle server enabled status (admin)
  static async toggleServer(serverId: number, enabled: boolean): Promise<ApiResponse<null>> {
    const response = await api.patch(`/api/admin/wireguard/servers/${serverId}/toggle`, { enabled });
    return response.data;
  }

  // Set rate limit (admin)
  static async setRateLimit(serverId: number, downloadRate: number, uploadRate: number): Promise<ApiResponse<null>> {
    const response = await api.patch(`/api/admin/wireguard/servers/${serverId}/ratelimit`, {
      download_rate: downloadRate,
      upload_rate: uploadRate
    });
    return response.data;
  }
}
