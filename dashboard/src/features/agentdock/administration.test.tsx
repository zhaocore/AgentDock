import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Administration } from './administration';
import { ForcePasswordChange } from './force-password-change';
import { api } from '@/utils/api';
import { LocaleProvider } from '@/i18n/core';

const auth = vi.hoisted(() => ({
  user: { id: '11111111-1111-4111-8111-111111111111', is_admin: true },
  refreshUser: vi.fn(),
}));
vi.mock('@/hooks/use-auth', () => ({ useAuth: () => auth }));
vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}));
const userID = '22222222-2222-4222-8222-222222222222';
const row = {
  id: userID,
  email: 'member@test.example',
  status: 'active',
  is_admin: false,
  must_change_password: false,
  created_at: '2026-11-04T00:00:00Z',
};
const preview = {
  digest: 'a'.repeat(64),
  counts: {
    agents: 1,
    instances: 1,
    sessions: 2,
    messages: 3,
    confluence_tasks: 0,
    imports: 0,
    crawl_tasks: 0,
    crawl_runs: 0,
    tokens: 0,
  },
  library: { added: 0, updated: 0, kept: 0, conflicts: 0, items: [] },
};
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  auth.user.is_admin = true;
});
function mockReads() {
  vi.mocked(api.get).mockImplementation(async (path) => {
    if (path === '/admin/users') return { data: { data: [row] } };
    if (path === '/admin/settings')
      return { data: { registration_enabled: true } };
    if (String(path).endsWith('/deletion-preview')) return { data: preview };
    if (String(path).endsWith('/confluence-library'))
      return { data: { spaces: [], digest: 'digest' } };
    return { data: { data: [] } };
  });
}
it('changes registration, creates temporary account, previews then confirms deletion', async () => {
  mockReads();
  render(
    <LocaleProvider>
      <Administration />
    </LocaleProvider>,
  );
  await screen.findByTestId(`admin-user-${userID}`);
  fireEvent.click(screen.getByTestId('admin-registration'));
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/admin/settings', {
      registration_enabled: false,
    }),
  );
  fireEvent.change(screen.getByTestId('admin-create-email'), {
    target: { value: 'new@test.example' },
  });
  fireEvent.change(screen.getByTestId('admin-create-password'), {
    target: { value: 'temporary' },
  });
  fireEvent.click(screen.getByTestId('admin-create-submit'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith('/admin/users', {
      email: 'new@test.example',
      password: 'temporary',
    }),
  );
  fireEvent.click(screen.getByTestId(`admin-user-${userID}`));
  fireEvent.click(screen.getByTestId('admin-delete-preview'));
  await screen.findByTestId('admin-deletion-preview');
  expect(api.post).not.toHaveBeenCalledWith(
    `/admin/users/${userID}/delete`,
    expect.anything(),
  );
  fireEvent.click(screen.getByTestId('admin-delete-confirm'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(`/admin/users/${userID}/delete`, {
      digest: preview.digest,
    }),
  );
});
it('blocks ordinary user and reports mutation failures', async () => {
  auth.user.is_admin = false;
  render(
    <LocaleProvider>
      <Administration />
    </LocaleProvider>,
  );
  expect(screen.getByTestId('admin-forbidden')).toBeTruthy();
  expect(api.get).not.toHaveBeenCalled();
});
it('retains deletion preview after stop failure for explicit retry', async () => {
  mockReads();
  vi.mocked(api.post).mockRejectedValue(new Error('stop timeout'));
  render(
    <LocaleProvider>
      <Administration />
    </LocaleProvider>,
  );
  fireEvent.click(await screen.findByTestId(`admin-user-${userID}`));
  fireEvent.click(screen.getByTestId('admin-delete-preview'));
  fireEvent.click(await screen.findByTestId('admin-delete-confirm'));
  await screen.findByRole('alert');
  expect(screen.getByTestId('admin-deletion-preview')).toBeTruthy();
});
it('requires password change and refreshes account state', async () => {
  auth.refreshUser.mockResolvedValue(undefined);
  render(
    <LocaleProvider>
      <ForcePasswordChange />
    </LocaleProvider>,
  );
  fireEvent.change(screen.getByTestId('force-password-old'), {
    target: { value: 'temporary' },
  });
  fireEvent.change(screen.getByTestId('force-password-new'), {
    target: { value: 'permanent' },
  });
  fireEvent.submit(screen.getByTestId('force-password-form'));
  await waitFor(() => expect(auth.refreshUser).toHaveBeenCalledOnce());
  expect(api.post).toHaveBeenCalledWith('/auth/change-password', {
    old_password: 'temporary',
    new_password: 'permanent',
  });
});
