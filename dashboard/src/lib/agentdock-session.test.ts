import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  boundedBody,
  isAllowedAgentDockRoute,
  isSameOrigin,
  jwtExpiry,
} from './agentdock-session';
import { getServerApiUrl } from './server-api-url';

afterEach(() => vi.unstubAllEnvs());

describe('AgentDock session boundaries', () => {
  it('only proxies explicit methods and paths', () => {
    const id = '11111111-1111-1111-1111-111111111111';
    expect(isAllowedAgentDockRoute('GET', ['crawl-profiles'])).toBe(true);
    expect(isAllowedAgentDockRoute('GET', ['crawl-tasks', id, 'runs'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('POST', ['crawl-tasks', id, 'runs'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('PUT', ['crawl-tasks', id])).toBe(true);
    expect(isAllowedAgentDockRoute('GET', ['crawl-runs', id, 'report'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('PUT', ['crawl-profiles', 'docs'])).toBe(
      false,
    );
    expect(
      isAllowedAgentDockRoute('GET', ['crawl-runs', 'bad', 'report']),
    ).toBe(false);
    expect(isAllowedAgentDockRoute('GET', ['crawl-runs', id, 'markdown'])).toBe(
      true,
    );
    expect(
      isAllowedAgentDockRoute('POST', ['crawl-runs', id, 'markdown']),
    ).toBe(false);
    expect(
      isAllowedAgentDockRoute('GET', ['crawl-runs', 'bad', 'markdown']),
    ).toBe(false);
    // 仓库：列表 / 详情 / 同步 / 删除
    expect(isAllowedAgentDockRoute('GET', ['repositories'])).toBe(true);
    expect(isAllowedAgentDockRoute('GET', ['repositories', id])).toBe(true);
    expect(isAllowedAgentDockRoute('POST', ['repositories'])).toBe(true);
    expect(isAllowedAgentDockRoute('POST', ['repositories', id, 'sync'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('DELETE', ['repositories', id])).toBe(true);
    // Agent / 实例 / 会话
    expect(isAllowedAgentDockRoute('GET', ['agents'])).toBe(true);
    expect(isAllowedAgentDockRoute('POST', ['agents', id, 'instances'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('POST', ['instances', id, 'start'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('POST', ['sessions', id, 'messages'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('GET', ['sessions', id, 'messages'])).toBe(
      true,
    );
    expect(isAllowedAgentDockRoute('DELETE', ['sessions', id])).toBe(true);
    // 拒绝形态：未知方法、路径穿越、未登记资源
    expect(isAllowedAgentDockRoute('POST', ['auth', 'refresh'])).toBe(false);
    expect(isAllowedAgentDockRoute('GET', ['admin', 'users'])).toBe(false);
    expect(isAllowedAgentDockRoute('PATCH', ['agents', id])).toBe(false);
    expect(isAllowedAgentDockRoute('GET', ['agents', '..'])).toBe(false);
    expect(isAllowedAgentDockRoute('GET', ['agents', 'bad'])).toBe(false);
    expect(isAllowedAgentDockRoute('POST', ['sessions'])).toBe(false);
  });
  it('requires matching Origin for writes', () => {
    expect(
      isSameOrigin(
        new Request('http://localhost:3000/api/session', {
          headers: { origin: 'http://evil.test' },
        }),
      ),
    ).toBe(false);
    expect(isSameOrigin(new Request('http://localhost:3000/api/session'))).toBe(
      false,
    );
    expect(
      isSameOrigin(
        new Request('http://localhost:3000/api/session', {
          headers: { origin: 'http://localhost:3000' },
        }),
      ),
    ).toBe(true);
    vi.stubEnv('DASHBOARD_URL', 'https://studio.test');
    expect(
      isSameOrigin(
        new Request('http://next:3000/api/session', {
          headers: { origin: 'https://studio.test' },
        }),
      ),
    ).toBe(true);
  });
  it('accepts explicitly registered extra origins like internal IPs', () => {
    vi.stubEnv('NODE_ENV', 'production');
    vi.stubEnv('DASHBOARD_URL', 'https://studio.test');
    vi.stubEnv(
      'DASHBOARD_EXTRA_ORIGINS',
      'http://10.53.1.2:3000, http://10.53.1.3:3000',
    );
    for (const origin of ['http://10.53.1.2:3000', 'http://10.53.1.3:3000']) {
      expect(
        isSameOrigin(
          new Request('http://next:3000/api/agentdock/auth/login', {
            headers: { origin },
          }),
        ),
        origin,
      ).toBe(true);
    }
    // 未登记的邻近 IP 仍拒绝
    expect(
      isSameOrigin(
        new Request('http://next:3000/api/agentdock/auth/login', {
          headers: { origin: 'http://10.53.1.4:3000' },
        }),
      ),
    ).toBe(false);
  });
  it('accepts same-port loopback aliases only during development', () => {
    vi.stubEnv('NODE_ENV', 'development');
    vi.stubEnv('DASHBOARD_URL', 'http://127.0.0.1:3100');
    const request = new Request(
      'http://localhost:3100/api/agentdock/auth/register',
      {
        headers: { origin: 'http://localhost:3100' },
      },
    );
    expect(isSameOrigin(request)).toBe(true);
    vi.stubEnv('NODE_ENV', 'production');
    expect(isSameOrigin(request)).toBe(false);
  });
  it('rejects foreign origins, ports, schemes and malformed Origin values', () => {
    vi.stubEnv('NODE_ENV', 'development');
    vi.stubEnv('DASHBOARD_URL', 'http://127.0.0.1:3100');
    for (const origin of [
      'http://localhost:3101',
      'https://localhost:3100',
      'http://localhost.evil.test:3100',
      'http://evil.test:3100',
      'http://localhost:3100/path',
      'http://user@localhost:3100',
      'null',
      'broken',
    ]) {
      expect(
        isSameOrigin(
          new Request('http://localhost:3100/api/session', {
            headers: { origin },
          }),
        ),
        origin,
      ).toBe(false);
    }
    expect(isSameOrigin(new Request('http://localhost:3100/api/session'))).toBe(
      false,
    );
    vi.stubEnv('DASHBOARD_URL', 'https://studio.test');
    expect(
      isSameOrigin(
        new Request('http://localhost:3100/api/session', {
          headers: { origin: 'http://localhost:3100' },
        }),
      ),
    ).toBe(false);
  });
  it('bounds cookie lifetime and refuses expired or malformed tokens', () => {
    const token = `header.${Buffer.from(JSON.stringify({ exp: 200 })).toString('base64url')}.signature`;
    expect(jwtExpiry(token, 100)).toBe(100);
    expect(() => jwtExpiry(token, 201)).toThrow('expired');
    expect(() => jwtExpiry('broken', 100)).toThrow('invalid JWT');
  });
  it('rejects oversized streamed bodies without disk buffering', async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array(4));
        controller.enqueue(new Uint8Array(4));
        controller.close();
      },
    });
    await expect(
      new Response(boundedBody(stream, 7)).arrayBuffer(),
    ).rejects.toThrow('size limit');
  });
  it('never permits credentials or a non-HTTP upstream', () => {
    vi.stubEnv('API_INTERNAL_URL', 'file:///etc/passwd');
    expect(getServerApiUrl).toThrow();
    vi.stubEnv('API_INTERNAL_URL', 'https://user:password@service.test/v1');
    expect(getServerApiUrl).toThrow();
    vi.stubEnv('API_INTERNAL_URL', 'http://agentdock:8080/custom/');
    expect(getServerApiUrl()).toBe('http://agentdock:8080/custom');
  });
});
