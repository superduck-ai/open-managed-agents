import type { RemoteTranscriptStore } from "../runtime/replication.ts";
import {
  transcriptBatch,
  TranscriptUploadError,
} from "../runtime/transcript-batch.ts";
import { DsrError, decimal } from "./http.ts";
import type { DsrClient } from "./client.ts";

export function createRemoteTranscriptStore(
  client: (rootSessionId: string) => DsrClient,
): RemoteTranscriptStore {
  return {
    read: async function* (rootSessionId, sessionId, signal, snapshot) {
      const api = client(rootSessionId);
      if (snapshot === null) return;
      let expected = snapshot?.nextOffset;
      if (expected === undefined) {
        const directory = await api.listTranscripts(signal);
        const transcript = directory.find(
          (item) => item.dsh_session_id === sessionId,
        );
        if (!transcript) return;
        expected = transcript.next_offset;
      }
      const end = BigInt(decimal(expected));
      let count = 0n;
      let cursor: string | null = "0";
      do {
        const page = await api.readTranscript(sessionId, cursor, signal);
        count += BigInt(page.records.length);
        if (
          page.next_offset !== expected ||
          count > end ||
          (page.next_from_offset === null ? count !== end : count >= end)
        )
          throw new DsrError(0, "invalid_response");
        yield page.records;
        cursor = page.next_from_offset;
      } while (cursor !== null);
    },

    write: async (rootSessionId, sessionId, records, signal) => {
      const api = client(rootSessionId);
      for (let index = 0; index < records.length;) {
        const batch = transcriptBatch(records, index);
        const first = batch[0]!;
        const from = "seq" in first ? BigInt(first.seq) + 1n : 0n;
        const parent = "seq" in first ? null : (first.parentSession ?? null);
        const next = await upload(() =>
          api.appendRecords(sessionId, String(from), batch, parent, signal),
        );
        if (BigInt(decimal(next)) < from + BigInt(batch.length))
          throw new TranscriptUploadError(
            "Unexpected transcript append watermark",
          );
        index += batch.length;
      }
    },

    fenced: (error) =>
      error instanceof TranscriptUploadError ||
      (error instanceof DsrError &&
        ([
          "epoch_superseded",
          "unauthorized",
          "transcript_conflict",
          "transcript_gap",
          "invalid_response",
        ].includes(error.code) ||
          [400, 401, 403, 404, 410, 413, 422].includes(error.status))),
  };
}

async function upload<T>(send: () => Promise<T>): Promise<T> {
  try {
    return await send();
  } catch (error) {
    if (error instanceof DsrError && [400, 413, 422].includes(error.status))
      throw new TranscriptUploadError("Transcript batch cannot be uploaded", {
        cause: error,
      });
    throw error;
  }
}
