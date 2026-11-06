import { expect, test } from '@playwright/test';

const admin = '11111111-1111-4111-8111-111111111111';
const member = '22222222-2222-4222-8222-222222222222';

test('administrator registration control and two-step account deletion', async ({
  page,
  baseURL,
}) => {
  let enabled = true;
  let deleted = false;
  await page.route('**/api/agentdock/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace('/api/agentdock', '');
    let data: object = { data: [] };
    if (path === '/auth/me')
      data = {
        id: admin,
        email: 'admin@test.example',
        created_at: '2026-11-04T00:00:00Z',
        is_admin: true,
        must_change_password: false,
      };
    else if (path === '/admin/users')
      data = {
        data: deleted
          ? []
          : [
              {
                id: member,
                email: 'member@test.example',
                created_at: '2026-11-04T00:00:00Z',
                status: 'active',
                is_admin: false,
                must_change_password: false,
              },
            ],
      };
    else if (path === '/admin/settings') {
      if (request.method() === 'PUT')
        enabled = request.postDataJSON().registration_enabled;
      data = { registration_enabled: enabled };
    } else if (path.endsWith('/confluence-library'))
      data = { spaces: [], digest: 'empty' };
    else if (path.endsWith('/deletion-preview'))
      data = {
        digest: 'a'.repeat(64),
        counts: {
          agents: 0,
          instances: 0,
          sessions: 1,
          messages: 2,
          confluence_tasks: 0,
          imports: 0,
          crawl_tasks: 0,
          crawl_runs: 0,
          tokens: 0,
        },
        library: { added: 0, updated: 0, kept: 0, conflicts: 0, items: [] },
      };
    else if (path.endsWith('/delete')) {
      deleted = true;
      data = { stage: 'completed' };
    }
    await route.fulfill({ json: data });
  });
  await page.context().addCookies([
    {
      name: 'agentdock_session',
      value: 'controlled-ui-session',
      url: baseURL!,
    },
  ]);
  await page.goto('/dashboard/admin');
  for (const width of [1280, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    const email = page.getByTestId('admin-create-email');
    const password = page.getByTestId('admin-create-password');
    const submit = page.getByTestId('admin-create-submit');
    await expect(submit).toBeVisible();
    await expect(async () => {
      const emailBox = await email.boundingBox();
      const passwordBox = await password.boundingBox();
      const submitBox = await submit.boundingBox();
      expect(emailBox).not.toBeNull();
      expect(passwordBox).not.toBeNull();
      expect(submitBox).not.toBeNull();
      if (width === 1280) {
        expect(Math.abs(emailBox!.y - passwordBox!.y)).toBeLessThan(1);
        expect(
          Math.abs(
            passwordBox!.y +
              passwordBox!.height -
              (submitBox!.y + submitBox!.height),
          ),
        ).toBeLessThan(1);
      }
      for (const box of [emailBox!, passwordBox!, submitBox!]) {
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width);
      }
      expect(
        submitBox!.y >= passwordBox!.y + passwordBox!.height ||
          submitBox!.x >= passwordBox!.x + passwordBox!.width,
      ).toBe(true);
    }).toPass();
  }
  await page.getByTestId('admin-registration').uncheck();
  await expect.poll(() => enabled).toBe(false);
  await page.getByTestId(`admin-user-${member}`).click();
  await page.getByTestId('admin-delete-preview').click();
  await expect(page.getByTestId('admin-deletion-preview')).toBeVisible();
  expect(deleted).toBe(false);
  await page.getByTestId('admin-delete-confirm').click();
  await expect.poll(() => deleted).toBe(true);
  await expect(page.getByTestId(`admin-user-${member}`)).toHaveCount(0);
});

test('temporary password blocks dashboard until password is changed', async ({
  page,
  baseURL,
}) => {
  let must = true;
  await page.route('**/api/agentdock/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: object = { data: [] };
    if (path.endsWith('/auth/me'))
      data = {
        id: member,
        email: 'member@test.example',
        created_at: '2026-11-04T00:00:00Z',
        is_admin: false,
        must_change_password: must,
      };
    if (path.endsWith('/auth/change-password')) {
      must = false;
      data = { message: 'password changed' };
    }
    await route.fulfill({ json: data });
  });
  await page.context().addCookies([
    {
      name: 'agentdock_session',
      value: 'controlled-ui-session',
      url: baseURL!,
    },
  ]);
  await page.goto('/dashboard/admin');
  await expect(page.getByTestId('force-password-form')).toBeVisible();
  await expect(page.getByTestId('admin-create-form')).toHaveCount(0);
  await page.getByTestId('force-password-old').fill('temporary');
  await page.getByTestId('force-password-new').fill('permanent');
  await page.getByTestId('force-password-submit').click();
  await expect(page.getByTestId('admin-forbidden')).toBeVisible();
});

test('closed registration hides login entry', async ({ page }) => {
  await page.route('**/api/agentdock/auth/registration', (route) =>
    route.fulfill({ json: { registration_enabled: false } }),
  );
  await page.goto('/login');
  await expect(page.getByTestId('login-title')).toBeVisible();
  await expect(page.getByTestId('register-link')).toHaveCount(0);
});
