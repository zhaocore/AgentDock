import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { z } from 'zod';
import { useResource } from './use-resource';
import { api } from '@/utils/api';

vi.mock('@/utils/api', () => ({ api: { get: vi.fn() } }));
const schema = z.object({ name: z.string() });
function Probe({ path }: { path: string }) {
  const resource = useResource(path, schema, null);
  return (
    <div data-testid="result">
      {resource.data?.name || resource.error || 'pending'}
    </div>
  );
}

// 稳定引用:避免内联函数导致 effect 依赖变化而反复重启
const pollAlways = (_value: { name: string }): boolean => true;

function PollingProbe({ path }: { path: string }) {
  const resource = useResource(path, schema, pollAlways);
  return (
    <div data-testid="polling">
      {resource.data?.name || 'pending'}|{resource.loading ? 'busy' : 'idle'}
    </div>
  );
}
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it('discards stale responses and clears data on scope changes', async () => {
  let resolveOld: (value: { data: { name: string } }) => void = () => {
    throw new Error('old request not started');
  };
  vi.mocked(api.get).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve;
      }),
  );
  vi.mocked(api.get).mockResolvedValueOnce({ data: { name: 'new tenant' } });
  const view = render(<Probe path="/old" />);
  view.rerender(<Probe path="/new" />);
  await waitFor(() =>
    expect(screen.getByTestId('result').textContent).toBe('new tenant'),
  );
  await act(async () => resolveOld({ data: { name: 'private old tenant' } }));
  expect(screen.getByTestId('result').textContent).toBe('new tenant');
});

it('keeps data mounted and skips loading state during poll refreshes', async () => {
  vi.mocked(api.get)
    .mockResolvedValueOnce({ data: { name: 'first' } })
    .mockResolvedValueOnce({ data: { name: 'second' } });
  render(<PollingProbe path="/tasks" />);
  await waitFor(() =>
    expect(screen.getByTestId('polling').textContent).toBe('first|idle'),
  );
  // 等待真实轮询间隔（5s）触发第二次拉取
  await waitFor(
    () => expect(screen.getByTestId('polling').textContent).toBe('second|idle'),
    { timeout: 8000 },
  );
}, 12000);

it('shows malformed external data as an error', async () => {
  vi.mocked(api.get).mockResolvedValueOnce({ data: { name: 42 } });
  render(<Probe path="/broken" />);
  await waitFor(() =>
    expect(screen.getByTestId('result').textContent).toContain(
      'Expected string',
    ),
  );
});
