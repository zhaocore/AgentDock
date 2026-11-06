'use client';

import { useState } from 'react';
import { useAuth } from '@/hooks/use-auth';
import { api } from '@/utils/api';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Notice } from './ui';
import { getErrorMessage } from '@/lib/error-message';

/** 临时密码账户只呈现改密表单，成功刷新服务端账户状态。 */
export function ForcePasswordChange() {
  const { refreshUser } = useAuth();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <form
      data-testid="force-password-form"
      className="grid gap-4 max-w-md"
      onSubmit={async (e) => {
        e.preventDefault();
        const values = new FormData(e.currentTarget);
        setBusy(true);
        setError('');
        try {
          await api.post('/auth/change-password', {
            old_password: String(values.get('old')),
            new_password: String(values.get('new')),
          });
          await refreshUser();
        } catch (cause) {
          setError(getErrorMessage(cause));
        } finally {
          setBusy(false);
        }
      }}
    >
      <h1>首次登录：修改临时密码</h1>
      <Notice error={error} loading={busy} />
      <label>
        临时密码
        <Input
          name="old"
          type="password"
          autoComplete="current-password"
          data-testid="force-password-old"
          required
        />
      </label>
      <label>
        新密码
        <Input
          name="new"
          type="password"
          autoComplete="new-password"
          minLength={6}
          maxLength={72}
          data-testid="force-password-new"
          required
        />
      </label>
      <Button disabled={busy} data-testid="force-password-submit">
        修改密码并继续
      </Button>
    </form>
  );
}
