import api from './api';
import type { ApiResponse } from '@/types/auth';
import type { SettingsResponse } from '@/types/settings';

export const settingsService = {
  /** 读取运行时配置（含可配置项定义） */
  async get(): Promise<ApiResponse<SettingsResponse>> {
    const res = await api.get<ApiResponse<SettingsResponse>>('/api/admin/settings');
    return res.data;
  },

  /** 批量更新运行时配置 */
  async update(
    values: Record<string, string>,
  ): Promise<ApiResponse<{ values: Record<string, string> }>> {
    const res = await api.patch<ApiResponse<{ values: Record<string, string> }>>(
      '/api/admin/settings',
      { values },
    );
    return res.data;
  },
};
