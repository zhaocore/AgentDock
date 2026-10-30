import { expect, test } from '@playwright/test';

/** 浏览器交互契约；真实采集与队列由后端独立集成验证。 */
test('creates and edits a daily task, runs and downloads a report', async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error('baseURL required');
  await context.addCookies([
    {
      name: 'dutybot_session',
      value: 'crawl-ui-test',
      url: baseURL,
      httpOnly: true,
    },
  ]);
  const id = '11111111-1111-1111-1111-111111111111';
  const task = {
    id,
    name: 'Daily',
    agent_id: id,
    profile_id: 'docs',
    urls: ['https://docs.example/page'],
    enabled: true,
    created_at: '2026-10-30T01:00:00Z',
    updated_at: '2026-10-30T01:00:00Z',
  };
  const run = {
    id,
    task_id: id,
    task,
    trigger: 'manual',
    status: 'succeeded',
    stage: 'published',
    completed: 1,
    total: 1,
    error: '',
    created_at: task.created_at,
    updated_at: task.updated_at,
  };
  let created = false;
  let executed = false;
  let unavailable = false;
  await page.route('**/api/dutybot/**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path.endsWith('/auth/me'))
      return route.fulfill({
        json: { id, email: 'crawl@example.com', created_at: task.created_at },
      });
    if (path.endsWith('/crawl-profiles'))
      return route.fulfill({
        json: {
          data: unavailable
            ? []
            : [{ id: 'docs', origin: 'https://docs.example' }],
        },
      });
    if (path.endsWith('/agents'))
      return route.fulfill({
        json: {
          data: unavailable
            ? []
            : [
                {
                  id,
                  owner_user_id: '',
                  name: 'Reporter',
                  system_prompt: 'prompt',
                  model: 'test',
                  skills: [],
                  mcp_servers: [],
                  subagents: [],
                  created_at: task.created_at,
                  updated_at: task.updated_at,
                },
              ],
          total: 1,
          limit: 100,
          offset: 0,
          has_more: false,
        },
      });
    if (path.endsWith('/report'))
      return route.fulfill({
        body: '# Controlled report\n\nVerified fact',
        contentType: 'text/plain',
        headers: {
          'content-disposition': `attachment; filename="report-${id}.md"`,
        },
      });
    if (path.endsWith('/runs')) {
      if (req.method() === 'POST') {
        executed = true;
        return route.fulfill({ status: 202, json: run });
      }
      return route.fulfill({ json: { data: executed ? [run] : [] } });
    }
    if (req.method() === 'PUT') {
      expect(req.postDataJSON().enabled).toBe(false);
      return route.fulfill({ json: { ...task, enabled: false } });
    }
    if (path.endsWith('/crawl-tasks')) {
      if (req.method() === 'POST') {
        expect(req.postDataJSON()).toEqual({
          name: 'Daily',
          agent_id: id,
          profile_id: 'docs',
          urls: task.urls,
          enabled: true,
        });
        created = true;
        return route.fulfill({ status: 201, json: task });
      }
      return route.fulfill({ json: { data: created ? [task] : [] } });
    }
    return route.fulfill({
      status: 404,
      json: { error: 'unexpected UI request' },
    });
  });
  await page.goto('/dashboard/crawl');
  await page.getByTestId('crawl-name').fill('Daily');
  await page.getByTestId('crawl-agent').selectOption(id);
  await page.getByTestId('crawl-profile').selectOption('docs');
  await page.getByTestId('crawl-urls').fill(task.urls[0]);
  await page.getByTestId('crawl-save').click();
  await expect(page.getByTestId('crawl-edit')).toBeVisible();
  await page.getByTestId('crawl-edit').click();
  await page.getByTestId('crawl-enabled').uncheck();
  await page.getByTestId('crawl-save').click();
  await page.getByTestId('crawl-run').click();
  await page.getByTestId(`crawl-open-${id}`).click();
  await expect(page.getByTestId('crawl-report')).toContainText('Verified fact');
  const download = page.waitForEvent('download');
  await page.getByTestId(`crawl-download-${id}`).click();
  expect((await download).suggestedFilename()).toBe(`report-${id}.md`);
  unavailable = true;
  await page.reload();
  await expect(page.getByTestId('crawl-no-profile')).toBeVisible();
  const disabled = page.waitForRequest(
    (request) =>
      request.method() === 'PUT' &&
      request.url().endsWith(`/crawl-tasks/${id}`),
  );
  await page.getByTestId('crawl-disable').click();
  expect((await disabled).postDataJSON()).toEqual({
    name: task.name,
    agent_id: id,
    profile_id: 'docs',
    urls: task.urls,
    enabled: false,
  });
});
