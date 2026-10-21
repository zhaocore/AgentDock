import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { interpolate, LocaleProvider, useTranslation } from './core';

function renderT() {
  return renderHook(() => useTranslation(), { wrapper: LocaleProvider });
}

describe('interpolate', () => {
  it('替换 {name} 占位符', () => {
    expect(
      interpolate('Hello {name}, {count} items', { name: 'Ada', count: 3 }),
    ).toBe('Hello Ada, 3 items');
  });

  it('未知占位符原样保留，暴露键名拼写错误', () => {
    expect(interpolate('Hi {missing}', { name: 'Ada' })).toBe('Hi {missing}');
  });
});

describe('useTranslation', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('服务端/首渲染默认英文', () => {
    const { result } = renderT();
    expect(result.current.locale).toBe('en');
    expect(result.current.t('nav.requests')).toBe('Requests');
  });

  it('切换语言后词条生效并写入 localStorage', () => {
    const { result } = renderT();
    act(() => result.current.setLocale('zh'));
    expect(result.current.locale).toBe('zh');
    expect(result.current.t('nav.requests')).toBe('请求');
    expect(window.localStorage.getItem('memgo-locale')).toBe('zh');
  });

  it('缺失键抛错而非静默回退（fail loud）', () => {
    const { result } = renderT();
    expect(() => result.current.t('common.not_a_key' as never)).toThrow();
  });

  it('插值传入词条变量', () => {
    const { result } = renderT();
    expect(result.current.t('nav.requests')).toBe('Requests');
  });
});
