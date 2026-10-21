// 支持的语言清单与浏览器语言检测（无 React 依赖，便于单元测试）。

export const LOCALES = ['en', 'zh', 'ja', 'ko'] as const;
export type Locale = (typeof LOCALES)[number];

// 各语言的自我称呼，用于语言切换菜单展示。
export const LANGUAGE_LABELS: Record<Locale, string> = {
  en: 'English',
  zh: '中文',
  ja: '日本語',
  ko: '한국어',
};

export const STORAGE_KEY = 'memgo-locale';

// 浏览器语言列表 → 支持的 locale：先整串匹配（zh-CN），再按主语言匹配（zh）。
// candidates 参数供测试注入，省略时读取 navigator。
export function detectLocale(candidates?: readonly string[]): Locale {
  const languages =
    candidates ??
    (typeof navigator === 'undefined'
      ? []
      : (navigator.languages ?? [navigator.language]));
  for (const language of languages) {
    const lowered = language.toLowerCase();
    if ((LOCALES as readonly string[]).includes(lowered)) {
      return lowered as Locale;
    }
    const base = lowered.split('-')[0];
    if ((LOCALES as readonly string[]).includes(base)) {
      return base as Locale;
    }
  }
  return 'en';
}
