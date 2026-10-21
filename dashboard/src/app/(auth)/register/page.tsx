'use client';

import Link from 'next/link';
import { useAuth } from '@/hooks/use-auth';
import { useTranslation } from '@/i18n/core';
import { CredentialsForm } from '@/features/dutybot/credentials-form';

export default function RegisterPage() {
  const { register } = useAuth();
  const { t } = useTranslation();
  return (
    <CredentialsForm
      title={t('dutybot.signUp')}
      action={t('dutybot.register')}
      submit={register}
      destination="/dashboard/agents"
    >
      <Link className="gg-link" href="/login">
        {t('dutybot.login')}
      </Link>
    </CredentialsForm>
  );
}
