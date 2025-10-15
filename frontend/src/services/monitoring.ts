import api from '@/lib/api';
import { ApiResponse } from '@/types/auth';
import {
  SystemStats,
  CPUStats,
  MemoryStats,
  DiskStats,
  NetworkStats,
  MonitoringChartResponse,
  MonitoringHistoryResponse,
  MonitoringStatsResponse
} from '@/types/monitoring';

export class MonitoringService {
  // 获取系统整体统计信息
  static async getSystemStats(): Promise<ApiResponse<SystemStats>> {
    const response = await api.get('/api/admin/monitoring/system');
    return response.data;
  }

  // 获取CPU统计信息
  static async getCPUStats(): Promise<ApiResponse<CPUStats>> {
    const response = await api.get('/api/admin/monitoring/cpu');
    return response.data;
  }

  // 获取内存统计信息
  static async getMemoryStats(): Promise<ApiResponse<MemoryStats>> {
    const response = await api.get('/api/admin/monitoring/memory');
    return response.data;
  }

  // 获取磁盘统计信息
  static async getDiskStats(): Promise<ApiResponse<DiskStats>> {
    const response = await api.get('/api/admin/monitoring/disk');
    return response.data;
  }

  // 获取网络统计信息
  static async getNetworkStats(): Promise<ApiResponse<NetworkStats>> {
    const response = await api.get('/api/admin/monitoring/network');
    return response.data;
  }

  // 获取图表数据（简化版，仅返回图表所需字段）
  static async getMonitoringChart(hours?: number): Promise<ApiResponse<MonitoringChartResponse>> {
    const response = await api.get('/api/admin/monitoring/chart', {
      params: { hours }
    });
    return response.data;
  }

  // 获取历史监控记录（完整版）
  static async getMonitoringHistory(params?: {
    limit?: number;
    since?: number;
  }): Promise<ApiResponse<MonitoringHistoryResponse>> {
    const response = await api.get('/api/admin/monitoring/history', { params });
    return response.data;
  }

  // 获取聚合统计数据（完整版）
  static async getMonitoringStats(hours?: number): Promise<ApiResponse<MonitoringStatsResponse>> {
    const response = await api.get('/api/admin/monitoring/stats', {
      params: { hours }
    });
    return response.data;
  }
}
