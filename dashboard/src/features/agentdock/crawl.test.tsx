import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Crawl } from './crawl';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
const id = '11111111-1111-1111-1111-111111111111';
const task = {
  id,
  name: 'Daily',
  agent_id: id,
  profile_id: 'docs',
  profile_ids: ['other'],
  hops: 2,
  include_pattern: '/child',
  urls: [
    'https://docs.example/page',
    'https://feed.quanwenrss.com/feed/eeo-chanye-qiche.xml',
    'https://feed.quanwenrss.com/feed/eeo-kuaixun.xml',
  ],
  enabled: true,
  created_at: '2026-10-30T01:00:00Z',
  updated_at: '2026-10-30T01:00:00Z',
};
const run = {
  id,
  task_id: id,
  task,
  trigger: 'manual',
  status: 'partial',
  stage: 'published',
  completed: 1,
  total: 1,
  error: '1 page requires Cookie review',
  created_at: task.created_at,
  updated_at: task.updated_at,
};
function setup() {
  vi.mocked(api.get).mockImplementation(async (path) => ({
    data:
      path === '/crawl-tasks'
        ? { data: [task] }
        : path === '/crawl-profiles'
          ? {
              data: [
                { id: 'docs', origin: 'https://docs.example' },
                { id: 'other', origin: 'https://other.example' },
              ],
            }
          : path.startsWith('/agents')
            ? {
                data: [
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
              }
            : path.endsWith('/report')
              ? '# Report\n\nFact\n<script>danger()</script>\n![pixel](https://outside.invalid/pixel)'
              : { data: [run] },
  }));
  vi.mocked(api.post).mockImplementation(async (path) => ({
    data: path.endsWith('/runs') ? run : task,
  }));
  vi.mocked(api.put).mockResolvedValue({ data: task });
  return render(
    <LocaleProvider>
      <Crawl />
    </LocaleProvider>,
  );
}

it('creates, edits and manually runs task; previews safe report and downloads', async () => {
  setup();
  await screen.findByTestId(`crawl-open-${id}`);
  fireEvent.change(screen.getByTestId('crawl-name'), {
    target: { value: 'Daily' },
  });
  fireEvent.change(screen.getByTestId('crawl-agent'), {
    target: { value: id },
  });
  fireEvent.change(screen.getByTestId('crawl-profile'), {
    target: { value: 'docs' },
  });
  fireEvent.change(screen.getByTestId('crawl-urls'), {
    target: { value: task.urls.join('\n') },
  });
  expect((screen.getByTestId('crawl-hops') as HTMLInputElement).value).toBe(
    '0',
  );
  fireEvent.change(screen.getByTestId('crawl-hops'), {
    target: { value: '2' },
  });
  fireEvent.change(screen.getByTestId('crawl-pattern'), {
    target: { value: '/child' },
  });
  const extra = screen.getByTestId('crawl-extra-profiles') as HTMLSelectElement;
  extra.options[1].selected = true;
  fireEvent.change(extra);
  fireEvent.submit(screen.getByTestId('crawl-form'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith('/crawl-tasks', {
      name: 'Daily',
      agent_id: id,
      profile_id: 'docs',
      profile_ids: ['other'],
      hops: 2,
      include_pattern: '/child',
      urls: task.urls,
      enabled: true,
    }),
  );
  await waitFor(() =>
    expect(
      (screen.getByTestId('crawl-save') as HTMLButtonElement).disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByTestId('crawl-edit'));
  expect((screen.getByTestId('crawl-hops') as HTMLInputElement).value).toBe(
    '2',
  );
  expect((screen.getByTestId('crawl-pattern') as HTMLInputElement).value).toBe(
    '/child',
  );
  fireEvent.click(screen.getByTestId('crawl-enabled'));
  fireEvent.submit(screen.getByTestId('crawl-form'));
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith(
      `/crawl-tasks/${id}`,
      expect.objectContaining({ enabled: false }),
    ),
  );
  await waitFor(() =>
    expect(
      (screen.getByTestId('crawl-run') as HTMLButtonElement).disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByTestId('crawl-run'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(`/crawl-tasks/${id}/runs`),
  );
  fireEvent.click(screen.getByTestId(`crawl-open-${id}`));
  const article = await screen.findByTestId('crawl-report');
  expect(screen.getByTestId(`crawl-source-${id}`).getAttribute('href')).toBe(
    `/api/agentdock/crawl-runs/${id}/markdown`,
  );
  expect(article.textContent).toContain('Fact');
  expect(article.querySelector('script')).toBeNull();
  expect(article.querySelector('img')).toBeNull();
  expect(
    screen.getByTestId(`crawl-download-${id}`).getAttribute('href'),
  ).toContain('download=1');
});

it('shows execution conflict errors', async () => {
  setup();
  await screen.findByTestId(`crawl-open-${id}`);
  vi.mocked(api.post).mockRejectedValue(new Error('task already running'));
  fireEvent.click(screen.getByTestId('crawl-run'));
  await waitFor(() =>
    expect(screen.getByTestId('api-error').textContent).toContain(
      'task already running',
    ),
  );
});

it('disables existing task when Agent and login profile are unavailable', async () => {
  const view = setup();
  view.unmount();
  const get = vi.mocked(api.get).getMockImplementation()!;
  vi.mocked(api.get).mockImplementation(async (path, options) => {
    if (path === '/crawl-profiles') return { data: { data: [] } };
    if (path.startsWith('/agents'))
      return {
        data: { data: [], total: 0, limit: 100, offset: 0, has_more: false },
      };
    return get(path, options);
  });
  render(
    <LocaleProvider>
      <Crawl />
    </LocaleProvider>,
  );
  await screen.findByTestId('crawl-no-profile');
  fireEvent.click(screen.getByTestId('crawl-disable'));
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith(`/crawl-tasks/${id}`, {
      name: task.name,
      agent_id: task.agent_id,
      profile_id: task.profile_id,
      profile_ids: task.profile_ids,
      hops: task.hops,
      include_pattern: task.include_pattern,
      urls: task.urls,
      enabled: false,
    }),
  );
});

it('keeps full source download available after Agent report failure', async () => {
  const view = setup();
  view.unmount();
  const get = vi.mocked(api.get).getMockImplementation()!;
  vi.mocked(api.get).mockImplementation(async (path, options) =>
    path.endsWith('/runs')
      ? {
          data: {
            data: [
              {
                ...run,
                status: 'failed',
                stage: 'report',
                error: 'Agent unavailable',
              },
            ],
          },
        }
      : get(path, options),
  );
  render(
    <LocaleProvider>
      <Crawl />
    </LocaleProvider>,
  );
  expect(
    (await screen.findByTestId(`crawl-source-${id}`)).getAttribute('href'),
  ).toContain('/markdown');
  expect(screen.queryByTestId(`crawl-open-${id}`)).toBeNull();
});
