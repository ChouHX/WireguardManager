import { useEffect } from 'react';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';

import { AppLayout } from '@/components/layout/AppLayout';
import { AuthLayout } from '@/components/layout/AuthLayout';
import { AuthGuard, GuestGuard } from '@/components/common/AuthGuard';
import { ErrorBoundary } from '@/components/common/ErrorBoundary';
import { setUnauthorizedHandler } from '@/services/api';
import { useAuthStore } from '@/stores/auth-store';

import AccountPage from '@/pages/AccountPage';
import AdminWireguardPage from '@/pages/AdminWireguardPage';
import DashboardPage from '@/pages/DashboardPage';
import NotFoundPage from '@/pages/NotFoundPage';
import UsersPage from '@/pages/UsersPage';
import WireguardPage from '@/pages/WireguardPage';
import LoginPage from '@/pages/auth/LoginPage';
import RegisterPage from '@/pages/auth/RegisterPage';

export function AppRoutes() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const loadUser = useAuthStore((state) => state.loadUser);
  const logout = useAuthStore((state) => state.logout);

  // 首次挂载时用已保存的 token 恢复会话
  useEffect(() => {
    void loadUser();
  }, [loadUser]);

  // 401 统一处理：清理状态并回到登录页
  useEffect(() => {
    setUnauthorizedHandler(() => {
      logout();
      navigate('/auth/login', { replace: true });
    });
    return () => setUnauthorizedHandler(null);
  }, [logout, navigate]);

  return (
    <ErrorBoundary resetKey={pathname}>
      <Routes>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />

        <Route
          element={
            <GuestGuard>
              <AuthLayout />
            </GuestGuard>
          }
        >
          <Route path="/auth/login" element={<LoginPage />} />
          <Route path="/auth/register" element={<RegisterPage />} />
        </Route>

        <Route
          element={
            <AuthGuard>
              <AppLayout />
            </AuthGuard>
          }
        >
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/wireguard" element={<WireguardPage />} />
          <Route
            path="/admin-wireguard"
            element={
              <AuthGuard requiredRole="admin">
                <AdminWireguardPage />
              </AuthGuard>
            }
          />
          <Route
            path="/users"
            element={
              <AuthGuard requiredRole="admin">
                <UsersPage />
              </AuthGuard>
            }
          />
          <Route path="/account" element={<AccountPage />} />
        </Route>

        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </ErrorBoundary>
  );
}
