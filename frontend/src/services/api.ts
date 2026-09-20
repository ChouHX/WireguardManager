import axios, { type AxiosError, type AxiosInstance } from 'axios';
import type { ApiResponse, AppError } from '@/types/auth';

const TOKEN_KEY = 'wm_auth_token';

/** token 读写（localStorage 单一来源，store 与 axios 共用） */
export const tokenStorage = {
  get(): string | null {
    try {
      return localStorage.getItem(TOKEN_KEY);
    } catch {
      return null;
    }
  },
  set(token: string): void {
    try {
      localStorage.setItem(TOKEN_KEY, token);
    } catch {
      /* 忽略隐私模式下的写入失败 */
    }
  },
  clear(): void {
    try {
      localStorage.removeItem(TOKEN_KEY);
    } catch {
      /* 忽略 */
    }
  },
};

/** 401 时的全局回调，由 App 注入（避免 api 层直接依赖 router） */
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler;
}

const baseURL = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? '';

const api: AxiosInstance = axios.create({
  baseURL,
  timeout: 20000,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = tokenStorage.get();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse>) => {
    const status = error.response?.status ?? 0;

    if (status === 401) {
      tokenStorage.clear();
      onUnauthorized?.();
    }

    const payload = error.response?.data;
    const normalized: AppError = {
      message:
        payload?.error?.message ||
        payload?.message ||
        error.message ||
        'An unexpected error occurred',
      code: payload?.error?.code || 'UNKNOWN_ERROR',
      status,
      details: payload?.error?.details,
    };

    return Promise.reject(normalized);
  },
);

export default api;
