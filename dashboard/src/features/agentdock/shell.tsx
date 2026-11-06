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
import { ForcePasswordChange } from './force-password-change';

const navigation = [
  ['crawl', 'agentdock.crawl', Boxes],
  ['repositories', 'agentdock.repositories', Boxes],
  ['confluence', 'agentdock.confluence', Boxes],
  ['confluence-library', 'agentdock.confluenceLibrary', Boxes],
  ['agents', 'agentdock.agents', Bot],
  ['sessions', 'agentdock.sessions', MessageSquare],
  ['api-keys', 'agentdock.apiKeys', KeyRound],
  ['settings', 'agentdock.settings', Settings],
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
  const { user } = useAuth();
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
      {user?.is_admin && (
        <Link
          className="gg-nav"
          href="/dashboard/admin"
          data-testid={`${prefix}-admin`}
        >
          账户管理
        </Link>
      )}
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
          AgentDock
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
            AgentDock
          </SheetTitle>
          <SheetDescription className="sr-only">
            {t('agentdock.menu')}
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
            title={t('agentdock.menu')}
            aria-label={t('agentdock.menu')}
            data-testid="menu-toggle"
            onClick={() => setOpen(!open)}
          >
            <Menu className="size-4" />
          </Button>
          <span className="md:hidden text-lg font-semibold">AgentDock</span>
          <div className="ml-auto flex items-center gap-2">
            <select
              data-testid="language-switcher"
              className="gg-select w-auto"
              aria-label={t('agentdock.language')}
              value={locale}
              onChange={(event) => setLocale(event.target.value as 'en' | 'zh')}
            >
              <option value="en">English</option>
              <option value="zh">中文</option>
            </select>
            <Button
              variant="ghost"
              size="icon"
              title={t('agentdock.theme')}
              aria-label={t('agentdock.theme')}
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
              title={t('agentdock.logout')}
              aria-label={t('agentdock.logout')}
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
          {user?.must_change_password ? <ForcePasswordChange /> : children}
        </main>
      </div>
    </div>
  );
}
