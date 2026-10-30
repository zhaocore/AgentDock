'use client';

import { useState } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Bot,
  Boxes,
  KeyRound,
  MessageSquare,
  Settings,
  Menu,
  Moon,
  Sun,
  LogOut,
} from 'lucide-react';
import { useTheme } from 'next-themes';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet';
import { useTranslation } from '@/i18n/core';
import { useAuth } from '@/hooks/use-auth';
import { getErrorMessage } from '@/lib/error-message';
import { Notice } from './ui';

const navigation = [
  ['crawl', 'dutybot.crawl', Boxes],
  ['repositories', 'dutybot.repositories', Boxes],
  ['confluence', 'dutybot.confluence', Boxes],
  ['confluence-library', 'dutybot.confluenceLibrary', Boxes],
  ['agents', 'dutybot.agents', Bot],
  ['sessions', 'dutybot.sessions', MessageSquare],
  ['api-keys', 'dutybot.apiKeys', KeyRound],
  ['settings', 'dutybot.settings', Settings],
] as const;

function WorkspaceNavigation({
  prefix,
  onNavigate,
}: {
  prefix: string;
  onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const path = usePathname();
  return (
    <nav aria-label="Workspace" className="gg-navigation">
      {navigation.map(([route, label, Icon]) => (
        <Link
          key={route}
          data-testid={`${prefix}-${route}`}
          href={`/dashboard/${route}`}
          onClick={onNavigate}
          className={`gg-nav ${path === `/dashboard/${route}` || path.startsWith(`/dashboard/${route}/`) ? 'gg-nav-active' : ''}`}
        >
          <Icon className="size-4" />
          {t(label)}
        </Link>
      ))}
    </nav>
  );
}

export function Shell({ children }: { children: React.ReactNode }) {
  const { t, locale, setLocale } = useTranslation();
  const { user, logout } = useAuth();
  const { resolvedTheme, setTheme } = useTheme();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  return (
    <div className="gg-shell">
      <aside className="gg-sidebar">
        <Link className="gg-brand" href="/dashboard/agents">
          <Bot className="size-6 text-emerald-600" />
          dutybotGo
        </Link>
        <WorkspaceNavigation prefix="nav" onNavigate={() => setOpen(false)} />
        <div className="mt-auto border-t p-4 text-xs text-muted-foreground break-all">
          {user?.email}
        </div>
      </aside>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="left" className="w-64 p-0">
          <SheetTitle className="gg-brand">
            <Bot className="size-6 text-emerald-600" />
            dutybotGo
          </SheetTitle>
          <SheetDescription className="sr-only">
            {t('dutybot.menu')}
          </SheetDescription>
          <WorkspaceNavigation
            prefix="mobile-nav"
            onNavigate={() => setOpen(false)}
          />
        </SheetContent>
      </Sheet>
      <div className="gg-workspace">
        <header className="gg-header">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            title={t('dutybot.menu')}
            aria-label={t('dutybot.menu')}
            data-testid="menu-toggle"
            onClick={() => setOpen(!open)}
          >
            <Menu className="size-4" />
          </Button>
          <span className="md:hidden text-lg font-semibold">dutybotGo</span>
          <div className="ml-auto flex items-center gap-2">
            <select
              data-testid="language-switcher"
              className="gg-select w-auto"
              aria-label={t('dutybot.language')}
              value={locale}
              onChange={(event) => setLocale(event.target.value as 'en' | 'zh')}
            >
              <option value="en">English</option>
              <option value="zh">中文</option>
            </select>
            <Button
              variant="ghost"
              size="icon"
              title={t('dutybot.theme')}
              aria-label={t('dutybot.theme')}
              data-testid="theme-toggle"
              onClick={() =>
                setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')
              }
            >
              <Sun className="size-4 hidden dark:block" />
              <Moon className="size-4 dark:hidden" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              title={t('dutybot.logout')}
              aria-label={t('dutybot.logout')}
              data-testid="logout"
              onClick={() =>
                void logout().catch((cause) => setError(getErrorMessage(cause)))
              }
            >
              <LogOut className="size-4" />
            </Button>
          </div>
        </header>
        <main className="gg-main">
          <Notice error={error} loading={false} />
          {children}
        </main>
      </div>
    </div>
  );
}
