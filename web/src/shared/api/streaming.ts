export type ServerSentEvent<TData = Record<string, unknown>> = {
  event?: string;
  data: TData;
};

export async function postJsonSseStream<TData = Record<string, unknown>>({
  url,
  headers,
  body,
  signal,
  credentials = 'include',
  onEvent,
  errorFromResponse
}: {
  url: string;
  headers?: HeadersInit;
  body: unknown;
  signal?: AbortSignal;
  credentials?: RequestCredentials;
  onEvent: (event: ServerSentEvent<TData>) => void;
  errorFromResponse?: (response: Response) => Promise<Error>;
}) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('Accept', 'text/event-stream');
  if (!requestHeaders.has('Content-Type')) {
    requestHeaders.set('Content-Type', 'application/json');
  }

  const response = await fetch(url, {
    method: 'POST',
    credentials,
    headers: requestHeaders,
    body: JSON.stringify(body),
    signal
  });

  if (!response.ok) {
    throw errorFromResponse ? await errorFromResponse(response) : await streamError(response);
  }
  if (!response.body) {
    throw new Error('The stream did not include a response body.');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const parsed = consumeSseBuffer<TData>(buffer);
    buffer = parsed.remaining;
    parsed.events.forEach(onEvent);
  }
  buffer += decoder.decode();
  consumeSseBuffer<TData>(`${buffer}\n\n`).events.forEach(onEvent);
}

export function consumeSseBuffer<TData = Record<string, unknown>>(buffer: string) {
  const events: ServerSentEvent<TData>[] = [];
  let cursor = buffer.indexOf('\n\n');
  while (cursor !== -1) {
    const frame = buffer.slice(0, cursor);
    buffer = buffer.slice(cursor + 2);
    const event = parseSseFrame<TData>(frame);
    if (event) {
      events.push(event);
    }
    cursor = buffer.indexOf('\n\n');
  }
  return { events, remaining: buffer };
}

export function parseSseFrame<TData = Record<string, unknown>>(frame: string): ServerSentEvent<TData> | null {
  let event: string | undefined;
  const dataLines: string[] = [];
  frame.split('\n').forEach((line) => {
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim();
      return;
    }
    if (line.startsWith('data:')) {
      dataLines.push(line.slice('data:'.length).trimStart());
    }
  });
  if (!dataLines.length) {
    return null;
  }
  const dataText = dataLines.join('\n');
  if (dataText === '[DONE]') {
    return null;
  }
  return { event, data: JSON.parse(dataText) as TData };
}

async function streamError(response: Response) {
  let message = response.statusText || `Request failed with status ${response.status}`;
  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (error && typeof error === 'object' && typeof (error as Record<string, unknown>).message === 'string') {
      message = String((error as Record<string, unknown>).message);
    } else if (typeof error === 'string') {
      message = error;
    } else if (typeof payload.message === 'string') {
      message = payload.message;
    }
  } catch {
    // Keep the HTTP status fallback.
  }
  return new Error(message);
}
