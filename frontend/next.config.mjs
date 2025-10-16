/** @type {import('next').NextConfig} */
const nextConfig = {
  // Enable standalone output for Docker
  output: 'standalone',
  
  // API 代理配置（开发和生产环境）
  async rewrites() {
    // 从环境变量获取后端地址
    const apiTarget = process.env.API_PROXY_TARGET || 'http://localhost:8080';
    
    return [
      {
        source: '/api/:path*',
        destination: `${apiTarget}/api/:path*`,
      },
      {
        source: '/health',
        destination: `${apiTarget}/health`,
      },
    ];
  },
};

export default nextConfig;