import { NextRequest, NextResponse } from 'next/server';
import { SESSION_COOKIE } from '@/lib/dutybot-session';

export function middleware(request: NextRequest): NextResponse {
  const path = request.nextUrl.pathname;
  if (path === '/login' || path === '/register')
    return withPageSecurity(NextResponse.next());
  if (!request.cookies.has(SESSION_COOKIE)) {
    const login = new URL('/login', request.url);
    if (path.startsWith('/dashboard/'))
      login.searchParams.set('next', path + request.nextUrl.search);
    return withPageSecurity(NextResponse.redirect(login));
  }
  if (path === '/' || path === '/dashboard')
    return withPageSecurity(
      NextResponse.redirect(new URL('/dashboard/agents', request.url)),
    );
  return withPageSecurity(NextResponse.next());
}

// withPageSecurity 为页面响应加 frame-ancestors CSP；
// 放在 middleware 而非 next.config headers()，避免全局头覆盖代理透传的原文 sandbox CSP。
function withPageSecurity(response: NextResponse): NextResponse {
  response.headers.set('Content-Security-Policy', "frame-ancestors 'none'");
  return response;
}
export const config = {
  matcher: ['/', '/dashboard/:path*', '/login', '/register'],
};
