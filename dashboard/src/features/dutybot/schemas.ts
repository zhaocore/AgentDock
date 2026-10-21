import { z } from 'zod';

export const userSchema = z.object({
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
