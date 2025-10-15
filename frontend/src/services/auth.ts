import api from '@/lib/api';
import { 
  ApiResponse, 
  LoginRequest, 
  LoginResponse, 
  RegisterRequest, 
  UpdateProfileRequest,
  User 
} from '@/types/auth';

export class AuthService {
  // 用户登录
  static async login(data: LoginRequest): Promise<ApiResponse<LoginResponse>> {
    const response = await api.post('/api/login', data);
    return response.data;
  }

  // 用户注册
  static async register(data: RegisterRequest): Promise<ApiResponse<User>> {
    const response = await api.post('/api/register', data);
    return response.data;
  }

  // 获取当前用户信息
  static async getMe(): Promise<ApiResponse<User>> {
    const response = await api.get('/api/me');
    return response.data;
  }

  // 更新个人资料
  static async updateProfile(data: UpdateProfileRequest): Promise<ApiResponse<User>> {
    const response = await api.patch('/api/me', data);
    return response.data;
  }

  // 健康检查
  static async healthCheck(): Promise<any> {
    const response = await api.get('/health');
    return response.data;
  }
}

