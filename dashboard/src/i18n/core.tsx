'use client';

// i18n 核心：LocaleProvider + useTranslation。语言状态仅存于客户端。
// 服务端首渲染固定英文，挂载后按 localStorage 或浏览器语言切换，避免水合不一致。

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';

import { messages } from './dicts';
import type { Messages } from './dicts';
import type { Paths } from './dict-shape';
import { LOCALES, STORAGE_KEY, detectLocale } from './locale';
import type { Locale } from './locale';

export type TranslationKey = Paths<Messages>;

export type TranslateFn = (
  key: TranslationKey,
  vars?: Record<string, string | number>,
) => string;

type LocaleContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: TranslateFn;
};

const LocaleContext = createContext<LocaleContextValue | null>(null);

// 按点路径查找词条；缺失即抛错（fail loud），禁止静默回退掩盖键名拼写错误。
function lookup(
  all: Record<Locale, Messages>,
  locale: Locale,
  key: string,
): string {
  let node: unknown = all[locale];
  for (const segment of key.split('.')) {
    if (typeof node !== 'object' || node === null || !(segment in node)) {
      throw new Error(
        `i18n: missing translation key "${key}" for locale "${locale}"`,
      );
    }
    node = (node as Record<string, unknown>)[segment];
  }
  if (typeof node !== 'string') {
    throw new Error(
      `i18n: entry for key "${key}" locale "${locale}" is not a string`,
    );
  }
  return node;
}

// {name} 占位符插值。未知占位符原样保留，便于暴露键名与变量名拼写错误。
export function interpolate(
  template: string,
  vars?: Record<string, string | number>,
): string {
  if (!vars) return template;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(vars, name)
      ? String(vars[name])
      : match,
  );
}

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>('en');

  // 挂载后读取用户选择，无选择时跟随浏览器语言。存储不可用按未选择处理。
  useEffect(() => {
    let stored: string | null = null;
    try {
      stored = window.localStorage.getItem(STORAGE_KEY);
    } catch {
      stored = null; // localStorage 不可用（隐私模式等）：仅本次会话生效
    }
    const locales = LOCALES as readonly string[];
    const isValid = stored !== null && locales.includes(stored);
    const next = isValid ? (stored as Locale) : detectLocale();
    setLocaleState(next);
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // localStorage 不可用（隐私模式等）：仅本次会话生效，不掩盖业务错误。
    }
  }, []);

  const t = useMemo<TranslateFn>(
    () => (key, vars) => interpolate(lookup(messages, locale, key), vars),
    [locale],
  );

  const value = useMemo(
    () => ({ locale, setLocale, t }),
    [locale, setLocale, t],
  );

  return (
    <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
  );
}

export function useTranslation(): LocaleContextValue {
  const ctx = useContext(LocaleContext);
  if (!ctx) {
    throw new Error('useTranslation 必须在 LocaleProvider 内使用');
  }
  return ctx;
}
