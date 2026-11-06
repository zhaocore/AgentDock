'use client';

import { useState } from 'react';
import { z } from 'zod';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import { Notice } from './ui';
import { useResource } from './use-resource';
import { getErrorMessage } from '@/lib/error-message';
import {
  apiTokenListSchema,
  confluenceTaskListSchema,
  crawlTaskListSchema,
  crawlRunListSchema,
  confluenceLibrarySchema,
} from './schemas';

/** 他人资源仅提供查看、下载、停止与删除，不呈现启动或修改配置。 */
export function AdminResources({ userID }: { userID: string }) {
  const base = `/admin/users/${userID}`;
  const tokens = useResource(`${base}/api-tokens`, apiTokenListSchema, null);
  const exports = useResource(
    `${base}/confluence-tasks`,
    confluenceTaskListSchema,
    null,
  );
  const tasks = useResource(`${base}/crawl-tasks`, crawlTaskListSchema, null);
  const library = useResource(
    `${base}/confluence-library`,
    confluenceLibrarySchema,
    null,
  );
  const [task, setTask] = useState('');
  const [revision, setRevision] = useState(0);
  const runs = useResource(
    task ? `${base}/crawl-tasks/${task}/runs?revision=${revision}` : null,
    crawlRunListSchema,
    null,
  );
  const [content, setContent] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  async function act(work: () => Promise<void>) {
    setBusy(true);
    setError('');
    try {
      await work();
      tokens.refresh();
      exports.refresh();
      tasks.refresh();
      library.refresh();
      setRevision((v) => v + 1);
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  async function read(path: string) {
    const response = await api.get(path, { responseType: 'text' });
    setContent(z.string().parse(response.data));
  }
  return (
    <section className="grid gap-5">
      <h2>用户资源</h2>
      <Notice
        error={
          error ||
          tokens.error ||
          exports.error ||
          tasks.error ||
          library.error ||
          runs.error
        }
        loading={busy}
      />
      <Button
        variant="outline"
        data-testid="admin-resources-refresh"
        onClick={() => void act(async () => {})}
      >
        刷新资源
      </Button>
      <h3>API token</h3>
      <ul>
        {tokens.data?.data.map((token) => (
          <li key={token.id} className="flex gap-3 py-2">
            {token.label} · {token.token_prefix} ·{' '}
            {token.revoked_at ? '已撤销' : token.expires_at || '不过期'}
            <Button
              variant="outline"
              disabled={busy || token.revoked_at !== null}
              data-testid={`admin-revoke-${token.id}`}
              onClick={() =>
                void act(async () => {
                  await api.delete(`${base}/api-tokens/${token.id}`);
                })
              }
            >
              撤销
            </Button>
          </li>
        ))}
      </ul>
      <h3>Confluence 文件库</h3>
      <div className="flex gap-3">
        <Button
          disabled={busy || !library.data?.spaces.length}
          data-testid="admin-library-download"
          onClick={() =>
            void act(async () => {
              await api.get(`${base}/confluence-library/archive-check`);
              window.location.assign(
                `/api/agentdock${base}/confluence-library/archive`,
              );
            })
          }
        >
          下载整库 ZIP
        </Button>
        <Button
          variant="destructive"
          disabled={busy}
          data-testid="admin-library-delete"
          onClick={() => setConfirmation(`${base}/confluence-library`)}
        >
          删除文件库目录
        </Button>
      </div>
      {library.data?.spaces.map((space) => (
        <section key={space.id}>
          <h4>
            {space.key} · {space.base_url}
          </h4>
          <ul>
            {space.pages.map((page) => (
              <li key={page.id}>
                <Button
                  variant="ghost"
                  data-testid={`admin-library-page-${space.id}-${page.id}`}
                  onClick={() =>
                    void act(() =>
                      read(
                        `${base}/confluence-library/spaces/${space.id}/pages/${page.id}/file?name=page.md`,
                      ),
                    )
                  }
                >
                  {page.path} · v{page.version}
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ))}
      <h3>Confluence 导出任务</h3>
      <ul>
        {exports.data?.data.map((row) => (
          <li key={row.id} className="border-b py-2 grid gap-2">
            <p>
              {row.url} · {row.status} · {row.stage} · {row.completed}/
              {row.total} {row.error}
            </p>
            <div className="flex gap-3">
              <Button
                disabled={busy}
                variant="outline"
                data-testid={`admin-export-tree-${row.id}`}
                onClick={() =>
                  void act(async () => {
                    const response = await api.get(
                      `${base}/confluence-tasks/${row.id}/tree`,
                    );
                    setContent(JSON.stringify(response.data, null, 2));
                  })
                }
              >
                查看目录
              </Button>
              <Button
                disabled={busy}
                variant="outline"
                data-testid={`admin-export-stop-${row.id}`}
                onClick={() =>
                  void act(async () => {
                    await api.post(`${base}/confluence-tasks/${row.id}/stop`);
                  })
                }
              >
                停止
              </Button>
              <Button
                disabled={busy}
                variant="destructive"
                data-testid={`admin-export-delete-${row.id}`}
                onClick={() =>
                  setConfirmation(`${base}/confluence-tasks/${row.id}`)
                }
              >
                删除
              </Button>
            </div>
          </li>
        ))}
      </ul>
      <h3>爬虫任务</h3>
      <ul>
        {tasks.data?.data.map((row) => (
          <li key={row.id} className="border-b py-2">
            <p>
              {row.name} · {row.enabled ? '已启用' : '已停用'}
            </p>
            <p className="break-all">{row.urls.join('，')}</p>
            <div className="flex gap-3">
              <Button
                disabled={busy}
                variant="outline"
                data-testid={`admin-crawl-runs-${row.id}`}
                onClick={() => setTask(row.id)}
              >
                运行历史
              </Button>
              <Button
                disabled={busy}
                variant="outline"
                data-testid={`admin-crawl-stop-${row.id}`}
                onClick={() =>
                  void act(async () => {
                    await api.post(`${base}/crawl-tasks/${row.id}/stop`);
                  })
                }
              >
                停止
              </Button>
              <Button
                disabled={busy}
                variant="destructive"
                data-testid={`admin-crawl-delete-${row.id}`}
                onClick={() => setConfirmation(`${base}/crawl-tasks/${row.id}`)}
              >
                删除
              </Button>
            </div>
          </li>
        ))}
      </ul>
      <ul>
        {runs.data?.data.map((run) => (
          <li key={run.id} className="py-2">
            {run.created_at} · {run.status} · {run.completed}/{run.total}{' '}
            {run.error}
            <Button
              disabled={busy || !['succeeded', 'partial'].includes(run.status)}
              data-testid={`admin-report-${run.id}`}
              variant="ghost"
              onClick={() =>
                void act(() => read(`${base}/crawl-runs/${run.id}/report`))
              }
            >
              查看报告
            </Button>
            <a
              className="gg-link"
              data-testid={`admin-markdown-${run.id}`}
              href={`/api/agentdock${base}/crawl-runs/${run.id}/markdown`}
            >
              下载采集原文
            </a>
          </li>
        ))}
      </ul>
      {confirmation && (
        <section
          data-testid="admin-resource-confirmation"
          className="border p-4"
        >
          <p>确认删除所选资源？目录删除不清理共享原始对象。</p>
          <Button
            disabled={busy}
            variant="destructive"
            data-testid="admin-resource-delete-confirm"
            onClick={() =>
              void act(async () => {
                await api.delete(confirmation);
                setConfirmation('');
                setTask('');
                setContent('');
              })
            }
          >
            确认删除
          </Button>
          <Button
            variant="outline"
            data-testid="admin-resource-delete-cancel"
            onClick={() => setConfirmation('')}
          >
            取消
          </Button>
        </section>
      )}
      {content && (
        <pre
          data-testid="admin-resource-content"
          className="whitespace-pre-wrap break-all border p-4 max-h-[40rem] overflow-auto"
        >
          {content}
        </pre>
      )}
    </section>
  );
}
