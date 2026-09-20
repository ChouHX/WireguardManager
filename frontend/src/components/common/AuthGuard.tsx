import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';

import { LoadingScreen } from './LoadingScreen';
import { useAuthStore } from '@/stores/auth-store';
import type { UserRole } from '@/types/auth';

interface AuthGuardProps {
  children: ReactNode;
  /** 需要的最小角色；不传表示任意已登录用户 */
  requiredRole?: UserRole;
}

/**
 * 路由守卫：等待会话恢复完成后再判定，避免刷新时误跳登录页。
 */
export function AuthGuard({ children, requiredRole }: AuthGuardProps) {
  const location = useLocation();
  const user = useAuthStore((state) => state.user);
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const initialized = useAuthStore((state) => state.initialized);
  const loadUser = useAuthStore((state) => state.loadUser);

  // 会话尚未恢复时主动触发一次（例如直接访问深层路由）
  useEffect(() => {
    if (!initialized) {
      void loadUser();
    }
  }, [initialized, loadUser]);

  if (!initialized) {
    return <LoadingScreen />;
  }

  if (!isAuthenticated || !user) {
    return <Navigate to="/auth/login" replace state={{ from: location.pathname }} />;
  }

  if (requiredRole && user.role !== requiredRole) {
    return <Navigate to="/dashboard" replace />;
  }

  return <>{children}</>;
}

/** 已登录用户访问登录/注册页时跳回控制台 */
export function GuestGuard({ children }: { children: ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const initialized = useAuthStore((state) => state.initialized);

  if (initialized && isAuthenticated) {
    return <Navigate to="/dashboard" replace />;
  }

  return <>{children}</>;
}
