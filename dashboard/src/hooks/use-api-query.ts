import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from '@/components/ui/use-toast';
import { getErrorMessage } from '@/lib/error-message';

interface UseApiQueryOptions<T> {
  enabled?: boolean;
  errorToast?: string;
  initialData?: T;
}

interface UseApiQueryResult<T> {
  data: T | undefined;
  isLoading: boolean;
  error: string;
  refetch: () => Promise<void>;
}

export function useApiQuery<T>(
  fetcher: () => Promise<T>,
  options: UseApiQueryOptions<T> = {},
): UseApiQueryResult<T> {
  const { enabled = true, errorToast, initialData } = options;

  const [data, setData] = useState<T | undefined>(initialData);
  const [isLoading, setIsLoading] = useState(enabled);
  const [error, setError] = useState('');

  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  // 展示文案（errorToast）只进 ref：locale 切换改变文案不应重建 run 触发重复请求，
  // 出错时读取 ref 取最新语言文案。
  const errorToastRef = useRef(errorToast);
  errorToastRef.current = errorToast;

  const run = useCallback(async () => {
    setIsLoading(true);
    setError('');
    try {
      setData(await fetcherRef.current());
    } catch (err) {
      const errorToast = errorToastRef.current;
      const message = getErrorMessage(err, errorToast || 'Request failed');
      setError(message);
      if (errorToast) {
        toast({
          title: errorToast,
          description: message,
          variant: 'destructive',
        });
      }
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (enabled) void run();
  }, [enabled, run]);

  return { data, isLoading, error, refetch: run };
}
