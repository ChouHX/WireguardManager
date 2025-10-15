"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";
import { useAuthStore } from "@/stores/auth-store";
import { UserRole } from "@/types/auth";

interface AuthGuardProps {
  children: React.ReactNode;
  requireAuth?: boolean;
  requiredRole?: UserRole;
  redirectTo?: string;
}

export function AuthGuard({ 
  children, 
  requireAuth = true, 
  requiredRole,
  redirectTo = "/auth/login" 
}: AuthGuardProps) {
  const router = useRouter();
  const { user, isAuthenticated, isLoading, loadUser } = useAuthStore();

  useEffect(() => {
    // 如果需要认证但用户未登录
    if (requireAuth && !isLoading && !isAuthenticated) {
      router.push(redirectTo);
      return;
    }

    // 如果需要特定角色但用户角色不匹配
    if (requiredRole && user && user.role !== requiredRole && !isLoading) {
      // 根据用户角色重定向到适当的页面
      if (user.role === UserRole.ADMIN) {
        router.push("/admin/dashboard");
      } else {
        router.push("/dashboard");
      }
      return;
    }
  }, [isAuthenticated, isLoading, user, requireAuth, requiredRole, router, redirectTo]);

  // 如果正在加载，显示加载界面
  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="flex flex-col items-center space-y-4">
          <Loader2 className="h-8 w-8 animate-spin" />
          <p className="text-muted-foreground">加载中...</p>
        </div>
      </div>
    );
  }

  // 如果需要认证但用户未登录，不渲染子组件（即将重定向）
  if (requireAuth && !isAuthenticated) {
    return null;
  }

  // 如果需要特定角色但用户角色不匹配，不渲染子组件（即将重定向）
  if (requiredRole && user && user.role !== requiredRole) {
    return null;
  }

  return <>{children}</>;
}
