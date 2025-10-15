import axios, { AxiosInstance, AxiosError } from 'axios';
import Cookies from 'js-cookie';
import { ApiResponse } from '@/types/auth';

// 获取 API Base URL
// 开发环境：使用 Next.js rewrites 代理到后端（需要在 next.config.mjs 中配置）
// 生产环境：使用相对路径（通过 Nginx 代理）
const getBaseURL = () => {
  // 如果明确设置了 API URL，使用它
  if (process.env.NEXT_PUBLIC_API_URL) {
    return process.env.NEXT_PUBLIC_API_URL;
  }
  
  // 开发环境：使用空字符串，让 Next.js rewrites 处理
  // 生产环境：使用空字符串，让 Nginx 处理
  return '';
};

// 创建 axios 实例
const api: AxiosInstance = axios.create({
  baseURL: getBaseURL(),
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// 请求拦截器 - 添加认证token
api.interceptors.request.use(
  (config) => {
    const token = Cookies.get('auth_token');
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// 响应拦截器 - 统一处理响应和错误
api.interceptors.response.use(
  (response) => {
    return response;
  },
  (error: AxiosError<ApiResponse>) => {
    // 如果是401错误，清除token并跳转到登录页
    if (error.response?.status === 401) {
      Cookies.remove('auth_token');
      // 如果不在登录页面，则跳转到登录页
      if (typeof window !== 'undefined' && !window.location.pathname.includes('/auth')) {
        window.location.href = '/auth/login';
      }
    }
    
    // 返回错误信息
    const errorMessage = error.response?.data?.error?.message || 
                        error.response?.data?.message || 
                        error.message || 
                        'An unexpected error occurred';
    
    return Promise.reject({
      message: errorMessage,
      code: error.response?.data?.error?.code || 'UNKNOWN_ERROR',
      status: error.response?.status || 500,
      details: error.response?.data?.error?.details,
    });
  }
);

export default api;
