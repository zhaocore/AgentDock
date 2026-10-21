import { renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { toast } from '@/components/ui/use-toast';
import { useApiQuery } from './use-api-query';

vi.mock('@/components/ui/use-toast', () => ({
  toast: vi.fn(),
}));

describe('useApiQuery', () => {
  beforeEach(() => {
    vi.mocked(toast).mockClear();
  });

  it('errorToast 文案变化（如切换语言）不重新触发请求', async () => {
    const fetcher = vi.fn(async () => 'data');
    const { result, rerender } = renderHook(
      (props: { errorToast: string }) =>
        useApiQuery(fetcher, { errorToast: props.errorToast }),
      { initialProps: { errorToast: 'Load failed' } },
    );

    await waitFor(() => expect(result.current.data).toBe('data'));
    expect(fetcher).toHaveBeenCalledTimes(1);

    rerender({ errorToast: '加载失败' });
    await waitFor(() => expect(result.current.data).toBe('data'));
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('错误 toast 使用出错时的最新语言文案，显式 refetch 仍有效', async () => {
    const fetcher = vi.fn(async (): Promise<string> => {
      throw new Error('boom');
    });
    const { result, rerender } = renderHook(
      (props: { errorToast: string }) =>
        useApiQuery(fetcher, { errorToast: props.errorToast }),
      { initialProps: { errorToast: '加载失败' as string } },
    );

    await waitFor(() => expect(result.current.error).toBe('boom'));
    expect(toast).toHaveBeenLastCalledWith(
      expect.objectContaining({ title: '加载失败' }),
    );

    // 模拟语言切换后再手动重试：错误提示应为切换后的文案
    rerender({ errorToast: 'Failed to load' });
    await result.current.refetch();
    await waitFor(() => expect(result.current.error).toBe('boom'));
    expect(toast).toHaveBeenLastCalledWith(
      expect.objectContaining({ title: 'Failed to load' }),
    );
  });
});
