import api from './api';
import type {
  ApiResponse,
  LoginRequest,
  LoginResponse,
  RegisterRequest,
  UpdateProfileRequest,
  User,
} from '@/types/auth';

export const authService = {
  async login(data: LoginRequest): Promise<ApiResponse<LoginResponse>> {
    const res = await api.post<ApiResponse<LoginResponse>>('/api/login', data);
    return res.data;
  },

  async register(data: RegisterRequest): Promise<ApiResponse<User>> {
    const res = await api.post<ApiResponse<User>>('/api/register', data);
    return res.data;
  },

  async getMe(): Promise<ApiResponse<User>> {
    const res = await api.get<ApiResponse<User>>('/api/me');
    return res.data;
  },

  async updateProfile(data: UpdateProfileRequest): Promise<ApiResponse<User>> {
    const res = await api.patch<ApiResponse<User>>('/api/me', data);
    return res.data;
  },

  async healthCheck(): Promise<ApiResponse<unknown>> {
    const res = await api.get<ApiResponse<unknown>>('/health');
    return res.data;
  },
};
