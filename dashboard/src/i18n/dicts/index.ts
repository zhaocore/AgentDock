// 汇总所有命名空间字典。新增命名空间时：新建 dicts/<ns>.ts（结构同 entities.ts），
// 并在此注册 Messages 与四语言 messages。
import type { Shape } from '../dict-shape';
import type { Locale } from '../locale';

import { common } from './common';
import { nav } from './nav';
import { login } from './login';
import { setup } from './setup';
import { requests } from './requests';
import { memories } from './memories';
import { entities } from './entities';
import { categories } from './categories';
import { dream } from './dream';
import { webhooks } from './webhooks';
import { analytics } from './analytics';
import { exportDict } from './export';
import { apiKeys } from './apiKeys';
import { users } from './users';
import { configuration } from './configuration';
import { settings } from './settings';
import { dutybot } from './dutybot';

export type Messages = {
  dutybot: Shape<typeof dutybot.en>;
  common: Shape<typeof common.en>;
  nav: Shape<typeof nav.en>;
  login: Shape<typeof login.en>;
  setup: Shape<typeof setup.en>;
  requests: Shape<typeof requests.en>;
  memories: Shape<typeof memories.en>;
  entities: Shape<typeof entities.en>;
  categories: Shape<typeof categories.en>;
  dream: Shape<typeof dream.en>;
  webhooks: Shape<typeof webhooks.en>;
  analytics: Shape<typeof analytics.en>;
  export: Shape<typeof exportDict.en>;
  apiKeys: Shape<typeof apiKeys.en>;
  users: Shape<typeof users.en>;
  configuration: Shape<typeof configuration.en>;
  settings: Shape<typeof settings.en>;
};

export const messages: Record<Locale, Messages> = {
  en: {
    dutybot: dutybot.en,
    common: common.en,
    nav: nav.en,
    login: login.en,
    setup: setup.en,
    requests: requests.en,
    memories: memories.en,
    entities: entities.en,
    categories: categories.en,
    dream: dream.en,
    webhooks: webhooks.en,
    analytics: analytics.en,
    export: exportDict.en,
    apiKeys: apiKeys.en,
    users: users.en,
    configuration: configuration.en,
    settings: settings.en,
  },
  zh: {
    dutybot: dutybot.zh,
    common: common.zh,
    nav: nav.zh,
    login: login.zh,
    setup: setup.zh,
    requests: requests.zh,
    memories: memories.zh,
    entities: entities.zh,
    categories: categories.zh,
    dream: dream.zh,
    webhooks: webhooks.zh,
    analytics: analytics.zh,
    export: exportDict.zh,
    apiKeys: apiKeys.zh,
    users: users.zh,
    configuration: configuration.zh,
    settings: settings.zh,
  },
  ja: {
    dutybot: dutybot.ja,
    common: common.ja,
    nav: nav.ja,
    login: login.ja,
    setup: setup.ja,
    requests: requests.ja,
    memories: memories.ja,
    entities: entities.ja,
    categories: categories.ja,
    dream: dream.ja,
    webhooks: webhooks.ja,
    analytics: analytics.ja,
    export: exportDict.ja,
    apiKeys: apiKeys.ja,
    users: users.ja,
    configuration: configuration.ja,
    settings: settings.ja,
  },
  ko: {
    dutybot: dutybot.ko,
    common: common.ko,
    nav: nav.ko,
    login: login.ko,
    setup: setup.ko,
    requests: requests.ko,
    memories: memories.ko,
    entities: entities.ko,
    categories: categories.ko,
    dream: dream.ko,
    webhooks: webhooks.ko,
    analytics: analytics.ko,
    export: exportDict.ko,
    apiKeys: apiKeys.ko,
    users: users.ko,
    configuration: configuration.ko,
    settings: settings.ko,
  },
};
