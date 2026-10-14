import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { downloadApiFile } from './file-download';
import { onApiAuthFailure, setConsoleRequestContext } from './client';

const originalFetch = globalThis.fetch;
const originalCreateObjectURL = URL.createObjectURL;
const originalRevokeObjectURL = URL.revokeObjectURL;
const originalClick = window.HTMLAnchorElement.prototype.click;

afterEach(() => {
  globalThis.fetch = originalFetch;
  URL.createObjectURL = originalCreateObjectURL;
  URL.revokeObjectURL = originalRevokeObjectURL;
  window.HTMLAnchorElement.prototype.click = originalClick;
  setConsoleRequestContext({});
});

test('reports download authorization failures without creating an empty download', async () => {
  resetTestDom('https://oma.duck.ai/');
  const createObjectURL = mock(() => 'blob:empty');
  URL.createObjectURL = createObjectURL;
  globalThis.fetch = async () =>
    Response.json({ error: { type: 'authentication_error', message: 'Login required' } }, { status: 401 });
  const failure = mock(() => {});
  const unsubscribe = onApiAuthFailure(failure);
  try {
    await expect(
      downloadApiFile({ id: 'file_output', filename: 'output.csv' }, 'workspace_test'),
    ).rejects.toMatchObject({ status: 401, message: 'Login required' });
    expect(failure).toHaveBeenCalledTimes(1);
    expect(createObjectURL).not.toHaveBeenCalled();
  } finally {
    unsubscribe();
  }
});

test('downloads through the Files SDK with workspace scope and the original filename', async () => {
  resetTestDom('https://oma.duck.ai/');
  let capturedUrl: URL | undefined;
  let capturedInit: RequestInit | undefined;
  globalThis.fetch = async (input, init) => {
    capturedUrl = new URL(String(input), 'https://oma.duck.ai');
    capturedInit = init;
    return new Response('a,b\n1,2', { headers: { 'Content-Type': 'text/csv' } });
  };
  const click = mock(function (this: HTMLAnchorElement) {
    expect(this.download).toBe('output.csv');
    expect(this.href).toBe('blob:output');
  });
  window.HTMLAnchorElement.prototype.click = click;
  URL.createObjectURL = mock(() => 'blob:output');
  URL.revokeObjectURL = mock(() => {});
  setConsoleRequestContext({ organizationUuid: 'org_test' });
  await downloadApiFile({ id: 'file_output', filename: 'output.csv' }, 'workspace_test');
  expect(capturedUrl?.pathname).toBe('/v1/files/file_output/content');
  expect(capturedUrl?.searchParams.get('beta')).toBe('true');
  expect(new Headers(capturedInit?.headers).get('anthropic-beta')).toContain('files-api-2025-04-14');
  expect(new Headers(capturedInit?.headers).get('x-workspace-id')).toBe('workspace_test');
  expect(new Headers(capturedInit?.headers).get('x-organization-uuid')).toBe('org_test');
  expect(capturedInit?.credentials).toBe('include');
  expect(click).toHaveBeenCalledTimes(1);
  expect(document.querySelector('a')).toBeNull();
});
