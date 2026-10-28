import { afterEach, expect, mock, test } from 'bun:test';
import { PlatformAdminClient } from './platform-admin';
import { onApiAuthFailure, setConsoleRequestContext } from './client';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

test('无法解析的403不误判授权，401通知全局登出', async () => {
  const globalFailures: number[] = [];
  const dispose = onApiAuthFailure((status) => globalFailures.push(status));
  try {
    for (const status of [403, 401]) {
      const denied = mock();
      const client = new PlatformAdminClient(denied);
      globalThis.fetch = mock(async () => new Response('denied', { status })) as unknown as typeof fetch;
      await expect(client.request('/bootstrap')).rejects.toMatchObject({ status });
      if (status === 401) expect(denied).toHaveBeenCalledWith(status);
      else expect(denied).not.toHaveBeenCalled();
    }
    expect(globalFailures).toEqual([401]);
  } finally {
    dispose();
  }
});

test('后台写请求只使用独立 CSRF，不继承组织和工作区', async () => {
  setConsoleRequestContext({ organizationUuid: 'tenant', workspaceId: 'workspace', csrfToken: 'tenant-token' });
  let captured: RequestInit | undefined;
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) => {
    captured = options;
    return Response.json({ ok: true });
  }) as unknown as typeof fetch;
  const client = new PlatformAdminClient(() => {});
  client.csrfToken = 'platform-token';
  await client.request('/organizations', { method: 'POST', body: { name: '研发' }, key: 'operation' });
  const headers = new Headers(captured?.headers);
  expect(headers.get('X-CSRF-Token')).toBe('platform-token');
  expect(headers.get('Idempotency-Key')).toBe('operation');
  expect(headers.has('X-Organization-UUID')).toBe(false);
  expect(headers.has('X-Workspace-ID')).toBe(false);
  expect(captured?.credentials).toBe('include');
});

test('切换账号后丢弃旧写入响应，不中断服务端写入', async () => {
  let finish!: (response: Response) => void;
  let signal: AbortSignal | null | undefined;
  globalThis.fetch = mock((_url: RequestInfo | URL, options?: RequestInit) => {
    signal = options?.signal;
    return new Promise<Response>((resolve) => {
      finish = resolve;
    });
  }) as unknown as typeof fetch;
  const client = new PlatformAdminClient(() => {});
  const pending = client.request('/organizations', { method: 'POST', body: {} });
  client.invalidate();
  finish(Response.json({ uuid: 'old-account-result' }));
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(signal).toBeUndefined();
});

for (const code of ['platform_access_denied', 'verified_login_required', 'invalid_csrf', 'unknown']) {
  test(`403 ${code} 仅在授权失效时清空后台`, async () => {
    const denied = mock();
    const client = new PlatformAdminClient(denied);
    client.csrfToken = 'current-token';
    globalThis.fetch = mock(async () => Response.json({ error: { code } }, { status: 403 })) as typeof fetch;
    await expect(client.request('/organizations', { method: 'POST', body: {} })).rejects.toMatchObject({
      status: 403,
      code,
    });
    const accessDenied = ['platform_access_denied', 'verified_login_required'].includes(code);
    expect(denied.mock.calls.length).toBe(accessDenied ? 1 : 0);
    expect(client.csrfToken).toBe(accessDenied ? '' : 'current-token');
  });
}
