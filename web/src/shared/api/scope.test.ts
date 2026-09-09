import { afterEach, expect, mock, test } from 'bun:test';
import { cancelScopeRequests, consoleApi, getConsoleRequestContext, setConsoleRequestContext } from './client';
import { fetchBootstrap } from '../auth/api';
import { listConsoleWorkspaces } from '../workspaces/api';
import { listInvitations, respondToInvitation } from '../../features/organizations/api';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  cancelScopeRequests();
  setConsoleRequestContext({});
});

test('空 context 的账号请求不携带旧组织工作区且邀请保留 CSRF cookie', async () => {
  const requests: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    requests.push(options!);
    return Response.json(String(input).endsWith('/workspaces') ? [] : { data: [] });
  }) as typeof fetch;
  setConsoleRequestContext({ organizationUuid: 'old', workspaceId: 'old-ws', csrfToken: 'csrf' });
  await fetchBootstrap();
  await listInvitations();
  await respondToInvitation('invitation', 'accept');
  for (const request of requests) {
    expect(new Headers(request.headers).has('X-Organization-UUID')).toBe(false);
    expect(new Headers(request.headers).has('X-Workspace-ID')).toBe(false);
    expect(request.credentials).toBe('include');
  }
  expect(new Headers(requests[2].headers).get('X-CSRF-Token')).toBe('csrf');
  await listConsoleWorkspaces('target');
  expect(new Headers(requests[3].headers).get('X-Organization-UUID')).toBe('target');
  expect(new Headers(requests[3].headers).has('X-Workspace-ID')).toBe(false);
});

test('迟到读响应不会发布且使用发送时的上下文快照', async () => {
  let complete!: (response: Response) => void;
  let captured!: RequestInit;
  globalThis.fetch = mock((_input, options) => {
    captured = options!;
    return new Promise<Response>((resolve) => {
      complete = resolve;
    });
  }) as typeof fetch;
  setConsoleRequestContext({ organizationUuid: 'a', workspaceId: 'wa' });
  const request = consoleApi('/api/business');
  cancelScopeRequests();
  setConsoleRequestContext({ organizationUuid: 'b', workspaceId: 'wb' });
  complete(Response.json({ from: 'a' }));
  await expect(request).rejects.toHaveProperty('name', 'AbortError');
  expect(new Headers(captured.headers).get('X-Organization-UUID')).toBe('a');
  expect(captured.signal?.aborted).toBe(true);
  expect(getConsoleRequestContext().organizationUuid).toBe('b');
});

test('已发送写请求不取消不重发，但迟到写结果被隔离', async () => {
  let complete!: (response: Response) => void;
  let captured!: RequestInit;
  const fetchMock = mock((_input, options) => {
    captured = options!;
    return new Promise<Response>((resolve) => {
      complete = resolve;
    });
  });
  globalThis.fetch = fetchMock as typeof fetch;
  setConsoleRequestContext({ organizationUuid: 'a', workspaceId: 'wa' });
  const request = consoleApi('/api/business', { method: 'POST', body: '{}' });
  cancelScopeRequests();
  expect(captured.signal?.aborted ?? false).toBe(false);
  complete(Response.json({ created: true }));
  await expect(request).rejects.toHaveProperty('name', 'AbortError');
  expect(fetchMock).toHaveBeenCalledTimes(1);
});
