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
import { MarkdownTranscriptContent } from './SessionTranscriptContent';
import { sessionFilesQueryKey } from './sessionFiles';

const { act, cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
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
  const outputCard = screen.getByRole('button', { name: 'Preview result.csv' });
  expect(within(outputCard).getByText('CSV')).toBeTruthy();
  expect(within(outputCard).getByText('0 B')).toBeTruthy();
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

test('discovers files published after the idle refresh without opening resources or refocusing', async () => {
  resetTestDom('https://oma.duck.ai/');
  let files: FileMetadataApiResponse[] = [];
  const fetchMock = mock(async () => page(files));
  globalThis.fetch = fetchMock as typeof fetch;
  const view = renderOutputs({ session: makeSession('sesn_outputs', 'running') });
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  view.rerender({ session: makeSession() });
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  files = [file('file_late', 'late.txt')];
  await screen.findByRole('button', { name: 'Download late.txt' }, { timeout: 2500 });
});

test('keeps unsupported file types as a single download card', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async () => page([file('file_deck', 'deck.pptx')]);
  renderOutputs();
  await screen.findByRole('button', { name: 'Download deck.pptx' });
  expect(screen.queryByRole('button', { name: 'Preview deck.pptx' })).toBeNull();
});

test('reports a failed preview load and still offers the download', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async (input) =>
    String(input).includes('/content?')
      ? Response.json({ error: { type: 'api_error', message: 'Content offline' } }, { status: 500 })
      : page([file('file_output')]);
  renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview result.csv' }));
  const preview = await screen.findByTestId('session-file-preview');
  expect((await within(preview).findByRole('alert')).textContent).toContain('Content offline');
  expect(within(preview).getByRole('button', { name: 'Download result.csv' })).toBeTruthy();
});

test('previews a table from the card and closes without downloading', async () => {
  resetTestDom('https://oma.duck.ai/');
  const clicked = mock(() => undefined);
  window.HTMLAnchorElement.prototype.click = clicked;
  globalThis.fetch = async (input) =>
    String(input).includes('/content?') ? new Response('name,total\n"Doe, Jane",42') : page([file('file_output')]);
  renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview result.csv' }));
  const preview = await screen.findByTestId('session-file-preview');
  expect((await within(preview).findByRole('columnheader', { name: 'total' })).textContent).toBe('total');
  expect(within(preview).getByRole('cell', { name: 'Doe, Jane' })).toBeTruthy();
  fireEvent.click(within(preview).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByTestId('session-file-preview')).toBeNull());
  expect(clicked).not.toHaveBeenCalled();
});

test('loads HTML previews in a script-only sandboxed frame with no referrer', async () => {
  resetTestDom('https://oma.duck.ai/');
  URL.createObjectURL = mock(() => 'blob:preview_page');
  globalThis.fetch = async (input) =>
    String(input).includes('/content?') ? new Response('<p>page</p>') : page([file('file_page', 'page.html')]);
  renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview page.html' }));
  const preview = await screen.findByTestId('session-file-preview');
  const frame = await waitFor(() => {
    const element = preview.querySelector('iframe');
    if (!element) throw new Error('frame not rendered');
    return element;
  });
  expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
  expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer');
  expect(frame.hasAttribute('srcdoc')).toBe(false);
  expect(frame.getAttribute('src')).toBe('blob:preview_page');
});

test('cancels the content request when the preview closes while loading', async () => {
  resetTestDom('https://oma.duck.ai/');
  let contentSignal: AbortSignal | null | undefined;
  globalThis.fetch = async (input, init) => {
    if (!String(input).includes('/content?')) return page([file('file_output')]);
    contentSignal = init?.signal;
    return new Promise<Response>(() => undefined);
  };
  renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview result.csv' }));
  const preview = await screen.findByTestId('session-file-preview');
  expect(within(preview).getByRole('status').textContent).toBe('Loading...');
  await waitFor(() => expect(contentSignal).toBeDefined());
  fireEvent.click(within(preview).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(contentSignal?.aborted).toBe(true));
});

test('keeps an open preview when the output list refreshes with a new file', async () => {
  resetTestDom('https://oma.duck.ai/');
  let files = [file('file_output')];
  globalThis.fetch = async (input) => (String(input).includes('/content?') ? new Response('name\nkept') : page(files));
  const { client } = renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview result.csv' }));
  const preview = await screen.findByTestId('session-file-preview');
  await within(preview).findByRole('cell', { name: 'kept' });
  files = [file('file_output'), file('file_new', 'new.txt')];
  await act(async () => {
    await client.invalidateQueries({ queryKey: sessionFilesQueryKey('workspace_outputs', 'sesn_outputs') });
  });
  await screen.findByRole('button', { name: 'Preview new.txt', hidden: true });
  expect(within(screen.getByTestId('session-file-preview')).getByRole('cell', { name: 'kept' })).toBeTruthy();
});

test('loads PDF previews without a referrer and reports images that cannot be decoded', async () => {
  resetTestDom('https://oma.duck.ai/');
  URL.createObjectURL = mock(() => 'blob:preview_binary');
  globalThis.fetch = async (input) =>
    String(input).includes('/content?')
      ? new Response('not really binary')
      : page([file('file_pdf', 'report.pdf'), file('file_image', 'chart.png')]);
  renderOutputs();
  fireEvent.click(await screen.findByRole('button', { name: 'Preview report.pdf' }));
  const frame = await waitFor(() => {
    const element = screen.getByTestId('session-file-preview').querySelector('iframe');
    if (!element) throw new Error('frame not rendered');
    return element;
  });
  expect(frame.hasAttribute('sandbox')).toBe(false);
  expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer');
  fireEvent.click(within(screen.getByTestId('session-file-preview')).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByTestId('session-file-preview')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Preview chart.png' }));
  const image = await within(await screen.findByTestId('session-file-preview')).findByRole('img');
  expect(image.getAttribute('referrerpolicy')).toBe('no-referrer');
  fireEvent.error(image);
  expect(within(screen.getByTestId('session-file-preview')).getByRole('alert')).toBeTruthy();
  expect(
    within(screen.getByTestId('session-file-preview')).getByRole('button', { name: 'Download chart.png' }),
  ).toBeTruthy();
});

test('opens the preview from a transcript link, offers copy, and toggles the expanded size', async () => {
  resetTestDom('https://oma.duck.ai/');
  const clicked = mock(() => undefined);
  window.HTMLAnchorElement.prototype.click = clicked;
  const session = {
    ...makeSession(),
    resources: [{ type: 'file', file_id: 'file_code', mount_path: '/outputs/main.py' }],
  } as unknown as SessionApiResponse;
  globalThis.fetch = async (input) =>
    String(input).includes('/content?') ? new Response('print(42)') : page([file('file_code', 'main.py')]);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <SessionOutputFiles session={session} workspaceId="workspace_outputs" refreshKey={0}>
          <MarkdownTranscriptContent value="See [main.py](/outputs/main.py)" />
        </SessionOutputFiles>
      </I18nProvider>
    </QueryClientProvider>,
  );
  const link = await waitFor(() => {
    const element = screen.getByRole('link', { name: 'main.py' });
    if (element.getAttribute('href') !== '#download-file') throw new Error('file not resolved');
    return element;
  });
  fireEvent.click(link);
  const preview = await screen.findByTestId('session-file-preview');
  await waitFor(() => expect(preview.querySelector('pre')?.textContent).toBe('print(42)'));
  expect(clicked).not.toHaveBeenCalled();
  expect(within(preview).getByRole('button', { name: 'Copy' })).toBeTruthy();
  const expand = within(preview).getByRole('button', { name: 'Expand preview' });
  fireEvent.click(expand);
  expect(within(preview).getByRole('button', { name: 'Restore size' }).getAttribute('aria-pressed')).toBe('true');
});
