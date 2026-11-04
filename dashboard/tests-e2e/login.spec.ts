import { expect, test } from '@playwright/test';

test('AgentDock login and language render without backend', async ({
  page,
}) => {
  await page.goto('/login');
  await expect(page.getByTestId('login-title')).toHaveText(
    'Sign in to AgentDock',
  );
  await expect(page.getByTestId('auth-email')).toBeVisible();
  await page.addInitScript(() =>
    window.localStorage.setItem('memgo-locale', 'zh'),
  );
  await page.reload();
  await expect(page.getByTestId('login-title')).toHaveText('登录 AgentDock');
});

test('unapproved old routes are gone and unsafe proxy paths rejected', async ({
  request,
}) => {
  expect((await request.get('/setup')).status()).toBe(404);
  expect((await request.post('/api/agentdock/auth/refresh')).status()).toBe(
    404,
  );
  expect(
    (
      await request.post('/api/agentdock/auth/login', {
        data: { email: 'test@example.com', password: 'password123' },
      })
    ).status(),
  ).toBe(403);
});
