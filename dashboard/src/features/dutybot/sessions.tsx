'use client';

import { useState } from 'react';
import { MessageSquare, Plus, RefreshCw, Send } from 'lucide-react';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';
import { messageListSchema, sessionPageSchema } from './schemas';
import { useResource } from './use-resource';
import { DeleteButton, Notice, Pagination } from './ui';

/** 会话历史：按当前用户隔离；列表 + 消息查看 + 发送。 */
export function Sessions() {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);
  const resource = useResource(
    `/sessions?limit=20&offset=${offset}`,
    sessionPageSchema,
    null,
  );
  // 选中会话的消息流
  const [selected, setSelected] = useState('');
  const messages = useResource(
    selected ? `/sessions/${selected}/messages` : null,
    messageListSchema,
    null,
  );
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const send = async () => {
    if (!selected || !input.trim()) return;
    setBusy(true);
    setError('');
    try {
      await api.post(`/sessions/${selected}/messages`, { content: input });
      setInput('');
      messages.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('dutybot.sessions')}</h1>
        <div className="flex gap-2">
          <Button
            size="icon"
            variant="outline"
            title={t('dutybot.refresh')}
            aria-label={t('dutybot.refresh')}
            data-testid="session-refresh"
            onClick={resource.refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
        </div>
      </div>
      <Notice error={error || resource.error} loading={resource.loading} />
      {resource.data && (
        <div className="gg-table-wrap">
          <table className="gg-table" data-testid="session-table">
            <thead>
              <tr>
                <th>{t('dutybot.name')}</th>
                <th>{t('dutybot.status')}</th>
                <th>{t('dutybot.createdAt')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {resource.data.data.map((session) => (
                <tr key={session.id}>
                  <td>
                    <button
                      type="button"
                      data-testid={`session-open-${session.id}`}
                      className="text-left hover:underline"
                      onClick={() =>
                        setSelected(session.id === selected ? '' : session.id)
                      }
                    >
                      <MessageSquare className="inline size-3 mr-1" />
                      {session.title ?? session.id.slice(0, 8)}
                    </button>
                  </td>
                  <td className="text-xs text-muted-foreground">
                    {session.instance_id.slice(0, 8)}
                  </td>
                  <td className="text-xs">{session.updated_at}</td>
                  <td className="text-right">
                    <DeleteButton
                      testId={`session-delete-${session.id}`}
                      label={session.title ?? session.id}
                      onDelete={async () => {
                        await api.delete(`/sessions/${session.id}`);
                        if (selected === session.id) setSelected('');
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
            total={resource.data.total}
            hasMore={resource.data.has_more}
            onChange={setOffset}
          />
        </div>
      )}
      {selected && (
        <div className="mt-6" data-testid="session-messages">
          <h2 className="text-sm font-semibold mb-2">
            {t('dutybot.sessionMessages')}
          </h2>
          <Notice error={messages.error} loading={messages.loading} />
          <div className="space-y-3">
            {messages.data?.data.map((message) => (
              <div
                key={message.id}
                data-testid={`session-message-${message.id}`}
                className={
                  message.role === 'user'
                    ? 'ml-auto max-w-[80%] rounded-lg bg-emerald-50 p-3 text-sm dark:bg-emerald-950'
                    : 'mr-auto max-w-[80%] rounded-lg bg-muted p-3 text-sm'
                }
              >
                <p className="mb-1 text-xs font-medium text-muted-foreground">
                  {message.role === 'user'
                    ? t('dutybot.roleUser')
                    : t('dutybot.roleAssistant')}
                </p>
                <p className="whitespace-pre-wrap break-words">
                  {message.content}
                </p>
              </div>
            ))}
            {messages.data?.data.length === 0 && (
              <p className="text-sm text-muted-foreground">
                {t('dutybot.empty')}
              </p>
            )}
          </div>
          <form
            className="mt-4 flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <Input
              data-testid="session-message-input"
              placeholder={t('dutybot.messagePlaceholder')}
              value={input}
              onChange={(e) => setInput(e.target.value)}
            />
            <Button
              type="submit"
              data-testid="session-message-send"
              disabled={busy || !input.trim()}
            >
              <Send className="size-4" />
            </Button>
          </form>
        </div>
      )}
    </>
  );
}
