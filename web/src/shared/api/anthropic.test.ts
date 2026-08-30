import { afterEach, describe, expect, mock, test } from 'bun:test';
import { anthropicApi, anthropicBaseURL, anthropicBetaApi, setAnthropicClientForTest } from './anthropic';
import { setConsoleRequestContext } from './client';
import { resetTestDom } from '../../test/setup';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  setAnthropicClientForTest(null);
  resetTestDom('https://oma.duck.ai/');
});

describe('anthropicBetaApi', () => {
  test('lists models through the v1 SDK boundary with the requested workspace', async () => {
    let capturedInput: RequestInfo | URL = '';
    let capturedInit: RequestInit | undefined;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      capturedInput = input;
      capturedInit = init;
      return new Response(JSON.stringify({ data: [{ id: 'claude-sonnet-4-6', display_name: 'Claude Sonnet 4.6' }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as unknown as typeof fetch;

    const page = await anthropicApi.models.list<{ id: string }>({ limit: 1000 }, 'workspace_models');

    expect(String(capturedInput).startsWith('/v1/models?')).toBe(true);
    expect(new Headers(capturedInit?.headers).get('x-workspace-id')).toBe('workspace_models');
    expect(page.data).toEqual([{ id: 'claude-sonnet-4-6', display_name: 'Claude Sonnet 4.6' }]);
  });

  test('uses same-origin SDK requests with workspace headers and cookie credentials', async () => {
    let capturedInput: RequestInfo | URL = '';
    let capturedInit: RequestInit | undefined;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      capturedInput = input;
      capturedInit = init;
      return new Response(
        JSON.stringify({
          data: [
            {
              id: 'file_123',
              type: 'file',
              filename: 'notes.txt',
              mime_type: 'text/plain',
              size_bytes: 5,
              created_at: '2026-01-01T00:00:00Z',
            },
          ],
          has_more: true,
          first_id: 'file_123',
          last_id: 'file_123',
        }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
    });

    const page = await anthropicBetaApi.files.list<{ id: string }>({ limit: 20 });

    expect(anthropicBaseURL()).toBe(window.location.origin);
    expect(String(capturedInput).startsWith('/v1/files?')).toBe(true);
    expect(capturedInit?.credentials).toBe('include');
    const headers = new Headers(capturedInit?.headers);
    expect(headers.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(headers.get('x-workspace-id')).toBe('wrkspc_test_uuid');
    expect(headers.get('x-api-key')).toBeNull();
    expect(headers.get('authorization')).toBeNull();
    expect(page).toEqual({
      data: [
        {
          id: 'file_123',
          type: 'file',
          filename: 'notes.txt',
          mime_type: 'text/plain',
          size_bytes: 5,
          created_at: '2026-01-01T00:00:00Z',
        },
      ],
      has_more: true,
      first_id: 'file_123',
      last_id: 'file_123',
    });
    expect(Object.getPrototypeOf(page)).toBe(Object.prototype);
  });

  test('uploads skill versions through same-origin SDK requests with cookie credentials', async () => {
    let capturedInput: RequestInfo | URL = '';
    let capturedInit: RequestInit | undefined;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      capturedInput = input;
      capturedInit = init;
      return new Response(
        JSON.stringify({
          skill_id: 'skill_123',
          version: '1783557867000000',
          name: 'emoji-translator',
          description: 'Translate text to emoji.',
        }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }) as unknown as typeof fetch;

    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: 'wrkspc_test_uuid',
      csrfToken: 'csrf_test_token',
    });

    const file = new File(['skill archive'], 'emoji-translator.zip', { type: 'application/zip' });
    const version = await anthropicBetaApi.skills.versions.create<{ skill_id: string }>('skill_123', { files: [file] });

    expect(String(capturedInput)).toBe('/v1/skills/skill_123/versions?beta=true');
    expect(capturedInit?.method).toBe('POST');
    expect(capturedInit?.credentials).toBe('include');
    expect(capturedInit?.body).toBeInstanceOf(FormData);
    const body = capturedInit?.body as FormData;
    const uploadedFile = body.get('files[]') as File | null;
    expect(uploadedFile?.name).toBe('emoji-translator.zip');
    const headers = new Headers(capturedInit?.headers);
    expect(headers.get('anthropic-beta')).toBe('skills-2025-10-02');
    expect(headers.get('x-organization-uuid')).toBe('org_test_uuid');
    expect(headers.get('x-workspace-id')).toBe('wrkspc_test_uuid');
    expect(headers.get('x-csrf-token')).toBe('csrf_test_token');
    expect(headers.get('x-api-key')).toBeNull();
    expect(headers.get('authorization')).toBeNull();
    expect(version.skill_id).toBe('skill_123');
  });

  test('normalizes SDK API errors to the frontend API error shape', async () => {
    globalThis.fetch = mock(async () => {
      return new Response(
        JSON.stringify({
          error: {
            type: 'invalid_request_error',
            message: 'Workspace is required.',
          },
        }),
        {
          status: 400,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }) as unknown as typeof fetch;

    await expect(anthropicBetaApi.files.list({ limit: 20 }, 'missing_workspace')).rejects.toEqual({
      status: 400,
      code: 'invalid_request_error',
      message: 'Workspace is required.',
    });
  });

  test('normalizes an unconfigured model catalog as HTTP 503', async () => {
    globalThis.fetch = mock(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              type: 'api_error',
              message: 'This workspace has no LLM provider configured',
            },
          }),
          { status: 503, headers: { 'Content-Type': 'application/json' } },
        ),
    ) as unknown as typeof fetch;

    await expect(anthropicApi.models.list({ limit: 1000 }, 'unconfigured')).rejects.toEqual({
      status: 503,
      code: 'api_error',
      message: 'This workspace has no LLM provider configured',
    });
  });
});
