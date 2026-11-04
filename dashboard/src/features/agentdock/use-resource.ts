'use client';

import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import { z } from 'zod';
import { api } from '@/utils/api';
import { getErrorMessage } from '@/lib/error-message';

/** 范围改变时撤销旧请求；语言变化不会重新读取业务数据。 */
export function useResource<T>(
  path: string | null,
  schema: z.ZodType<T>,
  poll: ((data: T) => boolean) | null,
) {
  const [state, setState] = useState<{
    path: string | null;
    data: T | null;
    error: string;
    loading: boolean;
  }>({ path: null, data: null, error: '', loading: false });
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    if (path === null) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = async () => {
      if (document.hidden) {
        timer = setTimeout(load, 5000);
        return;
      }
      // 轮询刷新已有数据时不回到 loading 态，避免界面周期性跳动
      setState((current) => ({
        path,
        data: current.path === path ? current.data : null,
        error: '',
        loading: current.path !== path || current.data === null,
      }));
      try {
        const response = await api.get(path, {
          signal: controller.signal,
          validateStatus: (status) =>
            status === 200 || (path === '/health' && status === 503),
        });
        const data = schema.parse(response.data);
        if (controller.signal.aborted) return;
        setState({ path, data, error: '', loading: false });
        if (poll?.(data)) timer = setTimeout(load, 5000);
      } catch (cause) {
        if (!controller.signal.aborted && !axios.isCancel(cause))
          setState({
            path,
            data: null,
            error: getErrorMessage(cause),
            loading: false,
          });
      }
    };
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [path, schema, poll, revision]);
  return {
    data: state.path === path ? state.data : null,
    error: state.path === path ? state.error : '',
    loading: path !== null && (state.path !== path || state.loading),
    refresh,
  };
}
