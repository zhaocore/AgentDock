import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ApiKeys } from './api-keys';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

const tokenRow = {
  id: '33333333-3333-3333-3333-333333333333',
  tenant_id: 'tenant',
  label: 'prod',
  token_prefix: 'gk_abcdef12',
  scopes: [{ kb_id: 'all', can_write: false }],
  expires_at: null,
  revoked_at: null,
  last_used_at: null,
  created_at: '2026-10-15T00:00:00Z',
};

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

function renderPage() {
  return render(
    <LocaleProvider>
      <ApiKeys />
    </LocaleProvider>,
  );
}

it('lists tokens with scope', async () => {
  vi.mocked(api.get).mockResolvedValueOnce({ data: { data: [tokenRow] } });

  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId(`apikey-label-${tokenRow.id}`).textContent).toBe(
      'prod',
    ),
  );
  // 范围为全局共享语义：all 展示为"全部资源"
  expect(screen.getByText('All resources')).toBeTruthy();
});

it('creates a token and reveals the one-time secret', async () => {
  const created = { ...tokenRow, token: 'gk_fullsecret' };
  vi.mocked(api.get).mockResolvedValueOnce({ data: { data: [] } });
  vi.mocked(api.post).mockResolvedValueOnce({ data: created });

  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId('empty-state').textContent).toContain(
      'No API keys',
    ),
  );
  fireEvent.click(screen.getByTestId('apikey-create-open'));
  fireEvent.change(screen.getByTestId('apikey-label-input'), {
    target: { value: 'prod' },
  });
  fireEvent.submit(screen.getByTestId('apikey-create-submit').closest('form')!);
  await waitFor(() =>
    expect(screen.getByTestId('apikey-secret').textContent).toBe(created.token),
  );
  expect(api.post).toHaveBeenCalledExactlyOnceWith('/api-tokens', {
    label: 'prod',
    kb_scope: 'all',
    can_write: false,
  });
});

it('keeps creation failures visible', async () => {
  vi.mocked(api.get).mockResolvedValueOnce({ data: { data: [] } });
  vi.mocked(api.post).mockRejectedValueOnce(new Error('label taken'));

  renderPage();
  await waitFor(() =>
    expect(screen.getByTestId('empty-state').textContent).toContain(
      'No API keys',
    ),
  );
  fireEvent.click(screen.getByTestId('apikey-create-open'));
  fireEvent.change(screen.getByTestId('apikey-label-input'), {
    target: { value: 'prod' },
  });
  fireEvent.submit(screen.getByTestId('apikey-create-submit').closest('form')!);
  await waitFor(() =>
    expect(screen.getByTestId('api-error').textContent).toContain(
      'label taken',
    ),
  );
  // 密钥展示对话框不应出现
  expect(screen.queryByTestId('apikey-secret')).toBeNull();
});
