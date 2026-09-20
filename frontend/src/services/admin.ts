import api from './api';
import type { ApiResponse, UpdateUserRequest, User } from '@/types/auth';

export const adminService = {
  async getUsers(): Promise<ApiResponse<User[]>> {
    const res = await api.get<ApiResponse<User[]>>('/api/admin/users');
    return res.data;
  },

  async deleteUser(userId: number): Promise<ApiResponse<null>> {
    const res = await api.delete<ApiResponse<null>>(`/api/admin/users/${userId}`);
    return res.data;
  },

  async updateUser(userId: number, data: UpdateUserRequest): Promise<ApiResponse<User>> {
    const res = await api.patch<ApiResponse<User>>(`/api/admin/users/${userId}`, data);
    return res.data;
  },
};
