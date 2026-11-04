import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Sessions } from './sessions';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const sessionRow = {
  id: '44444444-4444-4444-4444-444444444444',
  user_id: '00000000-0000-0000-0000-000000000001',
  instance_id: '33333333-3333-3333-3333-333333333333',
  agent_id: '22222222-2222-2222-2222-222222222222',
  title: '新会话',
  created_at: '2026-10-16T00:00:00Z',
  updated_at: '2026-10-16T00:00:00Z',
};

function page() {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      data: [sessionRow],
      total: 1,
      limit: 20,
      offset: 0,
      has_more: false,
    },
  });
  return render(
    <LocaleProvider>
      <Sessions />
    </LocaleProvider>,
  );
}

it('lists sessions for the current user', async () => {
  page();
  await waitFor(() =>
    expect(screen.getByTestId(`session-open-${sessionRow.id}`)).toBeTruthy(),
  );
});

it('opens a session, shows messages and sends one', async () => {
  const message = {
    id: '55555555-5555-5555-5555-555555555555',
    session_id: sessionRow.id,
    role: 'assistant',
    content: 'hi',
    agent_meta: null,
    created_at: '2026-10-16T00:00:00Z',
  };
  vi.mocked(api.get)
    .mockResolvedValueOnce({
      data: {
        data: [sessionRow],
        total: 1,
        limit: 20,
        offset: 0,
        has_more: false,
      },
    })
    .mockResolvedValue({ data: { data: [message] } });
  render(
    <LocaleProvider>
      <Sessions />
    </LocaleProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId(`session-open-${sessionRow.id}`)).toBeTruthy(),
  );
  fireEvent.click(screen.getByTestId(`session-open-${sessionRow.id}`));
  await waitFor(() =>
    expect(
      screen.getByTestId(`session-message-${message.id}`).textContent,
    ).toContain('hi'),
  );
  vi.mocked(api.post).mockResolvedValueOnce({ data: message });
  fireEvent.change(screen.getByTestId('session-message-input'), {
    target: { value: 'next question' },
  });
  fireEvent.click(screen.getByTestId('session-message-send'));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      `/sessions/${sessionRow.id}/messages`,
      { content: 'next question' },
    ),
  );
});
