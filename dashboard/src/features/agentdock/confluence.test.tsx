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
    `/api/agentdock/confluence-tasks/${id}/pages/1/file?name=assets%2F`,
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

it.each(['', '10', '1'])(
  'waits for scope selection before exporting Markdown: %s',
  async (nodeID) => {
    let current = {
      ...task,
      stage: 'selection',
      status: 'waiting',
      completed: 0,
    };
    vi.mocked(api.get).mockImplementation(async (path) => ({
      data:
        path === '/confluence-tasks'
          ? { data: [current] }
          : path.endsWith('/tree')
            ? tree
            : current,
    }));
    vi.mocked(api.post).mockImplementation(async () => {
      current = {
        ...current,
        stage: 'markdown',
        status: 'queued',
        total: nodeID === '1' ? 1 : 2,
      };
      return { data: current };
    });
    render(
      <LocaleProvider>
        <Confluence />
      </LocaleProvider>,
    );
    await screen.findByTestId('confluence-export-all');
    expect(api.post).not.toHaveBeenCalled();
    expect(screen.getByTestId('confluence-progress').textContent).toContain(
      '0/2',
    );
    if (nodeID) {
      if (nodeID === '1')
        fireEvent.click(screen.getByTestId('confluence-folder-10'));
      fireEvent.click(screen.getByTestId(`confluence-scope-${nodeID}`));
      fireEvent.click(screen.getByTestId('confluence-export-selected'));
    } else {
      expect(
        (screen.getByTestId('confluence-export-selected') as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      fireEvent.click(screen.getByTestId('confluence-export-all'));
    }
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith(`/confluence-tasks/${id}/export`, {
        node_id: nodeID,
      }),
    );
    await waitFor(() =>
      expect(screen.queryByTestId('confluence-export-all')).toBeNull(),
    );
  },
);

it('allows a new leaf export after completion and switches to the next batch', async () => {
  const nextID = '22222222-2222-2222-2222-222222222222';
  let current = { ...task, stage: 'markdown', status: 'running', total: 1 };
  const next = { ...current, id: nextID, status: 'queued', completed: 0 };
  vi.mocked(api.get).mockImplementation(async (path) => ({
    data:
      path === '/confluence-tasks'
        ? { data: [current, ...(current.id === nextID ? [] : [next])] }
        : path.endsWith('/tree')
          ? tree
          : current,
  }));
  vi.mocked(api.post).mockImplementation(async () => {
    current = next;
    return { data: next };
  });
  render(
    <LocaleProvider>
      <Confluence />
    </LocaleProvider>,
  );
  await screen.findByTestId('confluence-folder-10');
  expect(screen.queryByTestId('confluence-export-selected')).toBeNull();
  current = { ...current, status: 'completed' };
  fireEvent.click(screen.getByTestId('confluence-refresh'));
  await screen.findByTestId('confluence-export-all');
  fireEvent.click(screen.getByTestId('confluence-folder-10'));
  fireEvent.click(screen.getByTestId('confluence-scope-2'));
  fireEvent.click(screen.getByTestId('confluence-export-selected'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(`/confluence-tasks/${id}/export`, {
      node_id: '2',
    }),
  );
  await waitFor(() =>
    expect(
      (screen.getByTestId('confluence-tasks') as HTMLSelectElement).value,
    ).toBe(nextID),
  );
  await waitFor(() =>
    expect(screen.getByTestId('confluence-progress').textContent).toContain(
      '0/1',
    ),
  );
  expect(screen.queryByTestId('confluence-export-selected')).toBeNull();
});
