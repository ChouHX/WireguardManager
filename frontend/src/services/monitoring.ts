import api from './api';
import type { ApiResponse } from '@/types/auth';
import type {
  CPUStats,
  DiskStats,
  MemoryStats,
  MonitoringChartResponse,
  MonitoringHistoryResponse,
  MonitoringStatsResponse,
  NetworkStats,
  SystemStats,
} from '@/types/monitoring';

export const monitoringService = {
  /** 一次拿齐 CPU/内存/磁盘/网络/主机信息（后端非阻塞采样） */
  async getSystemStats(): Promise<ApiResponse<SystemStats>> {
    const res = await api.get<ApiResponse<SystemStats>>('/api/admin/monitoring/system');
    return res.data;
  },

  async getCPUStats(): Promise<ApiResponse<CPUStats>> {
    const res = await api.get<ApiResponse<CPUStats>>('/api/admin/monitoring/cpu');
    return res.data;
  },

  async getMemoryStats(): Promise<ApiResponse<MemoryStats>> {
    const res = await api.get<ApiResponse<MemoryStats>>('/api/admin/monitoring/memory');
    return res.data;
  },

  async getDiskStats(): Promise<ApiResponse<DiskStats>> {
    const res = await api.get<ApiResponse<DiskStats>>('/api/admin/monitoring/disk');
    return res.data;
  },

  async getNetworkStats(): Promise<ApiResponse<NetworkStats>> {
    const res = await api.get<ApiResponse<NetworkStats>>('/api/admin/monitoring/network');
    return res.data;
  },

  async getMonitoringChart(hours = 0.5): Promise<ApiResponse<MonitoringChartResponse>> {
    const res = await api.get<ApiResponse<MonitoringChartResponse>>(
      '/api/admin/monitoring/chart',
      { params: { hours } },
    );
    return res.data;
  },

  async getMonitoringHistory(params?: {
    limit?: number;
    since?: number;
  }): Promise<ApiResponse<MonitoringHistoryResponse>> {
    const res = await api.get<ApiResponse<MonitoringHistoryResponse>>(
      '/api/admin/monitoring/history',
      { params },
    );
    return res.data;
  },

  async getMonitoringStats(hours = 1): Promise<ApiResponse<MonitoringStatsResponse>> {
    const res = await api.get<ApiResponse<MonitoringStatsResponse>>(
      '/api/admin/monitoring/stats',
      { params: { hours } },
    );
    return res.data;
  },
};
