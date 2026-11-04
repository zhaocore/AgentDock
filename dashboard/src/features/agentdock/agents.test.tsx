import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Agents } from './agents';
import { LocaleProvider } from '@/i18n/core';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

const agentRow = {
  id: '22222222-2222-2222-2222-222222222222',
  owner_user_id: '',
  name: 'bot',
  system_prompt: 'sp',
  model: 'gpt-4o-mini',
  skills: ['search'],
  mcp_servers: [],
  subagents: [],
  created_at: '2026-10-16T00:00:00Z',
  updated_at: '2026-10-16T00:00:00Z',
};

function page(
  data: unknown = {
    data: [agentRow],
    total: 1,
    limit: 20,
    offset: 0,
    has_more: false,
  },
) {
  vi.mocked(api.get).mockResolvedValue({ data });
  return render(
    <LocaleProvider>
      <Agents />
    </LocaleProvider>,
  );
}

it('lists public presets and previews the synchronized prompt', async () => {
  page();
  await waitFor(() =>
    expect(
      screen.getByTestId(`agent-name-${agentRow.id}`).textContent,
    ).toContain('bot'),
  );
  expect(screen.getByTestId('agent-table').textContent).toContain(
    'gpt-4o-mini',
  );
  expect(screen.queryByTestId('agent-create-open')).toBeNull();
  expect(screen.queryByTestId(`agent-delete-${agentRow.id}`)).toBeNull();
  fireEvent.click(screen.getByTestId(`agent-prompt-${agentRow.id}`));
  expect(screen.getByTestId('agent-prompt-content').textContent).toBe('sp');
});

it('refreshes presets after deployer changes the prompt', async () => {
  page();
  await waitFor(() =>
    expect(screen.getByTestId(`agent-name-${agentRow.id}`)).toBeTruthy(),
  );
  vi.mocked(api.get).mockResolvedValue({
    data: {
      data: [{ ...agentRow, system_prompt: 'updated prompt' }],
      total: 1,
      limit: 20,
      offset: 0,
      has_more: false,
    },
  });
  fireEvent.click(screen.getByTestId('agent-refresh'));
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByTestId(`agent-prompt-${agentRow.id}`));
  await waitFor(() =>
    expect(screen.getByTestId('agent-prompt-content').textContent).toBe(
      'updated prompt',
    ),
  );
  expect(api.post).not.toHaveBeenCalled();
});

it('expands instances and starts one', async () => {
  const inst = {
    id: '33333333-3333-3333-3333-333333333333',
    agent_id: agentRow.id,
    owner_user_id: '00000000-0000-0000-0000-000000000001',
    status: 'created',
    repo_ids: [],
    created_at: '2026-10-16T00:00:00Z',
    updated_at: '2026-10-16T00:00:00Z',
  };
  vi.mocked(api.get)
    .mockResolvedValueOnce({
      data: {
        data: [agentRow],
        total: 1,
        limit: 20,
        offset: 0,
        has_more: false,
      },
    })
    .mockResolvedValueOnce({ data: { data: [inst] } });
  page();
  await waitFor(() =>
    expect(screen.getByTestId(`agent-name-${agentRow.id}`)).toBeTruthy(),
  );
  fireEvent.click(screen.getByTestId(`agent-toggle-instances-${agentRow.id}`));
  await waitFor(() =>
    expect(screen.getByTestId(`instance-row-${inst.id}`)).toBeTruthy(),
  );
  vi.mocked(api.post).mockResolvedValueOnce({ status: 202 });
  fireEvent.click(screen.getByTestId(`instance-start-${inst.id}`));
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      `/instances/${inst.id}/start`,
    ),
  );
});
