'use client';

import { useState } from 'react';
import { KeyRound, RefreshCw } from 'lucide-react';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useAuth } from '@/hooks/use-auth';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';
import { healthSchema } from '@/features/agentdock/schemas';
import { useResource } from '@/features/agentdock/use-resource';
import { Notice, Status } from '@/features/agentdock/ui';

export default function Settings() {
  const { user } = useAuth();
  const { t } = useTranslation();
  const health = useResource('/health', healthSchema, null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  return (
    <>
      <h1>{t('agentdock.settings')}</h1>
      <p className="text-sm">{user?.email}</p>
      <section className="border-t pt-6 grid gap-4">
        <h2>{t('agentdock.changePassword')}</h2>
        <form
          className="max-w-md grid gap-4"
          data-testid="password-form"
          onSubmit={async (event) => {
            event.preventDefault();
            const form = event.currentTarget;
            const values = new FormData(form);
            setBusy(true);
            setError('');
            setSaved(false);
            try {
              await api.post('/auth/change-password', {
                old_password: String(values.get('old')),
                new_password: String(values.get('new')),
              });
              form.reset();
              setSaved(true);
            } catch (cause) {
              setError(getErrorMessage(cause));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label className="gg-field">
            {t('agentdock.oldPassword')}
            <Input
              name="old"
              type="password"
              autoComplete="current-password"
              required
            />
          </label>
          <label className="gg-field">
            {t('agentdock.newPassword')}
            <Input
              name="new"
              type="password"
              autoComplete="new-password"
              minLength={6}
              required
            />
          </label>
          <Notice error={error} loading={false} />
          {saved && (
            <p role="status" className="text-emerald-600">
              {t('agentdock.saved')}
            </p>
          )}
          <Button disabled={busy}>
            <KeyRound className="size-4 mr-2" />
            {t('agentdock.changePassword')}
          </Button>
        </form>
      </section>
      <section className="border-t pt-6 grid gap-4">
        <div className="gg-toolbar">
          <h2>{t('agentdock.health')}</h2>
          <Button
            variant="outline"
            size="icon"
            aria-label={t('agentdock.refresh')}
            title={t('agentdock.refresh')}
            data-testid="health-refresh"
            onClick={health.refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
        </div>
        <Notice error={health.error} loading={health.loading} />
        {health.data && (
          <dl className="gg-detail">
            <dt>MySQL</dt>
            <dd data-testid="health-mysql">
              <Status value={health.data.mysql} />
            </dd>
            <dt>Redis</dt>
            <dd data-testid="health-redis">
              <Status value={health.data.redis} />
            </dd>
          </dl>
        )}
      </section>
    </>
  );
}
