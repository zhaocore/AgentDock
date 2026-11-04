'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { ArrowRight, BookOpen } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';
import { Notice } from './ui';

export function CredentialsForm({
  title,
  action,
  submit,
  destination,
  children,
}: {
  title: string;
  action: string;
  submit: (email: string, password: string) => Promise<void>;
  destination: string;
  children: React.ReactNode;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <div className="gg-auth">
      <form
        data-testid="auth-form"
        onSubmit={async (event) => {
          event.preventDefault();
          setError('');
          setBusy(true);
          const values = new FormData(event.currentTarget);
          try {
            await submit(
              String(values.get('email')),
              String(values.get('password')),
            );
            router.replace(destination);
          } catch (cause) {
            setError(getErrorMessage(cause));
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="flex items-center gap-2 text-xl font-semibold">
          <BookOpen className="size-7 text-emerald-600" />
          AgentDock
        </div>
        <h1 data-testid="login-title">{title}</h1>
        <Notice error={error} loading={false} />
        <label className="gg-field" htmlFor="login-email">
          {t('agentdock.email')}
          <Input
            id="login-email"
            data-testid="auth-email"
            name="email"
            type="email"
            autoComplete="email"
            required
          />
        </label>
        <label className="gg-field" htmlFor="login-password">
          {t('agentdock.password')}
          <Input
            id="login-password"
            data-testid="auth-password"
            name="password"
            type="password"
            autoComplete="current-password"
            minLength={6}
            required
          />
        </label>
        <Button type="submit" data-testid="auth-submit" disabled={busy}>
          <ArrowRight className="size-4 mr-2" />
          {busy ? t('agentdock.loading') : action}
        </Button>
        <div className="text-sm">{children}</div>
      </form>
    </div>
  );
}
