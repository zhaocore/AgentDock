'use client';

import { useEffect, useMemo, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/utils/api';
import { getErrorMessage } from '@/lib/error-message';
import { useTranslation } from '@/i18n/core';
import { useResource } from './use-resource';
import { Notice } from './ui';
import {
  confluenceTaskListSchema,
  confluenceTaskSchema,
  confluenceTreeSchema,
  markdownFileSchema,
  type ConfluenceNode,
} from './schemas';

/** 仅展开已打开分支，避免万页目录一次创建全部 DOM。 */
function TreeNode({
  node,
  published,
  onSelect,
}: {
  node: ConfluenceNode;
  published: Set<string>;
  onSelect: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  if (!node.children.length)
    return (
      <li>
        <button
          className="text-left py-1 disabled:text-muted-foreground hover:underline"
          data-testid={`confluence-page-${node.id}`}
          disabled={!published.has(node.id)}
          onClick={() => onSelect(node.id)}
        >
          {node.title}
        </button>
      </li>
    );
  return (
    <li>
      <button
        className="text-left py-1"
        data-testid={`confluence-folder-${node.id}`}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {open ? '▾' : '▸'} {node.title}
      </button>
      {open && (
        <ul className="pl-4 border-l">
          {node.children.map((child) => (
            <TreeNode
              key={child.id}
              node={child}
              published={published}
              onSelect={onSelect}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

/** 任务进度每十秒刷新；正文仅在选页时读取。 */
export function Confluence() {
  const { t } = useTranslation();
  const tasks = useResource(
    '/confluence-tasks',
    confluenceTaskListSchema,
    null,
  );
  const [selected, setSelected] = useState('');
  const id = selected || tasks.data?.data[0]?.id || '';
  const task = useResource(
    id ? `/confluence-tasks/${id}` : null,
    confluenceTaskSchema,
    null,
  );
  const tree = useResource(
    task.data?.stage === 'export' ? `/confluence-tasks/${id}/tree` : null,
    confluenceTreeSchema,
    null,
  );
  const [page, setPage] = useState('');
  const fileBase = `/confluence-tasks/${id}/pages/${page}/file`;
  const markdown = useResource(
    id && page ? `${fileBase}?name=page.md` : null,
    markdownFileSchema,
    null,
  );
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [auto, setAuto] = useState(true);
  useEffect(() => {
    setPage('');
  }, [id]);
  const refreshTasks = tasks.refresh;
  const refreshTask = task.refresh;
  useEffect(() => {
    if (!auto) return;
    const timer = setInterval(() => {
      refreshTasks();
      refreshTask();
    }, 10_000);
    return () => clearInterval(timer);
  }, [auto, refreshTasks, refreshTask]);
  const published = useMemo(
    () =>
      new Set(
        tree.data?.pages
          .slice(0, task.data?.completed ?? 0)
          .map((item) => item.id),
      ),
    [tree.data, task.data?.completed],
  );
  const perform = async (path: string, body: { url: string } | null) => {
    setBusy(true);
    setError('');
    try {
      const response = await api.post(path, body);
      const updated = confluenceTaskSchema.parse(response.data);
      setSelected(updated.id);
      task.refresh();
      tasks.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  const active = task.data && ['queued', 'running'].includes(task.data.status);
  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('dutybot.confluence')}</h1>
        <Button
          data-testid="confluence-refresh"
          variant="outline"
          onClick={() => {
            tasks.refresh();
            task.refresh();
            tree.refresh();
          }}
        >
          {t('dutybot.refresh')}
        </Button>
      </div>
      <form
        className="flex gap-3 mb-4"
        onSubmit={(event) => {
          event.preventDefault();
          void perform('/confluence-tasks', { url });
        }}
      >
        <Input
          type="url"
          required
          aria-label="Confluence URL"
          data-testid="confluence-url"
          placeholder="https://confluence.example.com/pages/viewpage.action?pageId=123"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
        />
        <Button data-testid="confluence-start" disabled={busy || !url}>
          {t('dutybot.confluenceStart')}
        </Button>
      </form>
      <Notice
        error={
          error || tasks.error || task.error || tree.error || markdown.error
        }
        loading={tasks.loading}
      />
      <label className="flex items-center gap-2 mb-4">
        <input
          type="checkbox"
          data-testid="confluence-auto"
          checked={auto}
          onChange={(event) => setAuto(event.target.checked)}
        />
        {t('dutybot.confluenceAuto')}
      </label>
      {tasks.data && tasks.data.data.length > 0 && (
        <select
          className="gg-select mb-4"
          aria-label={t('dutybot.confluenceTasks')}
          data-testid="confluence-tasks"
          value={id}
          onChange={(event) => setSelected(event.target.value)}
        >
          {tasks.data.data.map((item) => (
            <option key={item.id} value={item.id}>
              {item.created_at} · {item.url}
            </option>
          ))}
        </select>
      )}
      {task.data && (
        <div
          className="rounded border p-4 mb-4 space-y-2"
          data-testid="confluence-progress"
        >
          <p>
            {t(
              task.data.stage === 'tree'
                ? 'dutybot.confluenceTreeStage'
                : 'dutybot.confluenceExportStage',
            )}{' '}
            · {t(`dutybot.confluence_${task.data.status}`)}
          </p>
          <p>
            {task.data.completed}/{task.data.total}
          </p>
          <progress
            className="w-full"
            aria-label={t('dutybot.confluenceProgress')}
            value={task.data.completed}
            max={Math.max(1, task.data.total)}
          />
          {task.data.error && (
            <p role="alert" className="text-red-600">
              {task.data.error}
            </p>
          )}
          {active && (
            <Button
              data-testid="confluence-stop"
              variant="outline"
              disabled={busy}
              onClick={() => void perform(`/confluence-tasks/${id}/stop`, null)}
            >
              {t('dutybot.confluenceStop')}
            </Button>
          )}
          {['stopped', 'failed'].includes(task.data.status) && (
            <Button
              data-testid="confluence-resume"
              disabled={busy}
              onClick={() =>
                void perform(`/confluence-tasks/${id}/start`, null)
              }
            >
              {t('dutybot.confluenceResume')}
            </Button>
          )}
        </div>
      )}
      <div className="grid gap-4 lg:grid-cols-[minmax(240px,1fr)_3fr]">
        <aside
          className="border rounded p-4 overflow-auto max-h-[70vh]"
          data-testid="confluence-tree"
        >
          <h2 className="font-semibold mb-2">{t('dutybot.confluenceFiles')}</h2>
          {tree.data && (
            <ul>
              {tree.data.nodes.map((node) => (
                <TreeNode
                  key={`${id}-${node.id}`}
                  node={node}
                  published={published}
                  onSelect={setPage}
                />
              ))}
            </ul>
          )}
        </aside>
        <article
          className="border rounded p-4 overflow-auto prose dark:prose-invert max-w-none"
          data-testid="confluence-markdown"
        >
          {page && (
            <p className="text-sm text-muted-foreground">
              {tree.data?.pages.find((item) => item.id === page)?.path}
            </p>
          )}
          {markdown.data && (
            <ReactMarkdown
              skipHtml
              remarkPlugins={[remarkGfm]}
              urlTransform={(value, key) => {
                if (key === 'src')
                  return /^\.\/assets\/asset-[a-f0-9]{24}\.[a-z0-9]{1,8}$/.test(
                    value,
                  )
                    ? `/api/dutybot${fileBase}?name=${encodeURIComponent(value.slice(2))}`
                    : '';
                if (value.startsWith('./assets/'))
                  return `/api/dutybot${fileBase}?name=${encodeURIComponent(value.slice(2))}`;
                return /^(https?:\/\/|#)/.test(value) ? value : '';
              }}
            >
              {markdown.data}
            </ReactMarkdown>
          )}
        </article>
      </div>
    </>
  );
}
