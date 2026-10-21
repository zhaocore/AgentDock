import { defineConfig } from '@playwright/test';

// 登录页无需后端即可渲染，作为 i18n 黑盒冒烟入口。
// 已有运行中的服务时用 PLAYWRIGHT_BASE_URL 指向它；否则自动起 next dev。
export default defineConfig({
  testDir: './tests-e2e',
  timeout: 30_000,
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:3000',
  },
  webServer: process.env.PLAYWRIGHT_BASE_URL
    ? undefined
    : {
        command: 'pnpm dev',
        url: 'http://localhost:3000/login',
        // 禁止复用未知来源的旧服务，避免连到其他任务遗留进程造成假通过或无关失败。
        // 需要复用外部服务时，用 PLAYWRIGHT_BASE_URL 显式指定。
        reuseExistingServer: false,
        timeout: 120_000,
      },
});
