import { transcriptResponse } from "./transcript-response.ts";
import { Context } from "@deepseek-ai/cordis";
import {
  SessionId,
  SessionSeq,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import { expect, test } from "bun:test";
import Sessions from "../src/runtime/sessions.ts";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { SessionReplication } from "../src/runtime/replication.ts";
import type { ReplicationState } from "../src/runtime/replication-state.ts";
import { withDownloadedTranscript } from "../src/runtime/transcript-download.ts";
import { DsrClient } from "../src/dsr/client.ts";
import { createRemoteTranscriptStore } from "../src/dsr/replication.ts";

const id = SessionId("completeness-probe");
const header = { id, version: 4 as const, createdAt: 0, isSeeded: false };
const history: SessionEvent[] = Array.from({ length: 4 }, (_, seq) =>
  seq % 2 === 0
    ? {
        type: "turn/start",
        seq: SessionSeq(seq),
        time: 0,
        data: { turn: seq / 2 + 1 },
      }
    : {
        type: "turn/end",
        seq: SessionSeq(seq),
        time: 0,
        data: { turn: (seq + 1) / 2, reason: { kind: "completed" } },
      },
);
interface Page {
  header: typeof header | null;
  events: SessionEvent[];
  next_from_seq: string | null;
}
async function fixture(nextSeq: string, pages: Page[]) {
  const calls: string[] = [];
  const client = new DsrClient(
    {
      request: async (request) => {
        calls.push(request.path);
        const url = new URL(request.path, "http://probe.local");
        if (url.pathname.endsWith("/transcripts"))
          return Response.json({
            data: [
              {
                dsh_session_id: id,
                parent_dsh_session_id: null,
                session_thread_id: null,
                next_offset: String(BigInt(nextSeq) + 1n),
              },
            ],
            next_after: null,
          });
        const index =
          calls.filter((path) => path.includes("?from_offset=")).length - 1;
        if (!pages[index]) throw new Error("Unexpected extra page request");
        const page = pages[index]!;
        return transcriptResponse(
          [...(page.header ? [page.header] : []), ...page.events],
          String(BigInt(nextSeq) + 1n),
          page.next_from_seq === null
            ? null
            : String(BigInt(page.next_from_seq) + 1n),
        );
      },
    },
    {
      code_session_id: id,
      worker_epoch: "1",
      session_token: "synthetic-session",
      model_token: "synthetic-model",
      claim_expires_at: "2026-11-06T00:00:00Z",
    },
  );
  const ctx = new Context();
  await ctx.plugin(Sessions);
  await ctx.plugin(TranscriptPersistence);
  await ctx.plugin(SessionReplication, {
    remote: createRemoteTranscriptStore(() => client),
  });
  await withDownloadedTranscript(
    id,
    (async function* () {
      yield [header, ...history];
    })(),
    () => {},
    async (remote) => {
      ctx.transcriptStore.replace(id, remote, () => {});
    },
  );
  const snapshot = () => {
    const owner = (
      ctx.sessionReplication as unknown as {
        jobs: Map<string, ReplicationState>;
      }
    ).jobs.get(id)!;
    return {
      declaredNextSeq: nextSeq,
      cachedCount: ctx.transcriptStore.eventCount(id),
      confirmed: owner.confirmed,
      acquired: owner.acquired ?? false,
      validated: owner.validated ?? false,
      directoryRequests: calls.filter((path) => !path.includes("?from_offset="))
        .length,
      pageRequests: calls.filter((path) => path.includes("?from_offset="))
        .length,
    };
  };
  return {
    ctx,
    client,
    snapshot,
    async close() {
      ctx.sessionReplication.cancel(id);
      await ctx.fiber.dispose();
    },
  };
}

for (const hinted of [false, true]) {
  test(`an early terminal page is rejected against directory next_seq=4, hint=${hinted}`, async () => {
    const f = await fixture("4", [
      { header, events: history.slice(0, 2), next_from_seq: null },
    ]);
    try {
      let snapshotHint: { nextOffset: string } | undefined;
      if (hinted) {
        const directory = await f.client.listTranscripts();
        expect(directory[0]!.next_offset).toBe("5");
        snapshotHint = { nextOffset: directory[0]!.next_offset };
      }
      await expect(
        f.ctx.sessionReplication.ensure(id, snapshotHint),
      ).rejects.toMatchObject({ code: "invalid_response" });
      const snapshot = f.snapshot();
      expect(snapshot.cachedCount).toBe(4);
      expect(snapshot.confirmed).toBe(0);
      expect(snapshot.acquired).toBe(false);
      expect(snapshot.validated).toBe(false);
      expect(snapshot.directoryRequests).toBe(1);
    } finally {
      await f.close();
    }
  });
}

test("a later empty terminal page cannot replace the existing transcript", async () => {
  const f = await fixture("4", [
    { header, events: history.slice(0, 2), next_from_seq: "2" },
    { header: null, events: [], next_from_seq: null },
  ]);
  try {
    const directory = await f.client.listTranscripts();
    await expect(
      f.ctx.sessionReplication.ensure(id, {
        nextOffset: directory[0]!.next_offset,
      }),
    ).rejects.toMatchObject({ code: "invalid_response" });
    const snapshot = f.snapshot();
    expect(snapshot.cachedCount).toBe(4);
    expect(snapshot.confirmed).toBe(0);
    expect(snapshot.pageRequests).toBe(2);
  } finally {
    await f.close();
  }
});

test("reading beyond the initial directory watermark is rejected", async () => {
  const f = await fixture("2", [
    { header, events: history.slice(0, 2), next_from_seq: "2" },
    { header: null, events: history.slice(2), next_from_seq: null },
  ]);
  try {
    await expect(f.ctx.sessionReplication.ensure(id)).rejects.toMatchObject({
      code: "invalid_response",
    });
    const snapshot = f.snapshot();
    expect(snapshot.cachedCount).toBe(4);
    expect(snapshot.confirmed).toBe(0);
  } finally {
    await f.close();
  }
});

test("a terminal page beyond the snapshot watermark cannot replace cache", async () => {
  const f = await fixture("2", [
    { header, events: history, next_from_seq: null },
  ]);
  try {
    await expect(f.ctx.sessionReplication.ensure(id)).rejects.toMatchObject({
      code: "invalid_response",
    });
    expect(f.snapshot().cachedCount).toBe(4);
    expect(f.snapshot().acquired).toBe(false);
    expect(f.snapshot().confirmed).toBe(0);
  } finally {
    await f.close();
  }
});

for (const malformed of ["sequence", "cursor"] as const) {
  test(`existing page validation rejects malformed ${malformed}`, async () => {
    const f = await fixture("4", [
      {
        header,
        events:
          malformed === "sequence"
            ? [history[0]!, history[2]!]
            : history.slice(0, 2),
        next_from_seq: malformed === "cursor" ? "3" : null,
      },
    ]);
    try {
      await expect(f.ctx.sessionReplication.ensure(id)).rejects.toMatchObject({
        code: "invalid_response",
      });
      expect(f.ctx.transcriptStore.eventCount(id)).toBe(4);
      expect(f.snapshot().acquired).toBe(false);
    } finally {
      await f.close();
    }
  });
}

test("complete pagination restores the declared number of events", async () => {
  const f = await fixture("4", [
    { header, events: history.slice(0, 2), next_from_seq: "2" },
    { header: null, events: history.slice(2), next_from_seq: null },
  ]);
  try {
    expect(await f.ctx.sessionReplication.ensure(id)).toBe(true);
    const snapshot = f.snapshot();
    expect(snapshot.confirmed).toBe(4);
    expect(snapshot.cachedCount).toBe(4);
  } finally {
    await f.close();
  }
});
