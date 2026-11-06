'use client';

import { useState } from 'react';
import { z } from 'zod';
import { useAuth } from '@/hooks/use-auth';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { getErrorMessage } from '@/lib/error-message';
import { useResource } from './use-resource';
import { Notice } from './ui';
import { AdminResources } from './admin-resources';
import {
  adminAccountsSchema,
  registrationSchema,
  deletionPreviewSchema,
} from './schemas';

const resourceLabels: Record<string, string> = {
  agents: '私有 Agent',
  instances: '实例',
  sessions: '会话',
  messages: '消息',
  confluence_tasks: '导出任务',
  imports: '已导入批次',
  crawl_tasks: '爬虫任务',
  crawl_runs: '运行报告',
  tokens: 'API token',
};
type Preview = z.infer<typeof deletionPreviewSchema>;

/** 管理员操作始终经独立受限 API；删除需先查看清单再确认。 */
export function Administration() {
  const { user } = useAuth();
  const permitted = user?.is_admin === true;
  const users = useResource(
    permitted ? '/admin/users' : null,
    adminAccountsSchema,
    null,
  );
  const settings = useResource(
    permitted ? '/admin/settings' : null,
    registrationSchema,
    null,
  );
  const [selected, setSelected] = useState('');
  const [registrationOverride, setRegistrationOverride] = useState<
    boolean | null
  >(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const current = users.data?.data.find((row) => row.id === selected);
  const base = `/admin/users/${selected}`;
  async function action(work: () => Promise<void>) {
    setBusy(true);
    setError('');
    try {
      await work();
      users.refresh();
      settings.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  if (!permitted)
    return <p data-testid="admin-forbidden">仅管理员可访问账户管理。</p>;
  return (
    <div className="grid gap-6">
      <h1>账户管理</h1>
      <Notice
        error={error || users.error || settings.error}
        loading={busy || users.loading || settings.loading}
      />
      <Button
        data-testid="admin-refresh"
        variant="outline"
        onClick={() => {
          users.refresh();
          settings.refresh();
          setRegistrationOverride(null);
        }}
      >
        刷新
      </Button>
      {settings.data && (
        <label>
          <input
            type="checkbox"
            data-testid="admin-registration"
            checked={registrationOverride ?? settings.data.registration_enabled}
            disabled={busy}
            onChange={(e) => {
              const enabled = e.target.checked;
              setRegistrationOverride(enabled);
              void action(async () => {
                try {
                  await api.put('/admin/settings', {
                    registration_enabled: enabled,
                  });
                } catch (cause) {
                  setRegistrationOverride(null);
                  throw cause;
                }
              });
            }}
          />
          允许公开注册
        </label>
      )}
      <form
        data-testid="admin-create-form"
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const form = e.currentTarget;
          const data = new FormData(form);
          void action(async () => {
            await api.post('/admin/users', {
              email: String(data.get('email')),
              password: String(data.get('password')),
            });
            form.reset();
          });
        }}
      >
        <label>
          邮箱
          <Input
            name="email"
            type="email"
            data-testid="admin-create-email"
            required
          />
        </label>
        <label>
          临时密码
          <Input
            name="password"
            type="password"
            autoComplete="new-password"
            minLength={6}
            maxLength={72}
            data-testid="admin-create-password"
            required
          />
        </label>
        <Button disabled={busy} data-testid="admin-create-submit">
          添加账户
        </Button>
      </form>
      <ul>
        {users.data?.data.map((row) => (
          <li key={row.id} className="border-b py-2">
            <Button
              variant="ghost"
              data-testid={`admin-user-${row.id}`}
              onClick={() => {
                setSelected(row.id);
                setPreview(null);
                setError('');
              }}
            >
              {row.email} · {row.is_admin ? '管理员' : row.status}
              {row.must_change_password ? ' · 待改密' : ''}
            </Button>
          </li>
        ))}
      </ul>
      {current && (
        <section key={selected} className="grid gap-4">
          <h2>{current.email}</h2>
          {!current.is_admin && (
            <>
              <div className="flex gap-3">
                <Button
                  disabled={busy || current.status === 'deleting'}
                  data-testid="admin-status"
                  onClick={() =>
                    void action(async () => {
                      await api.put(`${base}/status`, {
                        status:
                          current.status === 'active' ? 'disabled' : 'active',
                      });
                      setPreview(null);
                    })
                  }
                >
                  {current.status === 'active' ? '禁用账户' : '启用账户'}
                </Button>
                <Button
                  variant="destructive"
                  disabled={busy}
                  data-testid="admin-delete-preview"
                  onClick={() =>
                    void action(async () => {
                      const response = await api.get(
                        `${base}/deletion-preview`,
                      );
                      setPreview(deletionPreviewSchema.parse(response.data));
                    })
                  }
                >
                  检查删除与资源转移
                </Button>
              </div>
              <form
                data-testid="admin-reset-form"
                className="flex gap-3"
                onSubmit={(e) => {
                  e.preventDefault();
                  const form = e.currentTarget;
                  const data = new FormData(form);
                  void action(async () => {
                    await api.post(`${base}/password`, {
                      password: String(data.get('password')),
                    });
                    form.reset();
                    setPreview(null);
                  });
                }}
              >
                <label>
                  新临时密码
                  <Input
                    name="password"
                    type="password"
                    autoComplete="new-password"
                    minLength={6}
                    maxLength={72}
                    data-testid="admin-reset-password"
                    required
                  />
                </label>
                <Button
                  disabled={busy || current.status === 'deleting'}
                  data-testid="admin-reset-submit"
                >
                  重置密码
                </Button>
              </form>
            </>
          )}
          {preview && (
            <section
              className="border p-4 grid gap-3"
              data-testid="admin-deletion-preview"
            >
              <h3>删除账户，资源转给管理员</h3>
              <p>
                工作将停止；密码、token
                和外部授权不转移。失败时账户保持冻结，可重新检查后重试。
              </p>
              <dl>
                {Object.entries(preview.counts).map(([label, count]) => (
                  <div key={label}>
                    {resourceLabels[label]}: {count}
                  </div>
                ))}
              </dl>
              <p>
                新增 {preview.library.added}，更新 {preview.library.updated}
                ，保留 {preview.library.kept}，冲突 {preview.library.conflicts}
              </p>
              <ul>
                {preview.library.items.map((item) => (
                  <li key={`${item.space_id}/${item.page_id}`}>
                    {item.path}：{item.reason}
                  </li>
                ))}
              </ul>
              <Button
                variant="destructive"
                disabled={busy}
                data-testid="admin-delete-confirm"
                onClick={() =>
                  void action(async () => {
                    await api.post(`${base}/delete`, {
                      digest: preview.digest,
                    });
                    setSelected('');
                    setPreview(null);
                  })
                }
              >
                确认删除并转移资源
              </Button>
              <Button
                variant="outline"
                data-testid="admin-delete-cancel"
                onClick={() => setPreview(null)}
              >
                取消
              </Button>
            </section>
          )}
          <AdminResources userID={selected} />
        </section>
      )}
    </div>
  );
}
