import { jsonResponse } from '../ManagedAgentsPage.test-utils';

export function installSessionRuntime() {
  const baseFetch = globalThis.fetch;
  const events: Record<string, unknown>[] = [];
  const sent: Record<string, unknown>[] = [];
  let session: Record<string, unknown> | null = null;
  let stream: ReadableStreamDefaultController<Uint8Array> | null = null;
  const emit = (event: Record<string, unknown>) => {
    events.push(event);
    stream?.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));
  };
  let sequence = 0;
  let loseSend = false;
  let sendResponse: Promise<void> | null = null;
  const event = (type: string, fields: Record<string, unknown> = {}) => ({
    id: `sevt_test_${++sequence}`,
    type,
    processed_at: new Date(Date.UTC(2026, 9, 8, 12, 0, sequence)).toISOString(),
    ...fields,
  });
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    if (url.includes('/events/stream?'))
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            stream = controller;
            controller.enqueue(new TextEncoder().encode(': connected\n\n'));
            init?.signal?.addEventListener(
              'abort',
              () => {
                controller.close();
                if (stream === controller) stream = null;
              },
              { once: true },
            );
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    if (url.includes('/events?') && method === 'GET') return jsonResponse({ data: events, next_page: null });
    if (url.includes('/events?') && method === 'POST') {
      const body = JSON.parse(String(init?.body));
      const inputEvent = body.events[0];
      sent.push(inputEvent);
      const accepted = event(inputEvent.type, inputEvent);
      emit(accepted);
      if (session) session.status = 'running';
      if (sendResponse) {
        await sendResponse;
        sendResponse = null;
      }
      if (loseSend) {
        loseSend = false;
        throw new TypeError('Sending response lost');
      }
      return jsonResponse({ data: [accepted] });
    }
    if (/\/v1\/sessions\/[^/]+\?/.test(url) && session) return jsonResponse(session);
    const response = await baseFetch(input, init);
    if (url.startsWith('/v1/sessions?') && method === 'POST') session = await response.clone().json();
    return response;
  }) as typeof fetch;
  return {
    sent,
    holdNextSendResponse() {
      let release!: () => void;
      sendResponse = new Promise<void>((resolve) => {
        release = resolve;
      });
      return release;
    },
    loseNextSend() {
      loseSend = true;
    },
    emit(type: string, fields: Record<string, unknown>) {
      emit(event(type, fields));
    },
    reply(text: string) {
      emit(event('agent.message', { content: [{ type: 'text', text }] }));
      emit(event('session.status_idle', { stop_reason: { type: 'end_turn' } }));
      if (session) session.status = 'idle';
    },
  };
}
