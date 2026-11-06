import { Context } from "@deepseek-ai/cordis";
import {
  SessionId,
  SessionSeq,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import { expect, spyOn, test } from "bun:test";
import Sessions from "../src/runtime/sessions.ts";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { SessionReplication } from "../src/runtime/replication.ts";
import {
  pendingBytes,
  updateReady,
  type ReplicationState,
} from "../src/runtime/replication-state.ts";
import type { DshLogData } from "../src/runtime/transcript-types.ts";

const id = SessionId("replication-bytes");
const header = { id, version: 4 as const, createdAt: 0, isSeeded: false };
function event(seq: number): SessionEvent {
  return {
    type: "turn/start",
    seq: SessionSeq(seq),
    time: 0,
    data: { turn: seq + 1 },
  };
}
function state(confirmed = 0): ReplicationState {
  return {
    chain: Promise.resolve(),
    failures: 0,
    confirmed,
    readyNext: confirmed,
    requestedNext: confirmed,
    headerConfirmed: true,
    headerBytes: 0,
    waiters: new Set(),
    byteBase: confirmed,
    byteEnds: [0],
    inFlight: false,
    abort: new AbortController(),
  };
}

test("restored confirmed events are not serialized to initialize the byte index", () => {
  const events = Array.from({ length: 1000 }, (_, index) => event(index));
  const owner = state(events.length);
  const stringify = spyOn(JSON, "stringify");
  try {
    updateReady(owner, { header, events });
    expect(stringify).not.toHaveBeenCalled();
    expect(owner.byteEnds).toEqual([0]);
    expect(owner.readyNext).toBe(1000);
    expect(pendingBytes(owner)).toBe(0);
  } finally {
    stringify.mockRestore();
  }
});

test("pending bytes include only new events after a restored prefix", () => {
  const events = [event(0), event(1), event(2), event(3)];
  const sizes = events.map((value) => Buffer.byteLength(JSON.stringify(value)));
  const owner = state(2);
  const stringify = spyOn(JSON, "stringify");
  try {
    updateReady(owner, { header, events });
    expect(stringify.mock.calls.map(([value]) => value)).toEqual(
      events.slice(2),
    );
    expect(pendingBytes(owner)).toBe(sizes[2]! + sizes[3]!);
    owner.confirmed = 3;
    expect(pendingBytes(owner)).toBe(sizes[3]!);
    updateReady(owner, { header, events });
    expect(stringify).toHaveBeenCalledTimes(2);
    owner.confirmed = 4;
    expect(pendingBytes(owner)).toBe(0);
    events.push(event(4));
    updateReady(owner, { header, events });
    expect(stringify).toHaveBeenCalledTimes(3);
    expect(pendingBytes(owner)).toBe(
      Buffer.byteLength(JSON.stringify(events[4])),
    );
  } finally {
    stringify.mockRestore();
  }
});

test("new transcripts account for their unconfirmed header and events", () => {
  const owner = state();
  owner.headerConfirmed = false;
  const events = [event(0)];
  updateReady(owner, { header, events });
  expect(pendingBytes(owner)).toBe(
    Buffer.byteLength(JSON.stringify(header)) +
      Buffer.byteLength(JSON.stringify(events[0])),
  );
  owner.headerConfirmed = true;
  expect(pendingBytes(owner)).toBe(
    Buffer.byteLength(JSON.stringify(events[0])),
  );
  owner.confirmed = 1;
  expect(pendingBytes(owner)).toBe(0);
});

test("ensure and repeated flush skip charging the downloaded confirmed history", async () => {
  const ctx = new Context();
  await ctx.plugin(Sessions);
  await ctx.plugin(TranscriptPersistence);
  const events: SessionEvent[] = Array.from(
    { length: 100 },
    (_, index) =>
      ({
        type: "probe/payload",
        seq: SessionSeq(index),
        time: 0,
        ignorable: true,
        data: { text: "x".repeat(1024) },
      }) as unknown as SessionEvent,
  );
  const writes: DshLogData[] = [];
  await ctx.plugin(SessionReplication, {
    remote: {
      read: async function* () {
        yield [header, ...events];
      },
      write: async (
        _root: string,
        _id: string,
        records: readonly DshLogData[],
      ) => {
        writes.push(...records);
      },
      fenced: () => false,
    },
  });
  try {
    await ctx.sessionReplication.ensure(id, { nextSeq: String(events.length) });
    const history = new Set(ctx.transcriptStore.view(id)!.events);
    const stringify = spyOn(JSON, "stringify");
    try {
      await ctx.sessionReplication.flush(id, AbortSignal.timeout(1000));
      await ctx.sessionReplication.flush(id, AbortSignal.timeout(1000));
      expect(
        stringify.mock.calls.filter(([value]) => history.has(value)),
      ).toHaveLength(0);
      expect(writes).toHaveLength(0);
      const writer = await ctx.sessionPersistence.open(id, "write");
      try {
        await writer.append([
          { ...events[0]!, seq: SessionSeq(events.length) },
        ]);
        await writer.flush({ signal: AbortSignal.timeout(1000) });
        expect(
          stringify.mock.calls.filter(([value]) => history.has(value)),
        ).toHaveLength(0);
        expect(writes).toEqual([
          ctx.transcriptStore.view(id)!.events[events.length]!,
        ]);
        expect(ctx.sessionReplication.status(id).pendingBytes).toBe(0);
      } finally {
        await writer.close();
      }
    } finally {
      stringify.mockRestore();
    }
  } finally {
    ctx.sessionReplication.cancel(id);
    await ctx.fiber.dispose();
  }
});
