/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  // Next 15.5 dev overlay 门户会拦截页面对话框按钮的点击，破坏 Playwright 交互
  devIndicators: false,
  eslint: {
    ignoreDuringBuilds: false,
  },
  typescript: {
    ignoreBuildErrors: false,
  },
  experimental: {
    middlewareClientMaxBodySize: '101mb',
    optimizePackageImports: ['@/components', '@/lib', '@/utils'],
  },
  compress: true,
  images: {
    formats: ['image/webp', 'image/avif'],
  },
  // 页面级 CSP（frame-ancestors）由 middleware 设置：headers() 的全局 CSP
  // 会覆盖同源代理为原文响应合并的 sandbox 策略，不能放在这里。
  async headers() {
    return [
      {
        source: '/(.*)',
        headers: [
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
        ],
      },
    ];
  },
  redirects: async () => {
    return [
      {
        source: '/settings',
        destination: '/dashboard/settings',
        permanent: false,
      },
    ];
  },
};

export default nextConfig;
