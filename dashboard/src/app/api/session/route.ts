import { cookies } from 'next/headers';
import { NextRequest, NextResponse } from 'next/server';
import { isSameOrigin, SESSION_COOKIE } from '@/lib/dutybot-session';

/** 退出只清理工作台会话，不声称吊销已签发 JWT。 */
export async function DELETE(request: NextRequest): Promise<Response> {
  if (!isSameOrigin(request))
    return NextResponse.json(
      { error: 'Invalid request origin' },
      { status: 403 },
    );
  (await cookies()).delete(SESSION_COOKIE);
  return NextResponse.json({ ok: true });
}
