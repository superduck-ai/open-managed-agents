import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { DshLogData } from "../runtime/transcript-types.ts";
import type { NativeTranscriptPort } from "../runtime/replication.ts";
import { DsrError } from "./http.ts";
import type { DsrClient } from "./client.ts";

export function dsrTranscriptPort(client: (root: string) => DsrClient): NativeTranscriptPort {
  return {
    read: async (root, id) => {
      const api = client(root);
      const directory = await api.listTranscripts();
      if (!directory.some(item => item.dsh_session_id === id)) return [];
      const records: DshLogData[] = [];
      let cursor: string | null = "0";
      do {
        const page = await api.readTranscript(id, cursor);
        if (page.header) records.push(page.header);
        records.push(...page.events);
        cursor = page.next_from_seq;
      } while (cursor !== null);
      return records;
    },
    write: async (root, id, records) => {
      const api = client(root);
      const headers = records.filter((record): record is SessionHeader => !("seq" in record));
      for (const header of headers) await api.putHeader(id, header, header.parentSession ?? null);
      const events = records.filter((record): record is SessionEvent => "seq" in record);
      for (let i = 0; i < events.length; i += 200) await appendBounded(api, id, events.slice(i, i + 200));
    },
    fenced: error => error instanceof DsrError && ["epoch_superseded", "unauthorized", "transcript_conflict", "transcript_purged"].includes(error.code),
  };
}

async function appendBounded(client: DsrClient, id: string, events: readonly SessionEvent[]): Promise<void> {
  try { await client.appendEvents(id, events); }
  catch (error) {
    if (!(error instanceof DsrError) || error.status !== 413 || events.length < 2) throw error;
    const middle = Math.floor(events.length / 2);
    await appendBounded(client, id, events.slice(0, middle));
    await appendBounded(client, id, events.slice(middle));
  }
}
