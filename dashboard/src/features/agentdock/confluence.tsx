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
  scopeID,
  onScope,
  canSelectScope,
}: {
  node: ConfluenceNode;
  published: Set<string>;
  onSelect: (id: string) => void;
  scopeID: string;
  onScope: (node: ConfluenceNode) => void;
  canSelectScope: boolean;
}) {
  const [open, setOpen] = useState(false);
  const choice = (
    <input
      type="radio"
      name="confluence-scope"
      aria-label={node.title}
      data-testid={`confluence-scope-${node.id}`}
      checked={scopeID === node.id}
      disabled={!canSelectScope}
      onChange={() => onScope(node)}
      className="mr-2"
    />
  );
  if (!node.children.length)
    return (
      <li>
        {choice}
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
      {choice}
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
              scopeID={scopeID}
              onScope={onScope}
              canSelectScope={canSelectScope}
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
    task.data && task.data.stage !== 'tree'
      ? `/confluence-tasks/${id}/tree`
      : null,
    confluenceTreeSchema,
    null,
  );
  const [page, setPage] = useState('');
  const [scope, setScope] = useState({ id: '', title: '' });
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
    setScope({ id: '', title: '' });
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
  const perform = async (
    path: string,
    body: { url: string } | { node_id: string } | null,
  ) => {
    setBusy(true);
    setError('');
    try {
      const response = await api.post(path, body);
      const updated = confluenceTaskSchema.parse(response.data);
      setSelected(updated.id);
      task.refresh();
      tasks.refresh();
      tree.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  const active = task.data && ['queued', 'running'].includes(task.data.status);
  const canSelectScope =
    (task.data?.stage === 'selection' &&
      ['waiting', 'stopped', 'failed'].includes(task.data.status)) ||
    (task.data?.stage === 'markdown' && task.data.status === 'completed');
  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('agentdock.confluence')}</h1>
        <Button
          data-testid="confluence-refresh"
          variant="outline"
          onClick={() => {
            tasks.refresh();
            task.refresh();
            tree.refresh();
          }}
        >
          {t('agentdock.refresh')}
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
          {t('agentdock.confluenceStart')}
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
        {t('agentdock.confluenceAuto')}
      </label>
      {tasks.data && tasks.data.data.length > 0 && (
        <select
          className="gg-select mb-4"
          aria-label={t('agentdock.confluenceTasks')}
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
                ? 'agentdock.confluenceTreeStage'
                : task.data.stage === 'selection'
                  ? 'agentdock.confluenceSelectionStage'
                  : 'agentdock.confluenceExportStage',
            )}{' '}
            · {t(`agentdock.confluence_${task.data.status}`)}
          </p>
          <p>
            {task.data.completed}/{task.data.total}
          </p>
          <progress
            className="w-full"
            aria-label={t('agentdock.confluenceProgress')}
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
              {t('agentdock.confluenceStop')}
            </Button>
          )}
          {task.data.stage !== 'selection' &&
            ['stopped', 'failed'].includes(task.data.status) && (
              <Button
                data-testid="confluence-resume"
                disabled={busy}
                onClick={() =>
                  void perform(`/confluence-tasks/${id}/start`, null)
                }
              >
                {t('agentdock.confluenceResume')}
              </Button>
            )}
        </div>
      )}
      <div className="grid gap-4 lg:grid-cols-[minmax(240px,1fr)_3fr]">
        <aside
          className="border rounded p-4 overflow-auto max-h-[70vh]"
          data-testid="confluence-tree"
        >
          <h2 className="font-semibold mb-2">
            {t('agentdock.confluenceFiles')}
          </h2>
          {canSelectScope && tree.data && (
            <div className="space-y-2 mb-3">
              <p className="text-sm">
                {t('agentdock.confluenceSelectionHelp')}
              </p>
              {scope.id && (
                <p className="text-sm" data-testid="confluence-selected-scope">
                  {scope.title}
                </p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  data-testid="confluence-export-all"
                  disabled={busy}
                  onClick={() =>
                    void perform(`/confluence-tasks/${id}/export`, {
                      node_id: '',
                    })
                  }
                >
                  {t('agentdock.confluenceExportAll')}
                </Button>
                <Button
                  variant="outline"
                  data-testid="confluence-export-selected"
                  disabled={busy || !scope.id}
                  onClick={() =>
                    void perform(`/confluence-tasks/${id}/export`, {
                      node_id: scope.id,
                    })
                  }
                >
                  {t('agentdock.confluenceExportSelected')}
                </Button>
              </div>
            </div>
          )}
          {tree.data && (
            <ul>
              {tree.data.nodes.map((node) => (
                <TreeNode
                  key={`${id}-${node.id}`}
                  node={node}
                  published={published}
                  onSelect={setPage}
                  scopeID={
                    (canSelectScope ? scope.id : tree.data?.selected_node_id) ??
                    ''
                  }
                  onScope={(node) =>
                    setScope({ id: node.id, title: node.title })
                  }
                  canSelectScope={Boolean(canSelectScope) && !busy}
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
                    ? `/api/agentdock${fileBase}?name=${encodeURIComponent(value.slice(2))}`
                    : '';
                if (value.startsWith('./assets/'))
                  return `/api/agentdock${fileBase}?name=${encodeURIComponent(value.slice(2))}`;
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
