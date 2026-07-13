import { afterEach, describe, expect, mock, test } from 'bun:test';
import { consoleApi, filesApi, messageBatchesApi, setConsoleRequestContext, skillsApi, webhooksApi } from './client';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

describe('consoleApi', () => {
  test('adds active console organization and workspace headers', async () => {
    let capturedHeaders = new Headers();
    globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
      capturedHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    await consoleApi<{ ok: boolean }>('/v1/agents?beta=true');

    expect(capturedHeaders.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(capturedHeaders.get('x-workspace-id')).toBe('wrkspc_test_uuid');
  });
});

describe('filesApi', () => {
  test('adds files beta and active console context headers', async () => {
    let capturedHeaders = new Headers();
    globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
      capturedHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ data: [], has_more: false }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    await filesApi<{ data: unknown[]; has_more: boolean }>('/v1/files?beta=true');

    expect(capturedHeaders.get('anthropic-beta')).toBe('files-api-2025-04-14');
    expect(capturedHeaders.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(capturedHeaders.get('x-workspace-id')).toBe('wrkspc_test_uuid');
  });
});

describe('skillsApi', () => {
  test('adds skills beta and active console context headers', async () => {
    let capturedHeaders = new Headers();
    globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
      capturedHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ data: [], has_more: false }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    await skillsApi<{ data: unknown[]; has_more: boolean }>('/v1/skills?beta=true');

    expect(capturedHeaders.get('anthropic-beta')).toBe('skills-2025-10-02');
    expect(capturedHeaders.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(capturedHeaders.get('x-workspace-id')).toBe('wrkspc_test_uuid');
  });
});

describe('messageBatchesApi', () => {
  test('adds message batches beta, API version, and active console context headers', async () => {
    let capturedHeaders = new Headers();
    globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
      capturedHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ data: [], has_more: false }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    await messageBatchesApi<{ data: unknown[]; has_more: boolean }>('/v1/messages/batches?beta=true');

    expect(capturedHeaders.get('anthropic-beta')).toBe('message-batches-2024-09-24');
    expect(capturedHeaders.get('anthropic-version')).toBe('2023-06-01');
    expect(capturedHeaders.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(capturedHeaders.get('x-workspace-id')).toBe('wrkspc_test_uuid');
  });
});

describe('webhooksApi', () => {
  test('adds webhooks beta and active console context headers', async () => {
    let capturedHeaders = new Headers();
    globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
      capturedHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    await webhooksApi<{ data: unknown[] }>('/v1/webhooks?beta=true');

    expect(capturedHeaders.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(capturedHeaders.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(capturedHeaders.get('x-workspace-id')).toBe('wrkspc_test_uuid');
  });
});
