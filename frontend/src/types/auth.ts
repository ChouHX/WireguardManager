// 认证与通用 API 类型定义

/** 后端统一响应包裹结构 */
export interface ApiResponse<T = unknown> {
  success: boolean;
  message: string;
  data?: T;
  error?: {
    code: string;
    message: string;
    details?: unknown;
  };
  timestamp: number;
  request_id?: string;
}

/** 用户角色（与后端 models.UserRole 保持一致） */
export type UserRole = 'admin' | 'normal_user';

export const USER_ROLE = {
  ADMIN: 'admin',
  NORMAL_USER: 'normal_user',
} as const satisfies Record<string, UserRole>;

export interface User {
  id: number;
  user_uid: string;
  email: string;
  name: string;
  role: UserRole;
  created_at: string;
}

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
  /** 修改密码时必须同时提交当前密码（服务端会校验） */
  current_password?: string;
}

export interface UpdateUserRequest {
  name?: string;
  email?: string;
  password?: string;
  role?: UserRole;
}

export interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
}

export interface PaginationInfo {
  current_page: number;
  per_page: number;
  total_pages: number;
  total_items: number;
  has_next: boolean;
  has_prev: boolean;
}

export interface PaginatedResponse<T> {
  items: T[];
  pagination: PaginationInfo;
}

/** 归一化后的接口错误对象（由 api.ts 拦截器抛出） */
export interface AppError {
  message: string;
  code: string;
  status: number;
  details?: unknown;
}

export function isAppError(err: unknown): err is AppError {
  return typeof err === 'object' && err !== null && 'message' in err && 'code' in err;
}

/** 从任意错误中提取可展示的消息 */
export function errorMessage(err: unknown, fallback: string): string {
  if (isAppError(err) && err.message) return err.message;
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}
