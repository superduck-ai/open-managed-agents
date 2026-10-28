import { afterEach, expect, mock, test } from 'bun:test';
import { postJsonSseStream } from './streaming';
import { cancelScopeRequests, setConsoleRequestContext } from './client';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

test('SSE 拒绝调用方与活动组织冲突', async () => {
  setConsoleRequestContext({ organizationUuid: 'target' });
  globalThis.fetch = mock(async () => new Response()) as typeof fetch;
  await expect(
    postJsonSseStream({ url: '/stream', headers: { 'X-Organization-UUID': 'other' }, body: {}, onEvent: () => {} }),
  ).rejects.toThrow('Conflicting organization scope');
  expect(globalThis.fetch).not.toHaveBeenCalled();
});

test('SSE 自动传播组织工作区及 CSRF，切换取消旧连接', async () => {
  setConsoleRequestContext({ organizationUuid: 'target', workspaceId: 'workspace', csrfToken: 'csrf' });
  let signal: AbortSignal | undefined;
  globalThis.fetch = mock(async (_input, options) => {
    const headers = new Headers(options?.headers);
    expect(headers.get('X-Organization-UUID')).toBe('target');
    expect(headers.get('X-Workspace-ID')).toBe('workspace');
    expect(headers.get('X-CSRF-Token')).toBe('csrf');
    signal = options?.signal as AbortSignal;
    cancelScopeRequests();
    return new Response('data: {"late":true}\n\n');
  }) as typeof fetch;
  const event = mock(() => {});
  await expect(postJsonSseStream({ url: '/stream', body: {}, onEvent: event })).rejects.toBeTruthy();
  expect(signal?.aborted).toBe(true);
  expect(event).not.toHaveBeenCalled();
});
