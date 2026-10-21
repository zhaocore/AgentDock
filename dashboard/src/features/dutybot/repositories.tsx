'use client';

import { useState } from 'react';
import { Boxes, GitBranch, Plus, RefreshCw, RotateCw } from 'lucide-react';
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
import { repositoryPageSchema } from './schemas';
import { useResource } from './use-resource';
import { DeleteButton, Notice, Pagination, Status } from './ui';

/** 克隆进行中时轮询刷新（必须是模块级稳定引用，避免 useResource 依赖抖动）。 */
function pollWhileCloning(data: { data: { status: string }[] }): boolean {
  return data.data.some(
    (repo) => repo.status === 'pending' || repo.status === 'cloning',
  );
}

/** 仓库管理：全局共享资源，登录即可见；添加/同步触发后台 git 任务。 */
export function Repositories() {
  const { t } = useTranslation();
  const resource = useResource(
    '/repositories?limit=20&offset=0',
    repositoryPageSchema,
    pollWhileCloning,
  );
  const [createOpen, setCreateOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [form, setForm] = useState({
    name: '',
    git_url: '',
    repo_type: 'frontend',
    branch: 'main',
  });
  const [offset, setOffset] = useState(0);
  // 分页参数变化重新拉取
  const paged = useResource(
    `/repositories?limit=20&offset=${offset}`,
    repositoryPageSchema,
    pollWhileCloning,
  );
  const view = offset === 0 ? resource : paged;

  const create = async () => {
    setBusy(true);
    setError('');
    try {
      await api.post('/repositories', form);
      setCreateOpen(false);
      setForm({ name: '', git_url: '', repo_type: 'frontend', branch: 'main' });
      resource.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };

  const sync = async (id: string) => {
    try {
      await api.post(`/repositories/${id}/sync`);
      resource.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    }
  };

  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('dutybot.repositories')}</h1>
        <div className="flex gap-2">
          <Button
            size="icon"
            variant="outline"
            title={t('dutybot.refresh')}
            aria-label={t('dutybot.refresh')}
            data-testid="repo-refresh"
            onClick={resource.refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
          <Button
            data-testid="repo-create-open"
            onClick={() => setCreateOpen(true)}
          >
            <Plus className="size-4 mr-2" />
            {t('dutybot.repoCreate')}
          </Button>
        </div>
      </div>
      <Notice error={error || view.error} loading={view.loading} />
      {view.data && !view.data.data.length && (
        <p
          className="py-10 text-center text-muted-foreground"
          data-testid="empty-state"
        >
          {t('dutybot.empty')}
        </p>
      )}
      {view.data && view.data.data.length > 0 && (
        <div className="gg-table-wrap">
          <table className="gg-table" data-testid="repo-table">
            <thead>
              <tr>
                <th>{t('dutybot.name')}</th>
                <th>{t('dutybot.gitUrl')}</th>
                <th>{t('dutybot.repoType')}</th>
                <th>{t('dutybot.repoBranch')}</th>
                <th>{t('dutybot.status')}</th>
                <th>{t('dutybot.repoLocalPath')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {view.data.data.map((repo) => (
                <tr key={repo.id}>
                  <td data-testid={`repo-name-${repo.id}`}>{repo.name}</td>
                  <td className="font-mono text-xs">{repo.git_url}</td>
                  <td>
                    {t(
                      `dutybot.repoType${repo.repo_type === 'rn' ? 'RN' : repo.repo_type === 'frontend' ? 'Frontend' : 'Backend'}`,
                    )}
                  </td>
                  <td>
                    <span className="inline-flex items-center gap-1">
                      <GitBranch className="size-3" />
                      {repo.branch}
                    </span>
                  </td>
                  <td>
                    <Status value={repo.status} />
                    {repo.status === 'failed' && repo.last_error && (
                      <p
                        className="text-xs text-red-600 mt-1 break-all"
                        data-testid={`repo-error-${repo.id}`}
                      >
                        {repo.last_error}
                      </p>
                    )}
                  </td>
                  <td className="font-mono text-xs">
                    {repo.local_path ?? '—'}
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <Button
                      size="sm"
                      variant="outline"
                      data-testid={`repo-sync-${repo.id}`}
                      onClick={() => void sync(repo.id)}
                    >
                      <RotateCw className="size-3 mr-1" />
                      {t('dutybot.repoSync')}
                    </Button>{' '}
                    <DeleteButton
                      testId={`repo-delete-${repo.id}`}
                      label={repo.name}
                      onDelete={async () => {
                        await api.delete(`/repositories/${repo.id}`);
                        resource.refresh();
                      }}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <Pagination
            offset={offset}
            total={view.data.total}
            hasMore={view.data.has_more}
            onChange={setOffset}
          />
        </div>
      )}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Boxes className="size-5" />
              {t('dutybot.repoCreate')}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              data-testid="repo-form-name"
              placeholder={t('dutybot.name')}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
            <Input
              data-testid="repo-form-url"
              placeholder="https://git.example.com/team/repo.git"
              value={form.git_url}
              onChange={(e) => setForm({ ...form, git_url: e.target.value })}
            />
            <select
              data-testid="repo-form-type"
              className="gg-select"
              aria-label={t('dutybot.repoType')}
              value={form.repo_type}
              onChange={(e) => setForm({ ...form, repo_type: e.target.value })}
            >
              <option value="frontend">{t('dutybot.repoTypeFrontend')}</option>
              <option value="backend">{t('dutybot.repoTypeBackend')}</option>
              <option value="rn">{t('dutybot.repoTypeRN')}</option>
            </select>
            <Input
              data-testid="repo-form-branch"
              placeholder={t('dutybot.repoBranch')}
              value={form.branch}
              onChange={(e) => setForm({ ...form, branch: e.target.value })}
            />
            {error && (
              <p role="alert" className="text-sm text-red-600">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setCreateOpen(false)}>
                {t('dutybot.cancel')}
              </Button>
              <Button
                data-testid="repo-form-submit"
                disabled={busy || !form.name || !form.git_url}
                onClick={() => void create()}
              >
                {t('dutybot.repoCreate')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
