'use client';

import { useState } from 'react';
import { z } from 'zod';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/utils/api';
import { getErrorMessage } from '@/lib/error-message';
import { useTranslation } from '@/i18n/core';
import { Notice } from './ui';
import { useResource } from './use-resource';
import {
  agentPageSchema,
  crawlProfileListSchema,
  crawlTaskSchema,
  crawlTaskListSchema,
  crawlRunSchema,
  crawlRunListSchema,
  type CrawlTask,
} from './schemas';

const reportSchema = z.string().min(1);
const pollRuns = () => true;

/** 任务管理只选择获授权的登录配置，不读取部署凭据。 */
export function Crawl() {
  const { t } = useTranslation();
  const tasks = useResource('/crawl-tasks', crawlTaskListSchema, null);
  const profiles = useResource('/crawl-profiles', crawlProfileListSchema, null);
  const agents = useResource('/agents?limit=100', agentPageSchema, null);
  const [selected, setSelected] = useState('');
  const currentID = selected || tasks.data?.data[0]?.id || '';
  const runs = useResource(
    currentID ? `/crawl-tasks/${currentID}/runs` : null,
    crawlRunListSchema,
    pollRuns,
  );
  const [reportID, setReportID] = useState('');
  const report = useResource(
    reportID ? `/crawl-runs/${reportID}/report` : null,
    reportSchema,
    null,
  );
  const [editing, setEditing] = useState('');
  const [name, setName] = useState('');
  const [agentID, setAgentID] = useState('');
  const [profileID, setProfileID] = useState('');
  const [urls, setURLs] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const current = tasks.data?.data.find((task) => task.id === currentID);

  function edit(task: CrawlTask) {
    setEditing(task.id);
    setName(task.name);
    setAgentID(task.agent_id);
    setProfileID(task.profile_id);
    setURLs(task.urls.join('\n'));
    setEnabled(task.enabled);
  }
  async function save(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    try {
      const body = {
        name,
        agent_id: agentID,
        profile_id: profileID,
        urls: urls
          .split('\n')
          .map((value) => value.trim())
          .filter(Boolean),
        enabled,
      };
      const response = editing
        ? await api.put(`/crawl-tasks/${editing}`, body)
        : await api.post('/crawl-tasks', body);
      const task = crawlTaskSchema.parse(response.data);
      setSelected(task.id);
      setEditing('');
      setName('');
      setURLs('');
      tasks.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  async function run() {
    setBusy(true);
    setError('');
    try {
      crawlRunSchema.parse(
        (await api.post(`/crawl-tasks/${currentID}/runs`)).data,
      );
      runs.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  async function disable(task: CrawlTask) {
    setBusy(true);
    setError('');
    try {
      crawlTaskSchema.parse(
        (
          await api.put(`/crawl-tasks/${task.id}`, {
            name: task.name,
            agent_id: task.agent_id,
            profile_id: task.profile_id,
            urls: task.urls,
            enabled: false,
          })
        ).data,
      );
      tasks.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="space-y-6" data-testid="crawl-workspace">
      <h1 className="text-2xl font-semibold">{t('dutybot.crawl')}</h1>
      <p className="text-muted-foreground">{t('dutybot.crawlSchedule')}</p>
      <Notice
        error={
          error ||
          tasks.error ||
          profiles.error ||
          agents.error ||
          runs.error ||
          report.error
        }
        loading={tasks.loading || profiles.loading || agents.loading}
      />
      {profiles.data?.data.length === 0 && (
        <p data-testid="crawl-no-profile">{t('dutybot.crawlNoProfile')}</p>
      )}
      <form
        onSubmit={(event) => void save(event)}
        className="grid gap-3 max-w-2xl"
        data-testid="crawl-form"
      >
        <h2 className="text-lg font-medium">
          {editing ? t('dutybot.crawlEdit') : t('dutybot.crawlCreate')}
        </h2>
        <label>
          {t('dutybot.crawlName')}
          <Input
            data-testid="crawl-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            required
            maxLength={200}
          />
        </label>
        <label>
          Agent
          <select
            data-testid="crawl-agent"
            className="gg-select w-full"
            value={agentID}
            onChange={(event) => setAgentID(event.target.value)}
            required
          >
            <option value="">{t('dutybot.crawlChoose')}</option>
            {agents.data?.data.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t('dutybot.crawlProfile')}
          <select
            data-testid="crawl-profile"
            className="gg-select w-full"
            value={profileID}
            onChange={(event) => setProfileID(event.target.value)}
            required
          >
            <option value="">{t('dutybot.crawlChoose')}</option>
            {profiles.data?.data.map((p) => (
              <option key={p.id} value={p.id}>
                {p.id} — {p.origin}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t('dutybot.crawlURLs')}
          <textarea
            data-testid="crawl-urls"
            className="gg-select w-full min-h-32"
            value={urls}
            onChange={(event) => setURLs(event.target.value)}
            required
          />
        </label>
        <label>
          <input
            data-testid="crawl-enabled"
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />{' '}
          {t('dutybot.crawlEnabled')}
        </label>
        <div className="flex gap-2">
          <Button
            data-testid="crawl-save"
            type="submit"
            disabled={busy || !profiles.data?.data.length}
          >
            {t('dutybot.crawlSave')}
          </Button>
          {editing && (
            <Button
              type="button"
              data-testid="crawl-cancel-edit"
              variant="outline"
              onClick={() => {
                setEditing('');
                setName('');
                setURLs('');
              }}
            >
              {t('dutybot.close')}
            </Button>
          )}
        </div>
      </form>
      <div className="space-y-3">
        <label>
          {t('dutybot.crawlTasks')}
          <select
            data-testid="crawl-task"
            className="gg-select w-full"
            value={currentID}
            onChange={(event) => {
              setSelected(event.target.value);
              setReportID('');
            }}
          >
            <option value="">{t('dutybot.crawlChoose')}</option>
            {tasks.data?.data.map((task) => (
              <option key={task.id} value={task.id}>
                {task.name} —{' '}
                {task.enabled
                  ? t('dutybot.crawlEnabled')
                  : t('dutybot.crawlDisabled')}
              </option>
            ))}
          </select>
        </label>
        {current && (
          <div className="flex gap-2">
            <Button
              data-testid="crawl-run"
              disabled={
                busy ||
                runs.data?.data.some((run) =>
                  ['queued', 'running'].includes(run.status),
                )
              }
              onClick={() => void run()}
            >
              {t('dutybot.crawlRun')}
            </Button>
            <Button
              variant="outline"
              data-testid="crawl-edit"
              onClick={() => edit(current)}
            >
              {t('dutybot.crawlEdit')}
            </Button>
            <Button
              variant="outline"
              data-testid="crawl-disable"
              disabled={busy || !current.enabled}
              onClick={() => void disable(current)}
            >
              {t('dutybot.crawlDisable')}
            </Button>
          </div>
        )}
        <Button
          variant="outline"
          data-testid="crawl-refresh"
          onClick={() => {
            tasks.refresh();
            runs.refresh();
          }}
        >
          {t('dutybot.refresh')}
        </Button>
      </div>
      <div className="space-y-3" data-testid="crawl-runs">
        {runs.data?.data.map((run) => (
          <article
            key={run.id}
            className="border p-3 rounded-md"
            data-testid={`crawl-run-${run.id}`}
          >
            <p>
              {run.created_at} · {run.trigger} · {run.status} · {run.stage} ·{' '}
              {run.completed}/{run.total}
            </p>
            {run.error && (
              <p role="alert" className="text-red-600">
                {run.error}
              </p>
            )}
            {run.status === 'succeeded' && (
              <div className="flex gap-3">
                <Button
                  variant="outline"
                  data-testid={`crawl-open-${run.id}`}
                  onClick={() => setReportID(run.id)}
                >
                  {t('dutybot.crawlReport')}
                </Button>
                <a
                  data-testid={`crawl-download-${run.id}`}
                  href={`/api/dutybot/crawl-runs/${run.id}/report?download=1`}
                >
                  {t('dutybot.crawlDownload')}
                </a>
              </div>
            )}
          </article>
        ))}
      </div>
      {report.data && (
        <article
          data-testid="crawl-report"
          className="prose dark:prose-invert max-w-none"
        >
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            skipHtml
            disallowedElements={['img']}
            components={{
              a: ({ children, href }) => (
                <a href={href} target="_blank" rel="noopener noreferrer">
                  {children}
                </a>
              ),
            }}
          >
            {report.data}
          </ReactMarkdown>
        </article>
      )}
    </section>
  );
}
