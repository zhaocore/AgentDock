import { expect, test } from '@playwright/test';

/** 验证真实localhost注册及退出，防止开发配置使用127.0.0.1时误拒绝。 */
test('localhost registration and logout keep Origin protection', async ({
  page,
  request,
  baseURL,
}) => {
  test.skip(
    process.env.AGENTDOCK_LIVE_E2E !== '1',
    'Requires real AgentDock API',
  );
  test.setTimeout(120_000);
  if (!baseURL) throw new Error('Playwright baseURL is required');
  const registerURL = new URL('/register', baseURL);
  registerURL.hostname = 'localhost';
  await page.goto(registerURL.toString());
  const email = `loopback-${Date.now()}@example.com`;
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill('agentdock-e2e-password-2026');
  const registered = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/agentdock/auth/register') &&
      response.request().method() === 'POST',
  );
  await page.getByTestId('auth-submit').click();
  expect((await registered).status()).toBe(201);
  await expect(page.getByTestId('agent-shared-description')).toBeVisible();
  const session = (await page.context().cookies()).find(
    (cookie) => cookie.name === 'agentdock_session',
  );
  expect(session?.httpOnly).toBe(true);
  await page.reload();
  await expect(page.getByTestId('agent-shared-description')).toBeVisible();
  const apiURL = new URL('/api/agentdock/auth/register', registerURL);
  for (const origin of [
    'http://evil.test',
    `http://localhost:${Number(registerURL.port) + 1}`,
  ]) {
    expect(
      (
        await request.post(apiURL.toString(), {
          headers: { Origin: origin },
          data: {},
        })
      ).status(),
    ).toBe(403);
  }
  const loggedOut = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/session') &&
      response.request().method() === 'DELETE',
  );
  await page.getByTestId('logout').click();
  expect((await loggedOut).status()).toBe(200);
  await expect(page.getByTestId('login-title')).toBeVisible();
  expect(
    (await page.context().cookies()).some(
      (cookie) => cookie.name === 'agentdock_session',
    ),
  ).toBe(false);
});
