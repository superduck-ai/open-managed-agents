export interface SseFrame {
  id?: string;
  event?: string;
  data?: string;
}

export async function* sse(
  body: ReadableStream<Uint8Array>, signal?: AbortSignal, onFrame?: () => void,
): AsyncIterable<SseFrame> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let data: string[] = [];
  let size = 0;
  let id: string | undefined;
  let event: string | undefined;
  let comment = false;
  const cancel = () => { void reader.cancel().catch(() => {}); };
  signal?.addEventListener("abort", cancel, { once: true });
  try {
    while (true) {
      signal?.throwIfAborted();
      const { done, value } = await reader.read();
      signal?.throwIfAborted();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let newline: number;
      while ((newline = buffer.indexOf("\n")) !== -1) {
        const line = buffer.slice(0, newline).replace(/\r$/, "");
        buffer = buffer.slice(newline + 1);
        if (line.startsWith("data:")) {
          const part = line.slice(5).replace(/^ /, "");
          size += part.length;
          if (size > 4 * 1024 * 1024) throw new Error("SSE frame too large");
          data.push(part);
        } else if (line.startsWith("id:")) {
          const value = line.slice(3).replace(/^ /, "");
          if (!value.includes("\0")) id = value;
        } else if (line.startsWith("event:")) {
          event = line.slice(6).replace(/^ /, "");
        } else if (line.startsWith(":")) {
          comment = true;
        } else if (!line) {
          if (data.length || comment) {
            onFrame?.();
            yield { id, event, data: data.length ? data.join("\n") : undefined };
          }
          data = [];
          size = 0;
          id = undefined;
          event = undefined;
          comment = false;
        }
      }
      if (buffer.length + size > 4 * 1024 * 1024) throw new Error("SSE frame too large");
    }
  } finally {
    signal?.removeEventListener("abort", cancel);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
