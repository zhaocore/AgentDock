'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * 跟随外部默认值的字符串状态：默认值变化（如 locale 初始化或切换语言）时自动同步，
 * 任何显式赋值（用户输入或服务端返回的值）视为已自定义，此后默认值变化不再覆盖。
 *
 * 返回 [value, setValue]；setValue 语义与普通 setState 相同，但会把值标记为已自定义。
 */
export function useDefaultValue(
  defaultValue: string,
): readonly [string, (next: string) => void] {
  const [value, setValue] = useState(defaultValue);
  const isCustomRef = useRef(false);

  useEffect(() => {
    if (!isCustomRef.current) {
      setValue(defaultValue);
    }
  }, [defaultValue]);

  const setCustom = useCallback((next: string) => {
    isCustomRef.current = true;
    setValue(next);
  }, []);

  return [value, setCustom] as const;
}
