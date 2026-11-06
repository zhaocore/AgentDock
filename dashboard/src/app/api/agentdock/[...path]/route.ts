import { NextRequest, NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { z } from 'zod';
import { getServerApiUrl } from '@/lib/server-api-url';
import {
  boundedBody,
  isAllowedAgentDockRoute,
  isSameOrigin,
  jwtExpiry,
  MAX_UPLOAD_BYTES,
  CONFLUENCE_UPLOAD_BYTES,
  SESSION_COOKIE,
} from '@/lib/agentdock-session';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

/** 同源代理只转发获准路径；登录响应中的 JWT 不交给浏览器脚本。 */
async function proxy(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
): Promise<Response> {
  const { path } = await context.params;
  if (!isAllowedAgentDockRoute(request.method, path))
    return NextResponse.json({ error: 'Route not allowed' }, { status: 404 });
  if (request.method !== 'GET' && !isSameOrigin(request))
    return NextResponse.json(
      { error: 'Invalid request origin' },
      { status: 403 },
    );
  const isLogin = path[0] === 'auth' && ['login', 'register'].includes(path[1]);
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE)?.value;
  const isPublic =
    isLogin ||
    (request.method === 'GET' && path.join('/') === 'auth/registration');
  if (!isPublic && !token)
    return NextResponse.json(
      { error: 'Authentication required' },
      { status: 401 },
    );
  const limit =
    path[0] === 'confluence-library' &&
    path[1] === 'imports' &&
    path.length === 2
      ? CONFLUENCE_UPLOAD_BYTES
      : path.at(-1) === 'documents'
        ? MAX_UPLOAD_BYTES
        : path[0] === 'crawl-tasks'
          ? 64 * 1024
          : 16 * 1024;
  const length = request.headers.get('content-length');
  if (length && (!/^\d+$/.test(length) || Number(length) > limit))
    return NextResponse.json(
      { error: 'Request body exceeds size limit' },
      { status: 413 },
    );
  const headers = new Headers();
  const contentType = request.headers.get('content-type');
  if (contentType) headers.set('content-type', contentType);
  if (token && !isLogin) headers.set('authorization', `Bearer ${token}`);
  let upstream: Response;
  try {
    const url = `${getServerApiUrl()}/${path.map(encodeURIComponent).join('/')}${request.nextUrl.search}`;
    const init: RequestInit & { duplex: 'half' } = {
      method: request.method,
      headers,
      cache: 'no-store',
      redirect: 'error',
      signal: request.signal,
      duplex: 'half',
    };
    if (request.body) init.body = boundedBody(request.body, limit);
    upstream = await fetch(url, init);
  } catch (error) {
    const oversized =
      error instanceof RangeError ||
      (error instanceof Error && error.cause instanceof RangeError);
    return NextResponse.json(
      {
        error: oversized
          ? 'Request body exceeds size limit'
          : 'AgentDock API connection failed',
      },
      { status: oversized ? 413 : 502 },
    );
  }
  const responseHeaders = new Headers({ 'cache-control': 'no-store' });
  // content-security-policy / content-disposition / x-content-type-options 用于原文响应：
  // 丢弃会让上传的 HTML 经同源代理失去 sandbox 策略，可在 Dashboard 来源执行脚本。
  for (const name of [
    'content-type',
    'x-request-id',
    'retry-after',
    'content-disposition',
    'x-content-type-options',
  ]) {
    const value = upstream.headers.get(name);
    if (value) responseHeaders.set(name, value);
  }
  // 全局 headers() 会给所有路径设置 frame-ancestors CSP；为避免覆盖丢失上游 sandbox
  // 策略，把两者合并进同一个 CSP 头。
  const upstreamCsp = upstream.headers.get('content-security-policy');
  if (upstreamCsp) {
    responseHeaders.set(
      'content-security-policy',
      `${upstreamCsp}; frame-ancestors 'none'`,
    );
  }
  if (upstream.status === 401) cookieStore.delete(SESSION_COOKIE);
  if (isLogin && upstream.ok) {
    try {
      const auth = z
        .object({ token: z.string().min(1), email: z.string().email() })
        .parse(await upstream.json());
      const maxAge = jwtExpiry(auth.token, Math.floor(Date.now() / 1000));
      cookieStore.set(SESSION_COOKIE, auth.token, {
        httpOnly: true,
        secure: process.env.NODE_ENV === 'production',
        sameSite: 'lax',
        path: '/',
        maxAge,
      });
      return NextResponse.json(
        { email: auth.email },
        { status: upstream.status, headers: responseHeaders },
      );
    } catch {
      return NextResponse.json(
        { error: 'AgentDock returned an invalid authentication response' },
        { status: 502 },
      );
    }
  }
  return new Response(upstream.body, {
    status: upstream.status,
    headers: responseHeaders,
  });
}

export { proxy as GET, proxy as POST, proxy as PUT, proxy as DELETE };
