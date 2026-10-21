import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Repositories } from './repositories';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const repoRow = {
  id: '11111111-1111-1111-1111-111111111111',
  name: 'web',
  git_url: 'https://git.example.com/web.git',
  repo_type: 'frontend',
  branch: 'main',
  local_path: null,
  status: 'ready',
  last_error: null,
  created_at: '2026-10-16T00:00:00Z',
  updated_at: '2026-10-16T00:00:00Z',
};

const pageBody = (rows: unknown[] = [repoRow]) => ({
  data: rows,
  total: rows.length,
  limit: 20,
  offset: 0,
  has_more: false,
});

function renderPage() {
  return render(
    <LocaleProvider>
      <Repositories />
    </LocaleProvider>,
  );
}

it('lists shared repositories with status', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: pageBody() });
  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId(`repo-name-${repoRow.id}`).textContent).toBe(
      'web',
    ),
  );
  expect(screen.getByTestId('repo-table').textContent).toContain('main');
});

it('shows empty state when no repositories', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: pageBody([]) });
  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId('empty-state').textContent).toContain(
      'No records',
    ),
  );
});

it('creates a repository and schedules clone', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: pageBody([]) });
  renderPage();
  await waitFor(() => expect(screen.getByTestId('empty-state')).toBeTruthy());
  fireEvent.click(screen.getByTestId('repo-create-open'));
  fireEvent.change(screen.getByTestId('repo-form-name'), {
    target: { value: 'web' },
  });
  fireEvent.change(screen.getByTestId('repo-form-url'), {
    target: { value: 'https://git.example.com/web.git' },
  });
  vi.mocked(api.post).mockResolvedValueOnce({ data: repoRow });
  fireEvent.click(screen.getByTestId('repo-form-submit'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledExactlyOnceWith('/repositories', {
      name: 'web',
      git_url: 'https://git.example.com/web.git',
      repo_type: 'frontend',
      branch: 'main',
    }),
  );
});

it('sync triggers accepted task', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: pageBody() });
  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId(`repo-name-${repoRow.id}`)).toBeTruthy(),
  );
  vi.mocked(api.post).mockResolvedValueOnce({ status: 202 });
  fireEvent.click(screen.getByTestId(`repo-sync-${repoRow.id}`));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      `/repositories/${repoRow.id}/sync`,
    ),
  );
});
