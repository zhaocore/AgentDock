import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Confluence } from './confluence';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetAllMocks();
});
const id = '11111111-1111-1111-1111-111111111111';
const task = {
  id,
  url: 'https://example.com/pages/viewpage.action?pageId=1',
  status: 'running',
  stage: 'export',
  completed: 1,
  total: 2,
  error: '',
  created_at: '2026-10-22T00:00:00Z',
  updated_at: '2026-10-22T00:00:00Z',
};
const tree = {
  nodes: [
    {
      id: '10',
      title: 'Folder',
      children: [
        { id: '1', title: 'Page', children: [] },
        { id: '2', title: 'Pending', children: [] },
      ],
    },
  ],
  pages: [
    { id: '1', title: 'Page', path: 'Folder/Page', version: 1, url: task.url },
    {
      id: '2',
      title: 'Pending',
      path: 'Folder/Pending',
      version: 1,
      url: task.url,
    },
  ],
};
function setup() {
  vi.mocked(api.get).mockImplementation(async (path) => ({
    data:
      path === '/confluence-tasks'
        ? { data: [task] }
        : path.endsWith('/tree')
          ? tree
          : path.includes('/file?')
            ? '# Saved\n\n![photo](./assets/asset-0123456789abcdef01234567.png)\n\n<script>danger()</script>'
            : task,
  }));
  return render(
    <LocaleProvider>
      <Confluence />
    </LocaleProvider>,
  );
}

it('starts, stops and resumes tasks; browses only completed files', async () => {
  setup();
  await screen.findByTestId('confluence-progress');
  expect(screen.getByTestId('confluence-progress').textContent).toContain(
    '1/2',
  );
  fireEvent.click(await screen.findByTestId('confluence-folder-10'));
  expect(
    (screen.getByTestId('confluence-page-2') as HTMLButtonElement).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByTestId('confluence-page-1'));
  await waitFor(() =>
    expect(screen.getByTestId('confluence-markdown').textContent).toContain(
      'Saved',
    ),
  );
  const article = screen.getByTestId('confluence-markdown');
  expect(article.querySelector('script')).toBeNull();
  expect(article.querySelector('img')?.getAttribute('src')).toContain(
    `/api/dutybot/confluence-tasks/${id}/pages/1/file?name=assets%2F`,
  );
  vi.mocked(api.post).mockResolvedValueOnce({
    data: { ...task, status: 'stopping' },
  });
  fireEvent.click(screen.getByTestId('confluence-stop'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(`/confluence-tasks/${id}/stop`, null),
  );
  vi.mocked(api.post).mockResolvedValueOnce({
    data: {
      ...task,
      id: '22222222-2222-2222-2222-222222222222',
      status: 'queued',
      stage: 'tree',
      completed: 0,
      total: 0,
    },
  });
  fireEvent.change(screen.getByTestId('confluence-url'), {
    target: { value: task.url },
  });
  fireEvent.click(screen.getByTestId('confluence-start'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith('/confluence-tasks', {
      url: task.url,
    }),
  );
});

it('polls every ten seconds and clears timers on unmount', async () => {
  const view = setup();
  await screen.findByTestId('confluence-progress');
  vi.useFakeTimers();
  // 重新启用时在假时钟中注册定时器。
  fireEvent.click(screen.getByTestId('confluence-auto'));
  fireEvent.click(screen.getByTestId('confluence-auto'));
  const before = vi.mocked(api.get).mock.calls.length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(9999);
  });
  expect(vi.mocked(api.get).mock.calls.length).toBe(before);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(vi.mocked(api.get).mock.calls.length).toBeGreaterThan(before);
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it('shows explicit service errors and allows manual refresh', async () => {
  vi.mocked(api.get).mockRejectedValue(new Error('RustFS unavailable'));
  render(
    <LocaleProvider>
      <Confluence />
    </LocaleProvider>,
  );
  expect((await screen.findByTestId('api-error')).textContent).toContain(
    'RustFS unavailable',
  );
  const before = vi.mocked(api.get).mock.calls.length;
  fireEvent.click(screen.getByTestId('confluence-refresh'));
  await waitFor(() =>
    expect(vi.mocked(api.get).mock.calls.length).toBeGreaterThan(before),
  );
});
