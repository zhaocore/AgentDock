import { describe, expect, it } from 'vitest';

import { messages } from './dicts';
import { LOCALES } from './locale';

// 收集字典的全部点路径键。四种语言的键集合必须与 en 完全一致：
// 缺失键会导致运行时抛错，多余键说明键结构漂移。
function collectKeys(node: unknown, prefix = ''): string[] {
  if (typeof node === 'string') {
    return prefix ? [prefix] : [];
  }
  return Object.entries(node as Record<string, unknown>).flatMap(
    ([key, value]) => collectKeys(value, prefix ? `${prefix}.${key}` : key),
  );
}

describe('i18n dictionaries', () => {
  const enKeys = collectKeys(messages.en).sort();

  it('en 字典非空', () => {
    expect(enKeys.length).toBeGreaterThan(50);
  });

  it.each(LOCALES.filter((locale) => locale !== 'en'))(
    '%s 的键集合与 en 完全一致',
    (locale) => {
      expect(collectKeys(messages[locale]).sort()).toEqual(enKeys);
    },
  );
});
