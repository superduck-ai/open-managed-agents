import { SessionLogOffset, type SessionId } from "@deepseek-ai/dsh-session";
import {
  SessionHandleClosedError,
  SessionReadOnlyError,
  type SessionAccess,
  type SessionHandle,
} from "@deepseek-ai/dsh-session-persistence";
import type TranscriptPersistence from "./transcript-persistence.ts";
import type { TranscriptRecord } from "./transcript-persistence.ts";

export function transcriptHandle(
  store: TranscriptPersistence,
  id: SessionId,
  record: TranscriptRecord,
  access: SessionAccess,
): SessionHandle {
  let closed = false;
  let closing: Promise<void> | undefined;
  function assertOpen(operation: string) {
    if (closed) throw new SessionHandleClosedError(id, operation);
    store.assertRecord(id, record);
  }
  const handle: SessionHandle = {
    id,
    header: record.header,
    inheritedEventCount: SessionLogOffset(0),
    access,
    async read(offset = 0, length = Number.MAX_SAFE_INTEGER, options) {
      assertOpen("read");
      options?.signal?.throwIfAborted();
      if (
        !Number.isSafeInteger(offset) ||
        offset < 0 ||
        !Number.isSafeInteger(length) ||
        length < 0
      )
        throw new TypeError(
          "Transcript read requires non-negative safe integers",
        );
      return {
        eventState: "shared-frozen",
        events: record.events.slice(offset, offset + length),
      };
    },
    async append(events, options) {
      assertOpen("append");
      options?.signal?.throwIfAborted();
      if (access !== "write") throw new SessionReadOnlyError(id, "append");
      store.append(id, record, events);
    },
    async flush(options) {
      assertOpen("flush");
      options?.signal?.throwIfAborted();
      if (access !== "write") throw new SessionReadOnlyError(id, "flush");
      await store.confirm(id, record, options?.signal);
    },
    close() {
      return (closing ??= (async () => {
        closed = true;
        try {
          if (access === "write" && store.owns(id, record, handle))
            await store.confirm(id, record, AbortSignal.timeout(5000));
        } finally {
          store.release(id, handle);
        }
      })());
    },
    async [Symbol.asyncDispose]() {
      await handle.close();
    },
  };
  return handle;
}
