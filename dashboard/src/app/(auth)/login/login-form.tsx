'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { useAuth } from '@/hooks/use-auth';
import { useTranslation } from '@/i18n/core';
import { CredentialsForm } from '@/features/dutybot/credentials-form';

export default function LoginForm() {
  const { login } = useAuth();
  const { t } = useTranslation();
  const next = useSearchParams().get('next');
  const destination =
    next?.startsWith('/dashboard/') && !next.includes('\\')
      ? next
      : '/dashboard/agents';
  return (
    <CredentialsForm
      title={t('dutybot.signIn')}
      action={t('dutybot.login')}
      submit={login}
      destination={destination}
    >
      <Link className="gg-link" data-testid="register-link" href="/register">
        {t('dutybot.register')}
      </Link>
    </CredentialsForm>
  );
}
