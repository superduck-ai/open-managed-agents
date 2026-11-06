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
import type { DshLogData } from "../src/runtime/transcript-types.ts";

const id = SessionId("transcript-consistency");
const first: SessionEvent = {
  type: "turn/start",
  seq: SessionSeq(0),
  time: 0,
  data: { turn: 1 },
};

async function fixture() {
  const ctx = new Context();
  await ctx.plugin(Sessions);
  await ctx.plugin(TranscriptPersistence);
  const writes: DshLogData[] = [];
  await ctx.plugin(SessionReplication, {
    remote: {
      read: async function* () {},
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
  await ctx.sessionReplication.ensure(id, null);
  const session = ctx.sessions.prepare(id, { meta: { createdAt: 0 } });
  const writer = await ctx.sessionPersistence.create(session.header);
  const detach = ctx.sessions.enter(session);
  ctx.sessions.announce(session);
  return {
    ctx,
    session,
    writer,
    detach,
    writes,
    async stored() {
      const reader = await ctx.sessionPersistence.open(id, "read");
      try {
        return (await reader.read()).events;
      } finally {
        await reader.close();
      }
    },
    async close() {
      detach();
      ctx.sessionReplication.cancel(id);
      await writer.close().catch(() => {});
      await ctx.fiber.dispose();
    },
  };
}

test("a handle cannot append independently while its Session is live", async () => {
  const f = await fixture();
  try {
    await expect(f.writer.append([first])).rejects.toThrow("live Session");
    expect(await f.stored()).toEqual([]);
    f.session.append("turn/start", { turn: 1 });
    await f.ctx.sessionReplication.flush(id, AbortSignal.timeout(1000));
    expect(await f.stored()).toEqual(f.session.snapshotEvents());
    expect(f.writes.filter((event) => "seq" in event)).toEqual([
      ...(await f.stored()),
    ]);
  } finally {
    await f.close();
  }
});

test("events appended after writer close are captured before immediate detach", async () => {
  const f = await fixture();
  try {
    await f.writer.close();
    f.session.append("turn/start", { turn: 1 });
    f.detach();
    await f.ctx.sessionReplication.flush(id, AbortSignal.timeout(1000));
    expect(await f.stored()).toEqual(f.session.snapshotEvents());
    expect(f.writes.filter((event) => "seq" in event)).toEqual([
      ...(await f.stored()),
    ]);
    expect(f.writes.filter((event) => "seq" in event)).toHaveLength(1);
  } finally {
    await f.close();
  }
});

test("a capture listener failure rejects confirmation instead of silently uploading", async () => {
  const f = await fixture();
  const append = spyOn(TranscriptPersistence.prototype, "append");
  try {
    append.mockImplementationOnce(() => {
      throw new Error("capture rejected");
    });
    f.session.append("turn/start", { turn: 1 });
    await expect(
      f.ctx.sessionReplication.flush(id, AbortSignal.timeout(1000)),
    ).rejects.toThrow("Live transcript capture failed");
    await expect(f.ctx.sessionReplication.flush(id)).rejects.toThrow(
      "Live transcript capture failed",
    );
    expect(f.writes.filter((event) => "seq" in event)).toHaveLength(0);
  } finally {
    append.mockRestore();
    await f.close();
  }
});

test("a different Session prefix cannot attach to an existing transcript", async () => {
  const f = await fixture();
  try {
    f.detach();
    await f.writer.append([first]);
    expect(() =>
      f.ctx.sessions.create(id, {
        seed: [{ ...first, time: 123 }],
        meta: { createdAt: 0 },
      }),
    ).toThrow("Session history conflicts");
    expect(await f.stored()).toEqual([first]);
    await f.writer.flush({ signal: AbortSignal.timeout(1000) });
    expect(f.writes.filter((event) => "seq" in event)).toEqual([first]);
  } finally {
    await f.close();
  }
});

test("forgotten live Sessions cannot recreate a transcript through capture", async () => {
  const f = await fixture();
  try {
    f.ctx.sessionReplication.forget(id);
    f.session.append("turn/start", { turn: 1 });
    await expect(f.ctx.transcriptStore.capture(f.session)).rejects.toThrow();
    expect(await f.ctx.sessionPersistence.stat(id)).toBeUndefined();
  } finally {
    await f.close();
  }
});

test("detached handle appends upload and flush without a live Session", async () => {
  const f = await fixture();
  try {
    f.detach();
    await f.writer.append([first]);
    await f.writer.flush({ signal: AbortSignal.timeout(1000) });
    expect(await f.stored()).toEqual([first]);
    expect(f.writes.filter((event) => "seq" in event)).toEqual([first]);
  } finally {
    await f.close();
  }
});
