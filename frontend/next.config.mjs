/** @type {import('next').NextConfig} */
const nextConfig = {
  // 开发环境 API 代理配置
  async rewrites() {
    // 只在开发环境且没有设置 NEXT_PUBLIC_API_URL 时启用代理
    if (process.env.NODE_ENV === 'development' && !process.env.NEXT_PUBLIC_API_URL) {
      const apiTarget = process.env.API_PROXY_TARGET || 'http://localhost:8080';
      return [
        {
          source: '/api/:path*',
          destination: `${apiTarget}/api/:path*`,
        },
      ];
    }
    return [];
  },
};

export default nextConfig;