import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import Settings from '@/app/(root)/dashboard/settings/page';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }));
vi.mock('@/hooks/use-auth', () => ({
  useAuth: () => ({ user: { email: 'health@example.com' } }),
}));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
const healthy = {
  status: 'ok',
  mysql: 'connected',
  mysql_error: '',
  redis: 'connected',
  redis_error: '',
};

it('shows actual Redis states and refreshes a degraded response', async () => {
  vi.mocked(api.get).mockResolvedValueOnce({
    data: {
      ...healthy,
      status: 'degraded',
      redis: 'error',
      redis_error: 'PING failed',
    },
  });
  vi.mocked(api.get).mockResolvedValueOnce({ data: healthy });
  render(
    <LocaleProvider>
      <Settings />
    </LocaleProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId('health-redis').textContent).toBe('error'),
  );
  expect(screen.getByTestId('health-mysql').textContent).toBe('connected');
  expect(vi.mocked(api.get).mock.calls[0][1]?.validateStatus?.(503)).toBe(true);
  expect(screen.queryByTestId('api-error')).toBeNull();
  fireEvent.click(screen.getByTestId('health-refresh'));
  await waitFor(() =>
    expect(screen.getByTestId('health-redis').textContent).toBe('connected'),
  );
});

it('does not fabricate statuses when the API omits a required probe result', async () => {
  vi.mocked(api.get).mockResolvedValueOnce({
    data: {
      status: 'ok',
      mysql: 'connected',
      mysql_error: '',
    },
  });
  render(
    <LocaleProvider>
      <Settings />
    </LocaleProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId('api-error').textContent).toContain('Required'),
  );
  expect(screen.queryByTestId('health-redis')).toBeNull();
});
