import { z } from 'zod';

/** 爬虫任务与运行只暴露元数据，凭据由部署者维护。 */
export const crawlTaskSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  agent_id: z.string().uuid(),
  profile_id: z.string(),
  profile_ids: z.array(z.string()),
  hops: z.number().int().min(0).max(5),
  include_pattern: z.string(),
  urls: z.array(z.string().url()),
  enabled: z.boolean(),
  created_at: z.string(),
  updated_at: z.string(),
});
export type CrawlTask = z.infer<typeof crawlTaskSchema>;
export const crawlTaskListSchema = z.object({ data: z.array(crawlTaskSchema) });
export const crawlProfileListSchema = z.object({
  data: z.array(z.object({ id: z.string(), origin: z.string().url() })),
});
export const crawlRunSchema = z.object({
  id: z.string().uuid(),
  task_id: z.string().uuid(),
  task: crawlTaskSchema,
  trigger: z.enum(['manual', 'scheduled']),
  status: z.enum([
    'queued',
    'running',
    'succeeded',
    'partial',
    'failed',
    'skipped',
  ]),
  stage: z.string(),
  completed: z.number().int(),
  total: z.number().int(),
  error: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});
export const crawlRunListSchema = z.object({ data: z.array(crawlRunSchema) });

export const userSchema = z.object({
  is_admin: z.boolean(),
  must_change_password: z.boolean(),
  id: z.string().uuid(),
  email: z.string().email(),
  created_at: z.string(),
});
export type AuthUser = z.infer<typeof userSchema>;

/** 分页包装 */
export const pageSchema = <T extends z.ZodTypeAny>(item: T) =>
  z.object({
    data: z.array(item),
    total: z.number().int(),
    limit: z.number().int(),
    offset: z.number().int(),
    has_more: z.boolean(),
  });

/** 代码仓库（全局共享） */
export const repoStatuses = ['pending', 'cloning', 'ready', 'failed'] as const;
export const repositorySchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  git_url: z.string(),
  repo_type: z.enum(['frontend', 'backend', 'rn']),
  branch: z.string(),
  local_path: z.string().nullable(),
  status: z.enum(repoStatuses),
  last_error: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
});
export const repositoryPageSchema = pageSchema(repositorySchema);

/** Agent 定义 */
export const mcpServerSchema = z.object({
  name: z.string(),
  transport: z.string(),
  endpoint: z.string().optional(),
  command: z.string().optional(),
  env: z.record(z.string(), z.string()).optional(),
});
export const subagentSchema = z.object({
  name: z.string(),
  description: z.string(),
  tools: z.array(z.string()).optional(),
});
export const agentSchema = z.object({
  id: z.string().uuid(),
  owner_user_id: z.union([z.string().uuid(), z.literal('')]),
  name: z.string(),
  system_prompt: z.string(),
  model: z.string(),
  skills: z.array(z.string()),
  mcp_servers: z.array(mcpServerSchema),
  subagents: z.array(subagentSchema),
  created_at: z.string(),
  updated_at: z.string(),
});
export const agentPageSchema = pageSchema(agentSchema);

/** 实例 */
export const instanceStatuses = [
  'created',
  'running',
  'stopped',
  'failed',
] as const;
export const instanceSchema = z.object({
  id: z.string().uuid(),
  agent_id: z.string().uuid(),
  owner_user_id: z.string().uuid(),
  status: z.enum(instanceStatuses),
  repo_ids: z.array(z.string()),
  created_at: z.string(),
  updated_at: z.string(),
});
export const instanceListSchema = z.object({
  data: z.array(instanceSchema),
});

/** 会话与消息 */
export const sessionSchema = z.object({
  id: z.string().uuid(),
  user_id: z.string().uuid(),
  instance_id: z.string().uuid(),
  agent_id: z.string().uuid(),
  title: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
});
export const sessionPageSchema = pageSchema(sessionSchema);
export const messageSchema = z.object({
  id: z.string().uuid(),
  session_id: z.string().uuid(),
  role: z.enum(['user', 'assistant']),
  content: z.string(),
  agent_meta: z.record(z.string(), z.unknown()).nullable().optional(),
  created_at: z.string(),
});
export const messageListSchema = z.object({
  data: z.array(messageSchema),
});

/** 健康检查 */
export const healthSchema = z.object({
  status: z.string(),
  mysql: z.string(),
  mysql_error: z.string(),
  redis: z.string(),
  redis_error: z.string(),
});

/** API 令牌 */
export const apiTokenScopeSchema = z.object({
  kb_id: z.string(),
  can_write: z.boolean(),
});
export const apiTokenSchema = z.object({
  id: z.string().uuid(),
  tenant_id: z.string(),
  label: z.string(),
  token_prefix: z.string(),
  scopes: z.array(apiTokenScopeSchema),
  expires_at: z.string().nullable(),
  revoked_at: z.string().nullable(),
  last_used_at: z.string().nullable(),
  created_at: z.string(),
});
export const createApiTokenResponseSchema = apiTokenSchema.extend({
  token: z.string(),
});
export type CreateApiTokenResponse = z.infer<
  typeof createApiTokenResponseSchema
>;
export const apiTokenListSchema = z.object({
  data: z.array(apiTokenSchema),
});

/** Confluence 后台任务与冻结目录。 */
export const confluenceTaskSchema = z.object({
  id: z.string().uuid(),
  url: z.string().url(),
  status: z.enum([
    'queued',
    'running',
    'stopping',
    'stopped',
    'failed',
    'completed',
    'waiting',
    'selecting',
  ]),
  stage: z.enum(['tree', 'selection', 'markdown', 'export']),
  completed: z.number().int().nonnegative(),
  total: z.number().int().nonnegative(),
  error: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});
export const confluenceTaskListSchema = z.object({
  data: z.array(confluenceTaskSchema),
});
export type ConfluenceNode = {
  id: string;
  title: string;
  children: ConfluenceNode[];
};
const confluenceNodeSchema: z.ZodType<ConfluenceNode> = z.lazy(() =>
  z.object({
    id: z.string().regex(/^\d+$/),
    title: z.string(),
    children: z.array(confluenceNodeSchema),
  }),
);
export const confluenceTreeSchema = z.object({
  selected_node_id: z.string().nullable().optional(),
  nodes: z.array(confluenceNodeSchema),
  pages: z.array(
    z.object({
      id: z.string().regex(/^\d+$/),
      title: z.string(),
      path: z.string(),
      url: z.string().url(),
      version: z.number().int().positive(),
    }),
  ),
});
export const markdownFileSchema = z.string();

/** 文件库按 Space 汇集任务与导入页面，校验界面所需字段。 */
export const confluenceLibraryPageSchema = z.object({
  id: z.string().regex(/^\d+$/),
  title: z.string(),
  url: z.string().url(),
  version: z.number().int().positive(),
  path: z.string(),
  modified_at: z.string().datetime({ offset: true }),
});
export const confluenceLibrarySpaceSchema = z.object({
  id: z.string().regex(/^[a-f0-9]{64}$/),
  key: z.string(),
  base_url: z.string().url(),
  pages: z.array(confluenceLibraryPageSchema),
});
export const confluenceLibrarySchema = z.object({
  spaces: z.array(confluenceLibrarySpaceSchema),
  digest: z.string(),
});
export type ConfluenceLibraryPage = z.infer<typeof confluenceLibraryPageSchema>;
export const confluenceImportReportSchema = z.object({
  id: z.string().uuid(),
  added: z.number().int().nonnegative(),
  updated: z.number().int().nonnegative(),
  kept: z.number().int().nonnegative(),
  conflicts: z.number().int().nonnegative(),
  items: z.array(
    z.object({
      space_id: z.string(),
      page_id: z.string(),
      path: z.string(),
      action: z.enum(['added', 'updated', 'kept', 'conflict']),
      reason: z.string(),
    }),
  ),
});

/** 管理界面只接受明确的账户状态、预览与设置字段。 */
export const adminAccountSchema = userSchema.extend({
  status: z.enum(['active', 'disabled', 'deleting']),
});
export const adminAccountsSchema = z.object({
  data: z.array(adminAccountSchema),
});
export const registrationSchema = z.object({
  registration_enabled: z.boolean(),
});
export const deletionPreviewSchema = z.object({
  digest: z.string().regex(/^[a-f0-9]{64}$/),
  counts: z.object({
    agents: z.number().int(),
    instances: z.number().int(),
    sessions: z.number().int(),
    messages: z.number().int(),
    confluence_tasks: z.number().int(),
    imports: z.number().int(),
    crawl_tasks: z.number().int(),
    crawl_runs: z.number().int(),
    tokens: z.number().int(),
  }),
  library: confluenceImportReportSchema.omit({ id: true }),
});
