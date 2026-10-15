import '../../../test/setup';
import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '../../../shared/i18n';
import { MessageScrollerProvider } from '../../../shared/ui/message-scroller';
import { toast } from '../../../shared/ui/sonner';
import { resetTestDom } from '../../../test/setup';
import type { FileMetadataApiResponse, SessionApiResponse } from '../types';
import { useSessionFiles } from './SessionFileContext';
import { SessionOutputFileList, SessionOutputFiles } from './SessionOutputFiles';
import { sessionFilesQueryKey } from './sessionFiles';

const { act, cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalCreateObjectURL = URL.createObjectURL;
const originalClick = window.HTMLAnchorElement.prototype.click;
let errorToast: ReturnType<typeof spyOn> | undefined;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  URL.createObjectURL = originalCreateObjectURL;
  window.HTMLAnchorElement.prototype.click = originalClick;
  errorToast?.mockRestore();
});

function makeSession(id = 'sesn_outputs', status = 'idle') {
  return { id, status, resources: [], archived_at: null, type: 'session' } as unknown as SessionApiResponse;
}

function file(id: string, filename = 'result.csv', downloadable = true): FileMetadataApiResponse {
  return {
    id,
    filename,
    downloadable,
    type: 'file',
    mime_type: 'text/csv',
    size_bytes: 123,
    created_at: '2026-10-12T08:00:00Z',
    scope: { type: 'session', id: 'sesn_outputs' },
  };
}

function page(files: FileMetadataApiResponse[], extra = {}) {
  return Response.json({ data: files, has_more: false, ...extra });
}

function TestFileList() {
  const data = useSessionFiles();
  return data ? <SessionOutputFileList files={data.files} workspaceId={data.workspaceId} /> : null;
}

function renderOutputs(overrides: Partial<Parameters<typeof SessionOutputFiles>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const initial = { session: makeSession(), workspaceId: 'workspace_outputs', refreshKey: 0, ...overrides };
  const tree = (props: typeof initial) => (
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <MessageScrollerProvider>
          <SessionOutputFiles {...props}>
            <TestFileList />
          </SessionOutputFiles>
        </MessageScrollerProvider>
      </I18nProvider>
    </QueryClientProvider>
  );
  const view = render(tree(initial));
  return { client, rerender: (props: Partial<typeof initial>) => view.rerender(tree({ ...initial, ...props })) };
}

test('shows a retryable file-list failure instead of reporting that no output exists', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async () =>
    Response.json({ error: { type: 'api_error', message: 'Files offline' } }, { status: 500 });
  renderOutputs();
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByText('Files offline')).toBeTruthy();
  globalThis.fetch = async () => page([file('file_ready')]);
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByRole('button', { name: 'Download result.csv' });
  expect(screen.queryByRole('alert')).toBeNull();
});

test('retains loaded file cards when refresh fails and repairs the list on retry', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async () => page([file('file_existing')]);
  const { client } = renderOutputs();
  await screen.findByRole('button', { name: 'Download result.csv' });
  globalThis.fetch = async () =>
    Response.json({ error: { type: 'api_error', message: 'Refresh failed' } }, { status: 500 });
  await act(async () => {
    await client.invalidateQueries({ queryKey: sessionFilesQueryKey('workspace_outputs', 'sesn_outputs') });
  });
  expect(screen.getByRole('button', { name: 'Download result.csv' }).hasAttribute('disabled')).toBe(false);
  expect(await screen.findByText('Refresh failed')).toBeTruthy();
  globalThis.fetch = async () => page([file('file_existing'), file('file_new', 'summary.pdf')]);
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByRole('button', { name: 'Download summary.pdf' });
  expect(screen.queryByRole('alert')).toBeNull();
});

test('renders all scoped pages, excludes uploads, and deduplicates files outside the resource projection', async () => {
  resetTestDom('https://oma.duck.ai/');
  const requests: URL[] = [];
  globalThis.fetch = async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    requests.push(url);
    return url.searchParams.get('after_id')
      ? page([{ ...file('file_output'), size_bytes: 0 }, file('file_extra', 'extra.pdf')])
      : page([file('file_input', 'input.csv', false), file('file_output')], { has_more: true, last_id: 'file_output' });
  };
  renderOutputs();
  await screen.findByRole('button', { name: 'Download extra.pdf' });
  expect(screen.getAllByRole('button', { name: 'Download result.csv' })).toHaveLength(1);
  expect(screen.queryByRole('button', { name: 'Download input.csv' })).toBeNull();
  expect(screen.getByText('CSV · 0 B')).toBeTruthy();
  expect(requests).toHaveLength(2);
  expect(requests.every((url) => url.searchParams.get('scope_id') === 'sesn_outputs')).toBe(true);
});

test('keeps the transcript clean when no generated file is available', async () => {
  resetTestDom('https://oma.duck.ai/');
  const fetchMock = mock(async () => page([file('file_input', 'input.csv', false)]));
  globalThis.fetch = fetchMock as typeof fetch;
  renderOutputs();
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  await act(async () => await new Promise((resolve) => setTimeout(resolve, 20)));
  expect(screen.queryByTestId('session-output-files')).toBeNull();
});

test('does not attribute session outputs to a child lane or display them among filtered messages', () => {
  resetTestDom('https://oma.duck.ai/');
  const fetchMock = mock(async () => page([file('file_output')]));
  globalThis.fetch = fetchMock as typeof fetch;
  const view = renderOutputs({ mainLane: false });
  expect(screen.queryByTestId('session-output-files')).toBeNull();
  view.rerender({ filtered: true });
  expect(fetchMock).not.toHaveBeenCalled();
});

test('discards an old session request and its result after the session scope changes', async () => {
  resetTestDom('https://oma.duck.ai/');
  let resolveOld: ((response: Response) => void) | undefined;
  let oldSignal: AbortSignal | null | undefined;
  globalThis.fetch = async (input, init) =>
    new URL(String(input), 'https://oma.duck.ai').searchParams.get('scope_id') === 'sesn_outputs'
      ? new Promise<Response>((resolve) => {
          resolveOld = resolve;
          oldSignal = init?.signal;
        })
      : page([file('file_new', 'new-session.csv')]);
  const view = renderOutputs();
  await waitFor(() => expect(resolveOld).toBeDefined());
  view.rerender({ session: makeSession('sesn_new'), workspaceId: 'workspace_new' });
  await screen.findByRole('button', { name: 'Download new-session.csv' });
  expect(oldSignal?.aborted).toBe(true);
  await act(async () => resolveOld?.(page([file('file_old', 'old-session.csv')])));
  expect(screen.queryByRole('button', { name: 'Download old-session.csv' })).toBeNull();
});

test('discovers new output files during a run and refetches as the run becomes idle', async () => {
  resetTestDom('https://oma.duck.ai/');
  let files: FileMetadataApiResponse[] = [];
  const fetchMock = mock(async () => page(files));
  globalThis.fetch = fetchMock as typeof fetch;
  const view = renderOutputs({ session: makeSession('sesn_outputs', 'running') });
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  files = [file('file_during_run')];
  await screen.findByRole('button', { name: 'Download result.csv' }, { timeout: 4500 });
  files = [file('file_final', 'final.csv')];
  view.rerender({ session: makeSession() });
  await screen.findByRole('button', { name: 'Download final.csv' });
  expect(screen.queryByRole('button', { name: 'Download result.csv' })).toBeNull();
});

test('downloads from a chat card, guards double clicks, and restores the action after a download failure', async () => {
  resetTestDom('https://oma.duck.ai/');
  let resolveDownload: ((response: Response) => void) | undefined;
  let downloads = 0;
  globalThis.fetch = async (input) =>
    String(input).includes('/content?')
      ? new Promise<Response>((resolve) => {
          downloads++;
          resolveDownload = resolve;
        })
      : page([file('file_output')]);
  errorToast = spyOn(toast, 'error').mockImplementation(() => 'toast_error');
  URL.createObjectURL = mock(() => 'blob:output_file');
  const clicked = mock(function (this: HTMLAnchorElement) {
    expect(this.download).toBe('result.csv');
  });
  window.HTMLAnchorElement.prototype.click = clicked;
  renderOutputs();
  const button = await screen.findByRole('button', { name: 'Download result.csv' });
  fireEvent.click(button);
  fireEvent.click(button);
  await waitFor(() => expect(downloads).toBe(1));
  expect(button.getAttribute('aria-busy')).toBe('true');
  await act(async () =>
    resolveDownload?.(Response.json({ error: { type: 'api_error', message: 'Download failed' } }, { status: 500 })),
  );
  await waitFor(() => expect(errorToast).toHaveBeenCalledWith('Download failed'));
  expect(button.hasAttribute('disabled')).toBe(false);
  fireEvent.click(button);
  await waitFor(() => expect(downloads).toBe(2));
  await act(async () => resolveDownload?.(new Response('a,b\n1,2')));
  await waitFor(() => expect(clicked).toHaveBeenCalledTimes(1));
});

test('shows the newest two files first and expands and collapses the remaining files', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async () =>
    page([
      { ...file('old', 'old.txt'), created_at: '2026-10-12T08:00:00Z' },
      { ...file('new', 'new.txt'), created_at: '2026-10-12T08:02:00Z' },
      { ...file('middle', 'middle.txt'), created_at: '2026-10-12T08:01:00Z' },
    ]);
  renderOutputs();
  await screen.findByRole('button', { name: 'Download new.txt' });
  expect(
    screen.getAllByRole('button', { name: /^Download/ }).map((button) => button.getAttribute('aria-label')),
  ).toEqual(['Download new.txt', 'Download middle.txt']);
  expect(screen.queryByRole('button', { name: 'Download old.txt' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /Show all 3 files/ }));
  expect(await screen.findByRole('button', { name: 'Download old.txt' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: /Collapse files/ }));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Download old.txt' })).toBeNull());
});
