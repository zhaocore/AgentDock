import { expect, test } from '@playwright/test';

/** UI 全流程；实际 Confluence/RustFS 契约另做服务端验收。 */
test('starts and stops export, polls progress and opens saved Markdown', async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error('baseURL required');
  await context.addCookies([
    {
      name: 'dutybot_session',
      value: 'ui-test-session',
      url: baseURL,
      httpOnly: true,
    },
  ]);
  const id = '11111111-1111-1111-1111-111111111111';
  const url = 'https://example.com/pages/viewpage.action?pageId=1';
  let started = false;
  let status = 'running';
  let stage = 'tree';
  let exportRequests = 0;
  let refreshes = 0;
  const task = () => ({
    id,
    url,
    status,
    stage,
    completed: stage === 'markdown' ? (status === 'completed' ? 2 : 1) : 0,
    total: stage === 'tree' ? 0 : stage === 'markdown' ? 2 : 3,
    error: '',
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
  });
  await page.route('**/api/dutybot/**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path.endsWith('/auth/me'))
      return route.fulfill({
        json: {
          id: '00000000-0000-0000-0000-000000000001',
          email: 'ui@example.com',
          created_at: '2026-10-22T00:00:00Z',
        },
      });
    if (path === '/api/dutybot/confluence-tasks') {
      if (req.method() === 'POST') {
        expect(req.postDataJSON()).toEqual({ url });
        started = true;
        return route.fulfill({ status: 202, json: task() });
      }
      return route.fulfill({ json: { data: started ? [task()] : [] } });
    }
    if (path.endsWith('/export')) {
      expect(req.postDataJSON()).toEqual({
        node_id: exportRequests === 0 ? '10' : '3',
      });
      exportRequests++;
      stage = 'markdown';
      status = 'running';
      return route.fulfill({ status: 202, json: task() });
    }
    if (path.endsWith('/stop')) {
      status = 'stopped';
      return route.fulfill({ status: 202, json: task() });
    }
    if (path.endsWith('/start')) {
      status = 'running';
      return route.fulfill({ status: 202, json: task() });
    }
    if (path.endsWith('/tree'))
      return route.fulfill({
        json: {
          nodes: [
            {
              id: '10',
              title: 'Branch',
              children: [
                { id: '1', title: 'Exported page', children: [] },
                { id: '2', title: 'Pending page', children: [] },
              ],
            },
            { id: '3', title: 'Outside', children: [] },
          ],
          pages: [
            {
              id: '1',
              title: 'Exported page',
              path: 'Exported page',
              version: 1,
              url,
            },
            {
              id: '2',
              title: 'Pending page',
              path: 'Branch/Pending',
              version: 1,
              url,
            },
            ...(stage === 'markdown'
              ? []
              : [
                  {
                    id: '3',
                    title: 'Outside',
                    path: 'Outside',
                    version: 1,
                    url,
                  },
                ]),
          ],
        },
      });
    if (path.endsWith('/file'))
      return route.fulfill({
        contentType: 'text/plain',
        body: '# Saved Markdown\n\nOffline content',
      });
    if (path.endsWith(`/${id}`)) {
      refreshes++;
      if (stage === 'tree') {
        stage = 'selection';
        status = 'waiting';
      }
      return route.fulfill({ json: task() });
    }
    throw new Error(`Unexpected request ${path}`);
  });
  await page.clock.install();
  await page.goto('/dashboard/confluence');
  await page.getByTestId('confluence-url').fill(url);
  await page.getByTestId('confluence-start').click();
  await expect(page.getByTestId('confluence-export-all')).toBeVisible();
  expect(exportRequests).toBe(0);
  await page.getByTestId('confluence-scope-10').check();
  await page.getByTestId('confluence-export-selected').click();
  await expect(page.getByTestId('confluence-progress')).toContainText('1/2');
  expect(exportRequests).toBe(1);
  await page.getByTestId('confluence-folder-10').click();
  await expect(page.getByTestId('confluence-page-3')).toBeDisabled();
  await page.getByTestId('confluence-page-1').click();
  await expect(page.getByTestId('confluence-markdown')).toContainText(
    'Saved Markdown',
  );
  const before = refreshes;
  await page.clock.fastForward(10_000);
  await expect.poll(() => refreshes).toBeGreaterThan(before);
  await page.getByTestId('confluence-refresh').click();
  await page.getByTestId('confluence-stop').click();
  await expect(page.getByTestId('confluence-resume')).toBeVisible();
  await page.getByTestId('confluence-resume').click();
  await expect(page.getByTestId('confluence-stop')).toBeVisible();
  status = 'completed';
  await page.getByTestId('confluence-refresh').click();
  await expect(page.getByTestId('confluence-export-selected')).toBeVisible();
  await page.getByTestId('confluence-scope-3').check();
  await page.getByTestId('confluence-export-selected').click();
  await expect.poll(() => exportRequests).toBe(2);
  await expect(page.getByTestId('confluence-export-selected')).toHaveCount(0);
});
