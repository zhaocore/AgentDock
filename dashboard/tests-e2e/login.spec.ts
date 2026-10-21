import { expect, test } from '@playwright/test';

test('dutybotGo login and language render without backend', async ({
  page,
}) => {
  await page.goto('/login');
  await expect(page.getByTestId('login-title')).toHaveText(
    'Sign in to dutybotGo',
  );
  await expect(page.getByTestId('auth-email')).toBeVisible();
  await page.addInitScript(() =>
    window.localStorage.setItem('memgo-locale', 'zh'),
  );
  await page.reload();
  await expect(page.getByTestId('login-title')).toHaveText('登录 dutybotGo');
});

test('unapproved old routes are gone and unsafe proxy paths rejected', async ({
  request,
}) => {
  expect((await request.get('/setup')).status()).toBe(404);
  expect((await request.post('/api/dutybot/auth/refresh')).status()).toBe(404);
  expect(
    (
      await request.post('/api/dutybot/auth/login', {
        data: { email: 'test@example.com', password: 'password123' },
      })
    ).status(),
  ).toBe(403);
});
