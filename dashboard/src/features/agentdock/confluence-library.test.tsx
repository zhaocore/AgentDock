import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ConfluenceLibrary } from './confluence-library';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';
import { buildLibraryDirectories } from './library-tree';

vi.mock('@/utils/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
const spaceID = 'a'.repeat(64);
const otherID = 'b'.repeat(64);
const page = {
  id: '1',
  title: 'One',
  url: 'https://example.com/pages/1',
  version: 1,
  path: 'Root/One',
  modified_at: '2026-10-21T00:00:00Z',
};
const catalog = {
  spaces: [
    {
      id: spaceID,
      key: 'TEAM',
      base_url: 'https://example.com',
      pages: [page],
    },
    {
      id: otherID,
      key: 'GAMES',
      base_url: 'https://example.com',
      pages: [{ ...page, id: '2', path: 'Games/Two', title: 'Two' }],
    },
  ],
  digest: 'digest',
};
const report = {
  id: '11111111-1111-1111-1111-111111111111',
  added: 1,
  updated: 0,
  kept: 0,
  conflicts: 1,
  items: [
    {
      space_id: spaceID,
      page_id: '3',
      path: 'Root/Three',
      action: 'added',
      reason: '新页面',
    },
    {
      space_id: spaceID,
      page_id: '1',
      path: 'Root/One',
      action: 'conflict',
      reason: '相同时间不同内容，保留现有',
    },
  ],
};
function setup() {
  vi.mocked(api.get).mockImplementation(async (path) => ({
    data:
      path === '/confluence-library'
        ? catalog
        : '# Saved page\n\n![image](./assets/asset-0123456789abcdef01234567.png)',
  }));
  return render(
    <LocaleProvider>
      <ConfluenceLibrary />
    </LocaleProvider>,
  );
}

it('browses multiple Space roots, nested Markdown and selected directory ZIP', async () => {
  setup();
  await screen.findByTestId(`library-space-${spaceID}`);
  fireEvent.click(screen.getByTestId('library-directory-Root'));
  fireEvent.click(screen.getByTestId('library-directory-Root%2FOne'));
  fireEvent.click(screen.getByTestId('library-page-1'));
  await waitFor(() =>
    expect(screen.getByTestId('library-markdown').textContent).toContain(
      'Saved page',
    ),
  );
  expect(
    screen.getByTestId('library-export-directory').getAttribute('href'),
  ).toContain(`space_id=${spaceID}&path=Root%2FOne`);
  expect(
    screen
      .getByTestId('library-markdown')
      .querySelector('img')
      ?.getAttribute('src'),
  ).toContain(`/spaces/${spaceID}/pages/1/file?name=assets%2F`);
  fireEvent.click(screen.getByTestId(`library-space-${otherID}`));
  expect(
    screen.getByTestId('library-export-directory').getAttribute('href'),
  ).toBe(`/api/agentdock/confluence-library/archive?space_id=${otherID}`);
  expect(screen.getByTestId('library-markdown').textContent).not.toContain(
    'Saved page',
  );
  fireEvent.click(screen.getByTestId('library-directory-Games'));
  fireEvent.click(screen.getByTestId('library-directory-Games%2FTwo'));
  expect(screen.getByTestId('library-page-2')).toBeTruthy();
});

it('previews overwrite checks before applying a directory merge', async () => {
  setup();
  await screen.findByTestId(`library-space-${spaceID}`);
  vi.mocked(api.post)
    .mockResolvedValueOnce({ data: report })
    .mockResolvedValueOnce({ data: report });
  const file = new File(['zip'], 'library.zip', { type: 'application/zip' });
  fireEvent.change(screen.getByTestId('library-import-file'), {
    target: { files: [file] },
  });
  await screen.findByTestId('library-import-report');
  expect(api.post).toHaveBeenCalledTimes(1);
  expect(api.post).toHaveBeenCalledWith('/confluence-library/imports', file, {
    headers: { 'Content-Type': 'application/zip' },
  });
  expect(screen.getByTestId('library-import-report').textContent).toContain(
    '相同时间不同内容',
  );
  fireEvent.click(screen.getByTestId('library-import-apply'));
  await screen.findByTestId('library-notice');
  expect(api.post).toHaveBeenCalledWith(
    `/confluence-library/imports/${report.id}/apply`,
  );
  expect(screen.queryByTestId('library-import-report')).toBeNull();
});

it('never applies a conflict-only ZIP and reports service errors', async () => {
  setup();
  await screen.findByTestId(`library-space-${spaceID}`);
  vi.mocked(api.post).mockResolvedValueOnce({ data: { ...report, added: 0 } });
  fireEvent.change(screen.getByTestId('library-import-file'), {
    target: { files: [new File(['zip'], 'library.zip')] },
  });
  await screen.findByTestId('library-import-report');
  expect(
    (screen.getByTestId('library-import-apply') as HTMLButtonElement).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByTestId('library-import-cancel'));
  expect(api.post).toHaveBeenCalledTimes(1);
  vi.mocked(api.post).mockRejectedValueOnce(
    new Error('ZIP missing or corrupt file'),
  );
  fireEvent.change(screen.getByTestId('library-import-file'), {
    target: { files: [new File(['invalid'], 'bad.zip')] },
  });
  expect((await screen.findByTestId('api-error')).textContent).toContain(
    'corrupt file',
  );
});

it('rejects oversized uploads before sending bytes', async () => {
  setup();
  await screen.findByTestId(`library-space-${spaceID}`);
  const file = new File(['zip'], 'large.zip');
  Object.defineProperty(file, 'size', { value: 10 * 1024 * 1024 * 1024 + 1 });
  fireEvent.change(screen.getByTestId('library-import-file'), {
    target: { files: [file] },
  });
  expect((await screen.findByTestId('api-error')).textContent).toContain(
    '10 GiB',
  );
  expect(api.post).not.toHaveBeenCalled();
});

it.each([101 * 1024 * 1024, 10 * 1024 * 1024 * 1024])(
  'accepts an import within the 10 GiB limit: %i bytes',
  async (size) => {
    setup();
    await screen.findByTestId(`library-space-${spaceID}`);
    vi.mocked(api.post).mockResolvedValueOnce({ data: report });
    const file = new File(['zip'], 'library.zip');
    Object.defineProperty(file, 'size', { value: size });
    fireEvent.change(screen.getByTestId('library-import-file'), {
      target: { files: [file] },
    });
    await screen.findByTestId('library-import-report');
    expect(api.post).toHaveBeenCalledWith('/confluence-library/imports', file, {
      headers: { 'Content-Type': 'application/zip' },
    });
  },
);

it('shows download API errors and request ID without starting a download', async () => {
  setup();
  await screen.findByTestId(`library-space-${spaceID}`);
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click');
  vi.mocked(api.get).mockRejectedValueOnce({
    isAxiosError: true,
    response: {
      data: { error: 'ZIP exceeds 10 GiB; export a smaller directory' },
      headers: { 'x-request-id': 'download-request' },
    },
  });
  fireEvent.click(screen.getByTestId('library-export-directory'));
  expect((await screen.findByTestId('api-error')).textContent).toContain(
    'ZIP exceeds 10 GiB; export a smaller directory (download-request)',
  );
  expect(api.get).toHaveBeenCalledWith(
    `/confluence-library/archive-check?space_id=${spaceID}`,
  );
  expect(click).not.toHaveBeenCalled();
  click.mockRestore();
});

it('builds shared ancestors without mutating input pages', () => {
  const pages = [page, { ...page, id: '2', title: 'Two', path: 'Root/Two' }];
  const before = JSON.stringify(pages);
  const tree = buildLibraryDirectories(pages);
  expect(tree).toHaveLength(1);
  expect(tree[0].children).toHaveLength(2);
  expect(JSON.stringify(pages)).toBe(before);
});
