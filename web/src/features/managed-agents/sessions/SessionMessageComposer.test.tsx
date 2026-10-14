import { afterEach, describe, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { resetTestDom } from '../../../test/setup';
import { SessionMessageComposer } from './SessionMessageComposer';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

describe('SessionMessageComposer', () => {
  test.each(['upload', 'mount', 'message'])('preserves attachments and resumes after a %s failure', async (stage) => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    const counts = { upload: 0, mount: 0, message: 0 };
    let fail = true;
    const onError = mock(() => {});
    const onMessageSent = mock(() => {});
    const onResourcesChanged = mock(() => {});
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      if (
        !['/v1/files', '/v1/sessions/session-test/resources', '/v1/sessions/session-test/events'].includes(url.pathname)
      )
        return jsonResponse({});
      const step = url.pathname === '/v1/files' ? 'upload' : url.pathname.endsWith('/resources') ? 'mount' : 'message';
      counts[step]++;
      if (fail && step === stage)
        return new Response(JSON.stringify({ error: { type: 'invalid_request_error', message: 'Try again' } }), {
          status: 400,
        });
      if (step === 'upload') return jsonResponse({ id: 'file_uploaded', type: 'file', filename: 'input.csv' });
      if (step === 'mount') return jsonResponse({ id: 'resource_input', type: 'file', file_id: 'file_uploaded' });
      return jsonResponse({ data: [] });
    }) as typeof fetch;
    renderComposer({ onError, onMessageSent, onResourcesChanged });
    const input = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'Read the attachment' } });
    fireEvent.change(screen.getByLabelText('Choose files to upload'), {
      target: { files: [new File(['a,b'], 'input.csv', { type: 'text/csv' })] },
    });
    fireEvent.submit(screen.getByTestId('session-message-composer'));
    await waitFor(() => expect(onError).toHaveBeenCalledWith('Try again'));
    expect(input.value).toBe('Read the attachment');
    expect(screen.getByText('input.csv')).toBeTruthy();
    expect(onMessageSent).not.toHaveBeenCalled();
    fail = false;
    fireEvent.submit(screen.getByTestId('session-message-composer'));
    await waitFor(() => expect(onMessageSent).toHaveBeenCalledTimes(1));
    expect(counts.upload).toBe(stage === 'upload' ? 2 : 1);
    expect(counts.mount).toBe(stage === 'mount' ? 2 : 1);
    expect(counts.message).toBe(stage === 'message' ? 2 : 1);
    expect(onResourcesChanged).toHaveBeenCalledTimes(1);
    expect(input.value).toBe('');
    expect(screen.queryByText('input.csv')).toBeNull();
  });

  test('sends multiple same-name attachments without text after all files are mounted', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    const requests: { url: URL; init?: RequestInit }[] = [];
    let uploaded = 0;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      requests.push({ url, init });
      if (url.pathname === '/v1/files')
        return jsonResponse({ id: `file_${++uploaded}`, type: 'file', filename: 'input.csv' });
      return jsonResponse(
        url.pathname.endsWith('/events') ? { data: [] } : { id: `resource_${uploaded}`, type: 'file' },
      );
    }) as typeof fetch;
    const onMessageSent = mock(() => {});
    renderComposer({ onMessageSent });
    fireEvent.change(screen.getByLabelText('Choose files to upload'), {
      target: { files: [new File(['first'], 'input.csv'), new File(['second'], 'input.csv')] },
    });
    fireEvent.submit(screen.getByTestId('session-message-composer'));
    fireEvent.submit(screen.getByTestId('session-message-composer'));
    await waitFor(() => expect(onMessageSent).toHaveBeenCalledTimes(1));
    expect(requests.map(({ url }) => url.pathname)).toEqual([
      '/v1/files',
      '/v1/sessions/session-test/resources',
      '/v1/files',
      '/v1/sessions/session-test/resources',
      '/v1/sessions/session-test/events',
    ]);
    const mounts = [requests[1], requests[3]].map(({ init }) => JSON.parse(String(init?.body)).mount_path as string);
    expect(mounts[0]).not.toBe(mounts[1]);
    const text = JSON.parse(String(requests[4].init?.body)).events[0].content[0].text as string;
    for (const mount of mounts) expect(text).toContain(`/mnt/session/uploads${mount}`);
    expect(requests[0].init?.body).toBeInstanceOf(FormData);
    expect((requests[0].init?.body as FormData).get('file')).toBeInstanceOf(File);
    expect(requests[0].url.searchParams.get('beta')).toBe('true');
    expect(new Headers(requests[0].init?.headers).get('anthropic-beta')).toContain('files-api-2025-04-14');
    expect(new Headers(requests[0].init?.headers).get('x-workspace-id')).toBe('default');
  });

  test('removes a selected attachment before sending', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    renderComposer();
    fireEvent.change(screen.getByLabelText('Choose files to upload'), {
      target: { files: [new File(['x'], 'input.csv')] },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Remove input.csv' }));
    expect(screen.queryByText('input.csv')).toBeNull();
    expect(screen.getByRole('button', { name: 'Send message' }).hasAttribute('disabled')).toBe(true);
  });

  test('uses a compact form and protects Enter submission from IME, repeats, and duplicate posts', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    let resolveRequest: ((response: Response) => void) | undefined;
    const fetchMock = mock(
      () =>
        new Promise<Response>((resolve) => {
          resolveRequest = resolve;
        }),
    );
    globalThis.fetch = fetchMock as typeof fetch;

    renderComposer();

    const form = screen.getByTestId('session-message-composer');
    const input = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement;
    expect(form.tagName).toBe('FORM');
    expect(input.getAttribute('rows')).toBe('1');
    const send = screen.getByRole('button', { name: 'Send message' });
    const frame = form.querySelector('[data-slot="input-group"]');
    expect(send.getAttribute('type')).toBe('submit');
    expect(send.className).toContain('hover:bg-primary/80');
    expect(send.className).not.toContain('dark:hover:bg-muted/50');
    expect(frame?.className).toContain('min-h-14');
    expect(frame?.className).toContain('rounded-[22px]');
    expect(frame?.className).toContain('border-0');
    expect(frame?.className).toContain('focus-visible]:ring-1');

    fireEvent.change(input, { target: { value: 'Keep this draft' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
    fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
    fireEvent.keyDown(input, { key: 'Enter', repeat: true });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(input.value).toBe('Keep this draft');

    fireEvent.keyDown(input, { key: 'Enter' });
    fireEvent.submit(form);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    resolveRequest?.(jsonResponse({ data: [] }));
    await waitFor(() => expect(input.value).toBe(''));
  });

  test('keeps Stop as a non-submit action and interrupts once', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    let resolveRequest: ((response: Response) => void) | undefined;
    const fetchMock = mock(
      () =>
        new Promise<Response>((resolve) => {
          resolveRequest = resolve;
        }),
    );
    globalThis.fetch = fetchMock as typeof fetch;
    const onEventsChanged = mock(() => {});

    renderComposer({ live: true, onEventsChanged });

    const stop = screen.getByRole('button', { name: 'Stop session' });
    expect(stop.getAttribute('type')).toBe('button');
    fireEvent.click(stop);
    fireEvent.click(stop);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body)) as {
      events: Array<{ type: string }>;
    };
    expect(body.events[0]?.type).toBe('user.interrupt');
    resolveRequest?.(jsonResponse({}));
    await waitFor(() => expect(onEventsChanged).toHaveBeenCalledTimes(1));
  });

  test('keeps a draft without sending while the session is busy', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    const fetchMock = mock(() => Promise.resolve(jsonResponse({ data: [] })));
    globalThis.fetch = fetchMock as typeof fetch;
    renderComposer({ acceptingMessages: false });
    const input = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'draft' } });
    fireEvent.submit(screen.getByTestId('session-message-composer'));
    expect(fetchMock).not.toHaveBeenCalled();
    expect(input.value).toBe('draft');
    expect(
      screen.getByTestId('session-message-composer').querySelector('button[type="submit"]')?.hasAttribute('disabled'),
    ).toBe(true);
  });
});

function renderComposer(overrides: Partial<Parameters<typeof SessionMessageComposer>[0]> = {}) {
  return render(
    <I18nProvider initialLocale="en">
      <SessionMessageComposer
        acceptingMessages={true}
        disabled={false}
        live={false}
        onError={() => {}}
        onEventsChanged={() => {}}
        onMessageSent={() => {}}
        sessionId="session-test"
        workspaceId="default"
        {...overrides}
      />
    </I18nProvider>,
  );
}

function jsonResponse(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' },
  });
}
