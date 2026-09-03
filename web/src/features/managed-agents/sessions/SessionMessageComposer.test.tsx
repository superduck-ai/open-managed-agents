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
    expect(frame?.className).toContain('border-session-border');
    expect(frame?.className).toContain('focus-visible]:ring-0');

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
});

function renderComposer({ live = false, onEventsChanged = () => {} } = {}) {
  return render(
    <I18nProvider initialLocale="en">
      <SessionMessageComposer
        disabled={false}
        live={live}
        onError={() => {}}
        onEventsChanged={onEventsChanged}
        onMessageSent={() => {}}
        sessionId="session-test"
        workspaceId="default"
      />
    </I18nProvider>,
  );
}

function jsonResponse(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' },
  });
}
