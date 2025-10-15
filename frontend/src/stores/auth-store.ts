import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import Cookies from 'js-cookie';
import { AuthState, User, LoginRequest, RegisterRequest } from '@/types/auth';
import { AuthService } from '@/services/auth';

interface AuthStore extends AuthState {
  // Actions
  login: (data: LoginRequest) => Promise<void>;
  register: (data: RegisterRequest) => Promise<void>;
  logout: () => void;
  loadUser: () => Promise<void>;
  updateUser: (user: User) => void;
  setLoading: (loading: boolean) => void;
}

export const useAuthStore = create<AuthStore>()(
  persist(
    (set, get) => ({
      // Initial state
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: false,

      // Actions
      login: async (data: LoginRequest) => {
        try {
          set({ isLoading: true });
          const response = await AuthService.login(data);
          
          if (response.success && response.data) {
            const { token, user } = response.data;
            
            // 保存 token 到 cookie
            Cookies.set('auth_token', token, { 
              expires: 1, // 1天过期
              secure: process.env.NODE_ENV === 'production',
              sameSite: 'strict'
            });
            
            set({
              user,
              token,
              isAuthenticated: true,
              isLoading: false,
            });
          }
        } catch (error: any) {
          set({ isLoading: false });
          throw error;
        }
      },

      register: async (data: RegisterRequest) => {
        try {
          set({ isLoading: true });
          const response = await AuthService.register(data);
          
          if (response.success && response.data) {
            // 注册成功后不自动登录，让用户手动登录
            set({ isLoading: false });
          }
        } catch (error: any) {
          set({ isLoading: false });
          throw error;
        }
      },

      logout: () => {
        // 清除 cookie 中的 token
        Cookies.remove('auth_token');
        
        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
        });
        
        // 跳转到登录页
        if (typeof window !== 'undefined') {
          window.location.href = '/auth/login';
        }
      },

      loadUser: async () => {
        try {
          const token = Cookies.get('auth_token');
          if (!token) {
            set({ 
              user: null,
              token: null,
              isAuthenticated: false,
              isLoading: false 
            });
            return;
          }

          set({ isLoading: true, token });
          const response = await AuthService.getMe();
          
          if (response.success && response.data) {
            set({
              user: response.data,
              token,
              isAuthenticated: true,
              isLoading: false,
            });
          }
        } catch (error: any) {
          console.error('Load user failed:', error);
          // 如果获取用户信息失败，清除认证状态
          Cookies.remove('auth_token');
          set({
            user: null,
            token: null,
            isAuthenticated: false,
            isLoading: false,
          });
        }
      },

      updateUser: (user: User) => {
        set({ user });
      },

      setLoading: (loading: boolean) => {
        set({ isLoading: loading });
      },
    }),
    {
      name: 'auth-store',
      // 持久化用户信息和认证状态，token 通过 cookie 管理
      partialize: (state) => ({
        user: state.user,
        isAuthenticated: state.isAuthenticated,
        token: state.token, // 也持久化 token 以便状态同步
      }),
      // 在 hydration 后自动加载用户信息
      onRehydrateStorage: () => (state) => {
        if (state) {
          // 检查 cookie 中的 token 是否存在
          const cookieToken = Cookies.get('auth_token');
          if (cookieToken && state.isAuthenticated && state.user) {
            // 如果 cookie 中有 token 且状态显示已认证，确保状态同步
            state.token = cookieToken;
          } else if (!cookieToken) {
            // 如果 cookie 中没有 token，清除认证状态
            state.user = null;
            state.token = null;
            state.isAuthenticated = false;
          }
        }
      },
    }
  )
);
