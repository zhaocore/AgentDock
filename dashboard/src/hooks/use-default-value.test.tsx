import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { LocaleProvider, useTranslation } from '@/i18n/core';
import { messages } from '@/i18n/dicts';
import { STORAGE_KEY } from '@/i18n/locale';
import { useDefaultValue } from './use-default-value';

// 渲染在 LocaleProvider 内：默认值随 locale 初始化/切换而变化，与 setup 页用法一致。
function useHarness() {
  const { t, setLocale } = useTranslation();
  const [value, setValue] = useDefaultValue(t('setup.defaultTestMessage'));
  return { value, setValue, setLocale };
}

describe('useDefaultValue', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('首渲染默认英文，locale 初始化为非英文（localStorage）后同步默认值', async () => {
    window.localStorage.setItem(STORAGE_KEY, 'ko');
    const { result } = renderHook(useHarness, { wrapper: LocaleProvider });
    // 等挂载 effect 完成 locale 初始化与默认值同步
    await act(async () => {});
    expect(result.current.value).toBe(messages.ko.setup.defaultTestMessage);
  });

  it('浏览器语言为非英文时默认值同步', async () => {
    Object.defineProperty(window.navigator, 'languages', {
      value: ['ko-KR', 'ko'],
      configurable: true,
    });
    try {
      const { result } = renderHook(useHarness, { wrapper: LocaleProvider });
      await act(async () => {});
      expect(result.current.value).toBe(messages.ko.setup.defaultTestMessage);
    } finally {
      delete (window.navigator as { languages?: unknown }).languages;
    }
  });

  it('未自定义时跟随语言切换', async () => {
    const { result } = renderHook(useHarness, { wrapper: LocaleProvider });
    await act(async () => {});
    act(() => result.current.setLocale('zh'));
    expect(result.current.value).toBe(messages.zh.setup.defaultTestMessage);
  });

  it('显式赋值后语言切换不覆盖自定义值', async () => {
    const { result } = renderHook(useHarness, { wrapper: LocaleProvider });
    await act(async () => {});
    act(() => result.current.setValue('custom message'));
    act(() => result.current.setLocale('ja'));
    expect(result.current.value).toBe('custom message');
  });
});
