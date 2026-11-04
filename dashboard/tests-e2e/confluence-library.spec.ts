import { expect, test } from '@playwright/test';

/** 文件库 UI 交互；真实 ZIP 与覆盖规则由后端往返测试验证。 */
test('browses Space roots, downloads directory ZIP and confirms checked import', async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error('baseURL required');
  await context.addCookies([
    {
      name: 'agentdock_session',
      value: 'ui-test-session',
      url: baseURL,
      httpOnly: true,
    },
  ]);
  const spaceID = 'a'.repeat(64);
  const otherID = 'b'.repeat(64);
  const importID = '11111111-1111-1111-1111-111111111111';
  let applied = false;
  let importRequests = 0;
  let checks = 0;
  const metadata = {
    id: '1',
    title: 'One',
    url: 'https://example.com/pages/1',
    version: 1,
    path: 'Root/One',
    modified_at: '2026-10-21T00:00:00Z',
  };
  const report = {
    id: importID,
    added: 1,
    updated: 0,
    kept: 1,
    conflicts: 1,
    items: [
      {
        space_id: spaceID,
        page_id: '2',
        path: 'Root/Two',
        action: 'added',
        reason: 'new',
      },
      {
        space_id: spaceID,
        page_id: '1',
        path: 'Root/One',
        action: 'conflict',
        reason: 'same time, different content; keep existing',
      },
    ],
  };
  await page.route('**/api/agentdock/**', async (route) => {
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
    if (path === '/api/agentdock/confluence-library')
      return route.fulfill({
        json: {
          spaces: [
            {
              id: spaceID,
              key: 'TEAM',
              base_url: 'https://example.com',
              pages: applied
                ? [
                    metadata,
                    { ...metadata, id: '2', title: 'Two', path: 'Root/Two' },
                  ]
                : [metadata],
            },
            {
              id: otherID,
              key: 'GAMES',
              base_url: 'https://example.com',
              pages: [
                { ...metadata, id: '3', path: 'Games/Three', title: 'Three' },
              ],
            },
          ],
          digest: 'digest',
        },
      });
    if (path.endsWith('/file'))
      return route.fulfill({
        contentType: 'text/plain',
        body: '# Saved Markdown',
      });
    if (path.endsWith('/archive-check')) {
      expect(new URL(req.url()).searchParams.get('path')).toBe('Root/One');
      checks++;
      if (checks === 1)
        return route.fulfill({
          status: 502,
          headers: { 'x-request-id': 'download-request' },
          json: { error: 'ZIP exceeds 10 GiB; export a smaller directory' },
        });
      return route.fulfill({ status: 204 });
    }
    if (path.endsWith('/archive')) {
      expect(new URL(req.url()).searchParams.get('path')).toBe('Root/One');
      return route.fulfill({
        contentType: 'application/zip',
        headers: {
          'content-disposition':
            'attachment; filename="confluence-library.zip"',
        },
        body: Buffer.from('PK fixture'),
      });
    }
    if (path.endsWith('/imports')) {
      importRequests++;
      expect(req.headers()['content-type']).toContain('application/zip');
      return route.fulfill({ json: report });
    }
    if (path.endsWith(`/imports/${importID}/apply`)) {
      applied = true;
      return route.fulfill({ json: report });
    }
    throw new Error(`Unexpected request ${path}`);
  });
  await page.goto('/dashboard/confluence-library');
  await expect(page.getByTestId('nav-confluence-library')).toHaveClass(
    /gg-nav-active/,
  );
  await expect(page.getByTestId('nav-confluence')).not.toHaveClass(
    /gg-nav-active/,
  );
  await expect(page.getByTestId(`library-space-${spaceID}`)).toBeVisible();
  await expect(page.getByTestId(`library-space-${otherID}`)).toBeVisible();
  await page.getByTestId('library-directory-Root').click();
  await page.getByTestId('library-directory-Root%2FOne').click();
  await page.getByTestId('library-page-1').click();
  await expect(page.getByTestId('library-markdown')).toContainText(
    'Saved Markdown',
  );
  await page.getByTestId('library-export-directory').click();
  await expect(page.getByTestId('api-error')).toContainText(
    'ZIP exceeds 10 GiB; export a smaller directory (download-request)',
  );
  const downloaded = page.waitForEvent('download');
  await page.getByTestId('library-export-directory').click();
  expect((await downloaded).suggestedFilename()).toBe('confluence-library.zip');
  await page.getByTestId('library-import-file').setInputFiles({
    name: 'library.zip',
    mimeType: 'application/zip',
    buffer: Buffer.from('ZIP fixture'),
  });
  await expect(page.getByTestId('library-import-report')).toContainText(
    'same time, different content',
  );
  await page.screenshot({
    path: test.info().outputPath('library.png'),
    fullPage: true,
  });
  expect(importRequests).toBe(1);
  expect(applied).toBe(false);
  await page.getByTestId('library-import-apply').click();
  await expect(page.getByTestId('library-notice')).toBeVisible();
  expect(applied).toBe(true);
  await page.getByTestId('library-directory-Root%2FTwo').click();
  await expect(page.getByTestId('library-page-2')).toBeVisible();
});
