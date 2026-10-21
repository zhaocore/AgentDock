import { expect, test } from '@playwright/test';

/** 使用真实API检测依赖，不通过创建对象验证写入权限。 */
test('settings shows real MySQL and Redis health', async ({ page }) => {
  test.skip(
    process.env.DUTYBOT_LIVE_E2E !== '1',
    'Requires real dutybotGo API',
  );
  test.setTimeout(120_000);
  await page.goto('/register');
  await page.getByTestId('auth-email').fill(`health-${Date.now()}@example.com`);
  await page.getByTestId('auth-password').fill('dutybot-e2e-password-2026');
  const registered = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/dutybot/auth/register') &&
      response.request().method() === 'POST',
  );
  await page.getByTestId('auth-submit').click();
  expect((await registered).status()).toBe(201);
  await expect(page.getByTestId('agent-shared-description')).toBeVisible();
  await page.getByTestId('nav-settings').click();
  await expect(page.getByTestId('health-redis')).toHaveText('connected');
  await expect(page.getByTestId('health-mysql')).toHaveText('connected');
  const refreshed = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/dutybot/health') &&
      response.request().method() === 'GET',
  );
  await page.getByTestId('health-refresh').click();
  expect((await refreshed).status()).toBe(200);
  await expect(page.getByTestId('health-redis')).toHaveText('connected');
  await expect(page.getByTestId('health-mysql')).toHaveText('connected');
});
