import { z } from 'zod';

export const SESSION_COOKIE = 'agentdock_session';
export const MAX_UPLOAD_BYTES = 101 * 1024 * 1024;
export const CONFLUENCE_UPLOAD_BYTES = 10 * 1024 * 1024 * 1024;

/** 只接受当前工作台使用的 AgentDock 路径与方法。 */
export function isAllowedAgentDockRoute(
  method: string,
  segments: string[],
): boolean {
  const path = segments.join('/');
  if (
    segments.some(
      (segment) =>
        segment.includes('/') ||
        segment.includes('\\') ||
        segment === '.' ||
        segment === '..',
    )
  )
    return false;
  const uuid =
    '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
  const routes: Record<string, RegExp[]> = {
    GET: [
      /^auth\/registration$/,
      /^admin\/(?:users|settings)$/,
      new RegExp(
        `^admin/users/${uuid}/(?:api-tokens|deletion-preview|deletion|confluence-library(?:/archive|/archive-check)?|confluence-tasks|crawl-tasks)$`,
      ),
      new RegExp(
        `^admin/users/${uuid}/confluence-library/spaces/[a-f0-9]{64}/pages/[0-9]+/file$`,
      ),
      new RegExp(
        `^admin/users/${uuid}/confluence-tasks/${uuid}(?:/tree|/pages/[0-9]+/file)?$`,
      ),
      new RegExp(`^admin/users/${uuid}/crawl-tasks/${uuid}(?:/runs)?$`),
      new RegExp(
        `^admin/users/${uuid}/crawl-runs/${uuid}/(?:report|markdown)$`,
      ),
      /^crawl-profiles$/,
      /^crawl-tasks$/,
      new RegExp(`^crawl-tasks/${uuid}(?:/runs)?$`),
      new RegExp(`^crawl-runs/${uuid}/(?:report|markdown)$`),
      /^confluence-library(?:\/archive|\/archive-check)?$/,
      /^confluence-library\/spaces\/[a-f0-9]{64}\/pages\/[0-9]+\/file$/,
      /^confluence-tasks$/,
      new RegExp(`^confluence-tasks/${uuid}(?:/tree|/pages/[0-9]+/file)?$`),
      /^auth\/me$/,
      /^health$/,
      /^repositories$/,
      new RegExp(`^repositories/${uuid}$`),
      /^agents$/,
      new RegExp(`^agents/${uuid}(?:/instances)?$`),
      new RegExp(`^instances/${uuid}(?:/sessions)?$`),
      /^sessions$/,
      new RegExp(`^sessions/${uuid}(?:/messages)?$`),
      new RegExp(`^api-tokens(?:/${uuid})?$`),
    ],
    POST: [
      /^admin\/users$/,
      new RegExp(`^admin/users/${uuid}/(?:password|delete)$`),
      new RegExp(
        `^admin/users/${uuid}/(?:confluence-tasks|crawl-tasks)/${uuid}/stop$`,
      ),
      /^crawl-tasks$/,
      new RegExp(`^crawl-tasks/${uuid}/runs$`),
      /^confluence-library\/imports$/,
      new RegExp(`^confluence-library/imports/${uuid}/apply$`),
      /^confluence-tasks$/,
      new RegExp(`^confluence-tasks/${uuid}/(?:start|stop|export)$`),
      /^auth\/(?:login|register|change-password)$/,
      /^repositories$/,
      new RegExp(`^repositories/${uuid}\/sync$`),
      /^agents$/,
      new RegExp(`^agents/${uuid}\/instances$`),
      new RegExp(`^instances/${uuid}\/(?:start|stop|sessions)$`),
      new RegExp(`^sessions/${uuid}\/messages$`),
      /^api-tokens$/,
    ],
    PUT: [
      /^admin\/settings$/,
      new RegExp(`^admin/users/${uuid}/status$`),
      new RegExp(`^crawl-tasks/${uuid}$`),
    ],
    DELETE: [
      new RegExp(`^admin/users/${uuid}/confluence-library$`),
      new RegExp(
        `^admin/users/${uuid}/(?:api-tokens|confluence-tasks|crawl-tasks)/${uuid}$`,
      ),
      new RegExp(`^repositories/${uuid}$`),
      new RegExp(`^agents/${uuid}$`),
      new RegExp(`^sessions/${uuid}$`),
      new RegExp(`^api-tokens/${uuid}$`),
    ],
  };
  return routes[method]?.some((route) => route.test(path)) === true;
}

/** 生产严格匹配配置源；开发只允许同协议同端口的明确回环别名。
 * 额外信任源经 DASHBOARD_EXTRA_ORIGINS（逗号分隔）显式登记，仅放行完整 origin。
 */
export function isSameOrigin(request: Request): boolean {
  const expected = new URL(process.env.DASHBOARD_URL || request.url);
  const origin = request.headers.get('origin');
  if (origin === expected.origin) return true;
  // 显式登记的额外源（如内网 IP 访问 http://10.53.x.x:3000）
  const extra = (process.env.DASHBOARD_EXTRA_ORIGINS || '')
    .split(',')
    .map((value) => value.trim())
    .filter(Boolean);
  if (origin !== null && extra.includes(origin)) return true;
  if (
    process.env.NODE_ENV !== 'development' ||
    origin === null ||
    !URL.canParse(origin)
  )
    return false;
  const source = new URL(origin);
  const loopbackHosts = ['localhost', '127.0.0.1'];
  return (
    origin === source.origin &&
    loopbackHosts.includes(expected.hostname) &&
    loopbackHosts.includes(source.hostname) &&
    source.protocol === expected.protocol &&
    source.port === expected.port
  );
}

/** Cookie 到期不晚于服务端签发的 JWT。 */
export function jwtExpiry(token: string, now: number): number {
  const parts = token.split('.');
  if (parts.length !== 3) throw new Error('AgentDock returned an invalid JWT');
  const payload = z
    .object({ exp: z.number().int().positive() })
    .parse(JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8')));
  const remaining = payload.exp - now;
  if (remaining <= 0) throw new Error('AgentDock returned an expired JWT');
  return remaining;
}

/** 上传直通内存流，限制总字节数且不写临时文件。 */
export function boundedBody(
  body: ReadableStream<Uint8Array>,
  limit: number,
): ReadableStream<Uint8Array> {
  let bytes = 0;
  return body.pipeThrough(
    new TransformStream<Uint8Array, Uint8Array>({
      transform(chunk, controller) {
        bytes += chunk.byteLength;
        if (bytes > limit)
          throw new RangeError('Request body exceeds size limit');
        controller.enqueue(chunk);
      },
    }),
  );
}
