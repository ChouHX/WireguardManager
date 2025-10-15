"use client";

import { useEffect, useState } from "react";
import Cookies from "js-cookie";
import { useAuthStore } from "@/stores/auth-store";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { loadUser, isAuthenticated, user, token } = useAuthStore();
  const [isInitialized, setIsInitialized] = useState(false);

  useEffect(() => {
    const initAuth = async () => {
      const cookieToken = Cookies.get('auth_token');
      
      // 如果有 cookie token 但没有用户信息，或者 token 不匹配，重新加载用户信息
      if (cookieToken && (!isAuthenticated || !user || token !== cookieToken)) {
        await loadUser();
      } else if (!cookieToken && isAuthenticated) {
        // 如果没有 cookie token 但状态显示已认证，清除状态
        await loadUser(); // 这会清除状态
      }
      
      setIsInitialized(true);
    };

    initAuth();
  }, []); // 只在组件挂载时运行一次

  // 在初始化完成前可以显示加载状态
  if (!isInitialized) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-900 dark:border-gray-100"></div>
      </div>
    );
  }

  return <>{children}</>;
}
