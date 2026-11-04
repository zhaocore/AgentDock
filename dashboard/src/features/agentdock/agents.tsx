'use client';

import { Fragment, useState } from 'react';
import { Plus, Play, Square, RefreshCw } from 'lucide-react';
import { api } from '@/utils/api';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';
import { agentPageSchema, instanceListSchema } from './schemas';
import { useResource } from './use-resource';
import { Notice, Pagination, Status } from './ui';

/** 公共预设只读展示、配置刷新与当前用户的实例启停。 */
export function Agents() {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);
  const resource = useResource(
    `/agents?limit=20&offset=${offset}`,
    agentPageSchema,
    null,
  );
  const [prompt, setPrompt] = useState<string | null>(null);
  const [expanded, setExpanded] = useState('');
  const [error, setError] = useState('');
  // 展开行按需加载实例列表
  const instances = useResource(
    expanded ? `/agents/${expanded}/instances` : null,
    instanceListSchema,
    null,
  );

  const createInstance = async (agentID: string) => {
    try {
      await api.post(`/agents/${agentID}/instances`, { repo_ids: [] });
      instances.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    }
  };

  const instanceAction = async (
    instanceID: string,
    action: 'start' | 'stop',
  ) => {
    try {
      await api.post(`/instances/${instanceID}/${action}`);
      instances.refresh();
    } catch (cause) {
      setError(getErrorMessage(cause));
    }
  };

  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('agentdock.agents')}</h1>
        <div className="flex gap-2">
          <Button
            size="icon"
            variant="outline"
            title={t('agentdock.refresh')}
            aria-label={t('agentdock.refresh')}
            data-testid="agent-refresh"
            onClick={resource.refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
        </div>
      </div>
      <p
        className="mb-2 text-sm text-muted-foreground"
        data-testid="agent-shared-description"
      >
        {t('agentdock.agentSharedDescription')}
      </p>
      <p
        className="mb-4 text-sm text-muted-foreground"
        data-testid="agent-mcp-status"
      >
        {t('agentdock.agentMcpPending')}
      </p>
      <Notice error={error || resource.error} loading={resource.loading} />
      {resource.data && (
        <div className="gg-table-wrap">
          <table className="gg-table" data-testid="agent-table">
            <thead>
              <tr>
                <th>{t('agentdock.name')}</th>
                <th>{t('agentdock.model')}</th>
                <th>{t('agentdock.skills')}</th>
                <th>{t('agentdock.mcpServers')}</th>
                <th>{t('agentdock.systemPrompt')}</th>
                <th>{t('agentdock.instances')}</th>
              </tr>
            </thead>
            <tbody>
              {resource.data.data.map((agent) => (
                <Fragment key={agent.id}>
                  <tr>
                    <td data-testid={`agent-name-${agent.id}`}>
                      {agent.name}
                      <span className="block text-xs text-muted-foreground">
                        {t(
                          agent.owner_user_id === ''
                            ? 'agentdock.agentPublic'
                            : 'agentdock.agentPrivate',
                        )}
                      </span>
                    </td>
                    <td className="font-mono text-xs">{agent.model}</td>
                    <td className="text-xs">
                      {agent.skills.join(', ') || '—'}
                    </td>
                    <td className="text-xs">
                      {agent.mcp_servers
                        .map((server) => server.name)
                        .join(', ') || '—'}
                    </td>
                    <td>
                      <Button
                        size="sm"
                        variant="ghost"
                        data-testid={`agent-prompt-${agent.id}`}
                        onClick={() => setPrompt(agent.system_prompt)}
                      >
                        {t('agentdock.systemPrompt')}
                      </Button>
                    </td>
                    <td>
                      <Button
                        size="sm"
                        variant="ghost"
                        data-testid={`agent-toggle-instances-${agent.id}`}
                        onClick={() =>
                          setExpanded(expanded === agent.id ? '' : agent.id)
                        }
                      >
                        {t('agentdock.instances')}
                      </Button>
                    </td>
                  </tr>
                  {expanded === agent.id && (
                    <tr key={`${agent.id}-instances`}>
                      <td colSpan={6} className="bg-muted/40 p-3">
                        <div className="flex items-center gap-2 mb-2">
                          <span className="text-sm font-medium">
                            {t('agentdock.instances')}
                          </span>
                          <Button
                            size="sm"
                            data-testid={`instance-create-${agent.id}`}
                            onClick={() => void createInstance(agent.id)}
                          >
                            <Plus className="size-3 mr-1" />
                            {t('agentdock.instanceCreate')}
                          </Button>
                        </div>
                        <Notice
                          error={instances.error}
                          loading={instances.loading}
                        />
                        {instances.data && (
                          <ul className="space-y-1 text-sm">
                            {instances.data.data.map((inst) => (
                              <li
                                key={inst.id}
                                className="flex items-center gap-2"
                                data-testid={`instance-row-${inst.id}`}
                              >
                                <span className="font-mono text-xs">
                                  {inst.id.slice(0, 8)}
                                </span>
                                <Status value={inst.status} />
                                {inst.status === 'running' ? (
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    data-testid={`instance-stop-${inst.id}`}
                                    onClick={() =>
                                      void instanceAction(inst.id, 'stop')
                                    }
                                  >
                                    <Square className="size-3 mr-1" />
                                    {t('agentdock.instanceStop')}
                                  </Button>
                                ) : (
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    data-testid={`instance-start-${inst.id}`}
                                    onClick={() =>
                                      void instanceAction(inst.id, 'start')
                                    }
                                  >
                                    <Play className="size-3 mr-1" />
                                    {t('agentdock.instanceStart')}
                                  </Button>
                                )}
                              </li>
                            ))}
                            {instances.data.data.length === 0 && (
                              <li className="text-muted-foreground">
                                {t('agentdock.empty')}
                              </li>
                            )}
                          </ul>
                        )}
                      </td>
                    </tr>
                  )}
                </Fragment>
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
      <Dialog
        open={prompt !== null}
        onOpenChange={(open) => {
          if (!open) setPrompt(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('agentdock.systemPrompt')}</DialogTitle>
          </DialogHeader>
          <pre
            className="max-h-96 overflow-auto whitespace-pre-wrap text-sm"
            data-testid="agent-prompt-content"
          >
            {prompt}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  );
}
