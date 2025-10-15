// API 响应的基础类型
export interface ApiResponse<T = any> {
  success: boolean;
  message: string;
  data?: T;
  error?: {
    code: string;
    message: string;
    details?: any;
  };
  timestamp: number;
  request_id?: string;
}

// 用户角色枚举
export enum UserRole {
  ADMIN = "admin",
  NORMAL_USER = "user",
}

// 用户信息
export interface User {
  id: number;
  email: string;
  name: string;
  role: UserRole;
  company?: {
    id: number;
    name: string;
  } | null;
  created_at: string;
}


// 认证相关的请求/响应类型
export interface LoginRequest {
  email: string;
  password: string;
}

export interface RegisterRequest {
  email: string;
  password: string;
  name: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface UpdateProfileRequest {
  name?: string;
  password?: string;
}


export interface UpdateUserRequest {
  name?: string;
  email?: string;
  password?: string;
  role?: UserRole;
}

// 认证状态
export interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
}

// 分页信息
export interface PaginationInfo {
  current_page: number;
  per_page: number;
  total_pages: number;
  total_items: number;
  has_next: boolean;
  has_prev: boolean;
}

// 分页响应
export interface PaginatedResponse<T> {
  items: T[];
  pagination: PaginationInfo;
}

