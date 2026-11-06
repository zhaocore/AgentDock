'use client';

import Link from 'next/link';
import { useResource } from '@/features/agentdock/use-resource';
import { registrationSchema } from '@/features/agentdock/schemas';
import { useSearchParams } from 'next/navigation';
import { useAuth } from '@/hooks/use-auth';
import { useTranslation } from '@/i18n/core';
import { CredentialsForm } from '@/features/agentdock/credentials-form';

export default function LoginForm() {
  const { login } = useAuth();
  const registration = useResource(
    '/auth/registration',
    registrationSchema,
    null,
  );
  const { t } = useTranslation();
  const next = useSearchParams().get('next');
  const destination =
    next?.startsWith('/dashboard/') && !next.includes('\\')
      ? next
      : '/dashboard/agents';
  return (
    <CredentialsForm
      title={t('agentdock.signIn')}
      action={t('agentdock.login')}
      submit={login}
      destination={destination}
    >
      {registration.data?.registration_enabled && (
        <Link className="gg-link" data-testid="register-link" href="/register">
          {t('agentdock.register')}
        </Link>
      )}
    </CredentialsForm>
  );
}
