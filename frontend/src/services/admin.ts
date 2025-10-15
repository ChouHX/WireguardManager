import api from '@/lib/api';
import { 
  ApiResponse,
  User,
  UpdateUserRequest
} from '@/types/auth';

export class AdminService {
  // 获取所有用户
  static async getUsers(): Promise<ApiResponse<User[]>> {
    const response = await api.get('/api/admin/users');
    return response.data;
  }

  // 删除用户
  static async deleteUser(userId: number): Promise<ApiResponse<null>> {
    const response = await api.delete(`/api/admin/users/${userId}`);
    return response.data;
  }

  // 更新用户信息
  static async updateUser(userId: number, data: UpdateUserRequest): Promise<ApiResponse<User>> {
    const response = await api.patch(`/api/admin/users/${userId}`, data);
    return response.data;
  }
}

