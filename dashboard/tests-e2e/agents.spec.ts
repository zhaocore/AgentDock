import { expect, test } from '@playwright/test';

/** 浏览器交互验收；API 响应固定，隔离契约由后端测试覆盖。 */
test('public presets preview refreshed prompts and create private instances', async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error('Playwright baseURL is required');
  const userID = '00000000-0000-0000-0000-000000000001';
  const agentID = '22222222-2222-2222-2222-222222222222';
  const instanceID = '33333333-3333-3333-3333-333333333333';
  let prompt = 'Original deployer prompt';
  let created = false;
  await context.addCookies([
    {
      name: 'dutybot_session',
      value: 'ui-test-session',
      url: baseURL,
      httpOnly: true,
    },
  ]);
  await page.route('**/api/dutybot/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/api/dutybot/auth/me') {
      await route.fulfill({
        json: {
          id: userID,
          email: 'ui-test@example.com',
          created_at: '2026-10-21T00:00:00Z',
        },
      });
    } else if (path === '/api/dutybot/agents') {
      await route.fulfill({
        json: {
          data: [
            {
              id: agentID,
              owner_user_id: '',
              name: 'agent1',
              system_prompt: prompt,
              model: 'shared-model',
              skills: ['review'],
              mcp_servers: [{ name: 'tools', transport: 'stdio' }],
              subagents: [],
              created_at: '2026-10-21T00:00:00Z',
              updated_at: '2026-10-21T00:00:00Z',
            },
          ],
          total: 1,
          limit: 20,
          offset: 0,
          has_more: false,
        },
      });
    } else if (path === `/api/dutybot/agents/${agentID}/instances`) {
      if (request.method() === 'POST') {
        expect(request.postDataJSON()).toEqual({ repo_ids: [] });
        created = true;
        await route.fulfill({ status: 201, json: {} });
      } else {
        await route.fulfill({
          json: {
            data: created
              ? [
                  {
                    id: instanceID,
                    agent_id: agentID,
                    owner_user_id: userID,
                    status: 'created',
                    repo_ids: [],
                    created_at: '2026-10-21T00:00:00Z',
                    updated_at: '2026-10-21T00:00:00Z',
                  },
                ]
              : [],
          },
        });
      }
    } else {
      throw new Error(`Unexpected API request: ${request.method()} ${path}`);
    }
  });
  await page.goto('/dashboard/agents');
  await expect(page.getByTestId(`agent-name-${agentID}`)).toContainText(
    'agent1',
  );
  await expect(page.getByTestId('agent-create-open')).toHaveCount(0);
  await expect(page.getByTestId(`agent-delete-${agentID}`)).toHaveCount(0);
  await page.getByTestId(`agent-prompt-${agentID}`).click();
  await expect(page.getByTestId('agent-prompt-content')).toHaveText(prompt);
  await page.keyboard.press('Escape');
  prompt = 'Updated deployer prompt';
  const refreshed = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/dutybot/agents',
  );
  await page.getByTestId('agent-refresh').click();
  await refreshed;
  await page.getByTestId(`agent-prompt-${agentID}`).click();
  await expect(page.getByTestId('agent-prompt-content')).toHaveText(prompt);
  await page.keyboard.press('Escape');
  await page.getByTestId(`agent-toggle-instances-${agentID}`).click();
  await page.getByTestId(`instance-create-${agentID}`).click();
  await expect(page.getByTestId(`instance-row-${instanceID}`)).toBeVisible();
});
