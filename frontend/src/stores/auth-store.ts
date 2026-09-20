import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { authService, tokenStorage } from '@/services';
import type { AuthState, LoginRequest, RegisterRequest, User } from '@/types/auth';

interface AuthStore extends AuthState {
  /** 是否已完成首次会话恢复（用于路由守卫避免闪烁） */
  initialized: boolean;
  login: (data: LoginRequest) => Promise<User>;
  register: (data: RegisterRequest) => Promise<void>;
  logout: () => void;
  loadUser: () => Promise<void>;
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthStore>()(
  persist(
    (set) => ({
      user: null,
      token: null,
      isAuthenticated: false,
      isLoading: false,
      initialized: false,

      login: async (data) => {
        set({ isLoading: true });
        try {
          const response = await authService.login(data);
          const payload = response.data;
          if (!response.success || !payload) {
            throw new Error(response.message || 'Login failed');
          }

          tokenStorage.set(payload.token);
          set({
            user: payload.user,
            token: payload.token,
            isAuthenticated: true,
            isLoading: false,
            initialized: true,
          });
          return payload.user;
        } catch (error) {
          set({ isLoading: false });
          throw error;
        }
      },

      register: async (data) => {
        set({ isLoading: true });
        try {
          const response = await authService.register(data);
          if (!response.success) {
            throw new Error(response.message || 'Registration failed');
          }
          set({ isLoading: false });
        } catch (error) {
          set({ isLoading: false });
          throw error;
        }
      },

      logout: () => {
        tokenStorage.clear();
        set({
          user: null,
          token: null,
          isAuthenticated: false,
          isLoading: false,
          initialized: true,
        });
      },

      loadUser: async () => {
        const token = tokenStorage.get();
        if (!token) {
          set({ user: null, token: null, isAuthenticated: false, isLoading: false, initialized: true });
          return;
        }

        set({ isLoading: true, token });
        try {
          const response = await authService.getMe();
          if (response.success && response.data) {
            set({
              user: response.data,
              token,
              isAuthenticated: true,
              isLoading: false,
              initialized: true,
            });
            return;
          }
          throw new Error(response.message || 'Failed to load profile');
        } catch {
          tokenStorage.clear();
          set({
            user: null,
            token: null,
            isAuthenticated: false,
            isLoading: false,
            initialized: true,
          });
        }
      },

      setUser: (user) => set({ user }),
    }),
    {
      name: 'wm-auth-store',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        user: state.user,
        token: state.token,
        isAuthenticated: state.isAuthenticated,
      }),
      onRehydrateStorage: () => (state) => {
        // token 以 localStorage 中的 wm_auth_token 为唯一来源
        const token = tokenStorage.get();
        if (!state) return;
        if (!token) {
          state.user = null;
          state.token = null;
          state.isAuthenticated = false;
        } else {
          state.token = token;
        }
      },
    },
  ),
);
