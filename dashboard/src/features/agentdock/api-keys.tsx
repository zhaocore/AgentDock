'use client';

import { Fragment, useState } from 'react';
import { ClipboardCopy, KeyRound, Plus, RefreshCw } from 'lucide-react';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';
import {
  apiTokenListSchema,
  createApiTokenResponseSchema,
  type CreateApiTokenResponse,
} from './schemas';
import { useResource } from './use-resource';
import { DeleteButton, Notice } from './ui';

const SCOPE_ALL = 'all';

export function ApiKeys() {
  const { t, locale } = useTranslation();
  const resource = useResource('/api-tokens', apiTokenListSchema, null);
  const [createOpen, setCreateOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // 创建成功后一次性展示密钥本体
  const [created, setCreated] = useState<CreateApiTokenResponse | null>(null);
  const [copied, setCopied] = useState(false);
  // 范围固定为 all（资源全局共享 / 按用户隔离）

  const copyToken = async () => {
    if (!created) return;
    await navigator.clipboard.writeText(created.token);
    setCopied(true);
  };

  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('agentdock.apiKeys')}</h1>
        <div className="flex gap-2">
          <Button
            size="icon"
            variant="outline"
            title={t('agentdock.refresh')}
            aria-label={t('agentdock.refresh')}
            onClick={resource.refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
          <Button
            data-testid="apikey-create-open"
            onClick={() => setCreateOpen(true)}
          >
            <Plus className="size-4 mr-2" />
            {t('agentdock.apiTokenCreate')}
          </Button>
        </div>
      </div>
      <Notice error={resource.error} loading={resource.loading} />
      {resource.data && (
        <div className="gg-table-wrap">
          <table className="gg-table" data-testid="apikey-table">
            <thead>
              <tr>
                <th>{t('agentdock.apiTokenLabel')}</th>
                <th>{t('agentdock.apiTokenKey')}</th>
                <th>{t('agentdock.apiTokenScope')}</th>
                <th>{t('agentdock.apiTokenLastUsed')}</th>
                <th>{t('agentdock.apiTokenExpires')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {resource.data.data.map((token) => (
                <Fragment key={token.id}>
                  <tr>
                    <td data-testid={`apikey-label-${token.id}`}>
                      {token.label}
                    </td>
                    <td className="font-mono">{token.token_prefix}…</td>
                    <td>
                      {token.scopes.map((scope) =>
                        scope.kb_id === SCOPE_ALL ? (
                          <span key={scope.kb_id}>
                            {t('agentdock.apiTokenScopeAll')}
                          </span>
                        ) : (
                          <span key={scope.kb_id} className="font-mono text-xs">
                            {scope.kb_id}
                          </span>
                        ),
                      )}
                      {' · '}
                      {token.scopes.some((scope) => scope.can_write)
                        ? `${t('agentdock.apiTokenRead')} + ${t('agentdock.apiTokenWrite')}`
                        : t('agentdock.apiTokenRead')}
                      {token.revoked_at &&
                        ` · ${t('agentdock.apiTokenRevoked')}`}
                    </td>
                    <td className="whitespace-nowrap text-muted-foreground">
                      {token.last_used_at
                        ? new Date(token.last_used_at).toLocaleString(locale)
                        : '—'}
                    </td>
                    <td className="whitespace-nowrap text-muted-foreground">
                      {token.expires_at
                        ? new Date(token.expires_at).toLocaleString(locale)
                        : t('agentdock.apiTokenExpiresNever')}
                    </td>
                    <td>
                      <div className="flex gap-2">
                        {!token.revoked_at && (
                          <DeleteButton
                            label={token.label}
                            testId={`apikey-revoke-${token.id}`}
                            onDelete={async () => {
                              await api.delete(`/api-tokens/${token.id}`);
                              resource.refresh();
                            }}
                          />
                        )}
                      </div>
                    </td>
                  </tr>
                </Fragment>
              ))}
            </tbody>
          </table>
          {!resource.data.data.length && (
            <p
              className="py-10 text-center text-muted-foreground"
              data-testid="empty-state"
            >
              {t('agentdock.apiTokenEmpty')}
            </p>
          )}
        </div>
      )}

      {/* 创建对话框 */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('agentdock.apiTokenCreate')}</DialogTitle>
          </DialogHeader>
          <form
            className="grid gap-4"
            onSubmit={async (event) => {
              event.preventDefault();
              setBusy(true);
              setError('');
              const values = new FormData(event.currentTarget);
              const expires = String(values.get('expires') || '').trim();
              try {
                const response = await api.post('/api-tokens', {
                  label: String(values.get('label')).trim(),
                  kb_scope: 'all',
                  can_write: values.get('can_write') === 'on',
                  ...(expires
                    ? { expires_at: new Date(expires).toISOString() }
                    : {}),
                });
                setCreateOpen(false);
                setCreated(createApiTokenResponseSchema.parse(response.data));
                resource.refresh();
              } catch (cause) {
                setError(getErrorMessage(cause));
              } finally {
                setBusy(false);
              }
            }}
          >
            <label className="gg-field">
              {t('agentdock.apiTokenLabel')}
              <Input
                data-testid="apikey-label-input"
                name="label"
                required
                maxLength={200}
                placeholder={t('agentdock.apiTokenLabelPlaceholder')}
              />
            </label>
            <p className="gg-field text-sm text-muted-foreground">
              {t('agentdock.apiTokenScope')}: {t('agentdock.apiTokenScopeAll')}
            </p>
            <label className="gg-field flex items-center gap-2">
              <input
                type="checkbox"
                data-testid="apikey-can-write"
                name="can_write"
                className="size-4"
              />
              {t('agentdock.apiTokenWrite')}
            </label>
            <label className="gg-field">
              {t('agentdock.apiTokenExpires')}
              <Input
                data-testid="apikey-expires"
                name="expires"
                type="datetime-local"
              />
            </label>
            <Notice error={error} loading={false} />
            <Button data-testid="apikey-create-submit" disabled={busy}>
              <KeyRound className="size-4 mr-2" />
              {t('agentdock.apiTokenCreate')}
            </Button>
          </form>
        </DialogContent>
      </Dialog>

      {/* 一次性密钥展示 */}
      <Dialog open={created !== null} onOpenChange={() => setCreated(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('agentdock.apiKeys')}</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            {t('agentdock.apiTokenSaveWarning')}
          </p>
          <div className="flex items-center gap-2">
            <code
              data-testid="apikey-secret"
              className="flex-1 overflow-x-auto rounded bg-muted p-2 font-mono text-sm"
            >
              {created?.token}
            </code>
            <Button
              size="icon"
              variant="outline"
              data-testid="apikey-copy"
              title={t('agentdock.apiTokenCopy')}
              aria-label={t('agentdock.apiTokenCopy')}
              onClick={() => void copyToken()}
            >
              <ClipboardCopy className="size-4" />
            </Button>
          </div>
          {copied && (
            <p
              className="text-sm text-muted-foreground"
              data-testid="apikey-copied"
            >
              {t('agentdock.apiTokenCopied')}
            </p>
          )}
          <Button data-testid="apikey-done" onClick={() => setCreated(null)}>
            {t('agentdock.close')}
          </Button>
        </DialogContent>
      </Dialog>
    </>
  );
}
