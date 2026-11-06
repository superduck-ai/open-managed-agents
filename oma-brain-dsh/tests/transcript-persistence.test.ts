import { Context } from "@deepseek-ai/cordis";
import {
  Session,
  SessionId,
  SessionLogOffset,
  SessionSeq,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import { AttachmentId } from "@deepseek-ai/dsh-attachment";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { imageOffloadProjection } from "@deepseek-ai/dsh-compaction-image-offload/projection";
import { expect, test } from "bun:test";
import SessionStore from "../src/runtime/sessions.ts";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { withDownloadedTranscript } from "../src/runtime/transcript-download.ts";

const header = {
  id: SessionId("persistence-test"),
  version: 4 as const,
  createdAt: 0,
  isSeeded: false,
};
const event = (seq: number): SessionEvent =>
  seq === 0
    ? { type: "turn/start", seq: SessionSeq(seq), time: 0, data: { turn: 1 } }
    : {
        type: "turn/end",
        seq: SessionSeq(seq),
        time: 0,
        data: { turn: 1, reason: { kind: "completed" } },
      };

async function fixture(
  flush: (
    id: string,
    through: number,
    signal?: AbortSignal,
  ) => Promise<void> = async () => {},
) {
  const ctx = new Context();
  await ctx.plugin(SessionStore);
  ctx.sessions.registerMessageProjection(imageOffloadProjection);
  await ctx.plugin(TranscriptPersistence);
  ctx.transcriptStore.connect({ changed: () => {}, flush });
  return ctx;
}

test("invalid lifecycle and image offload references cannot replace a restored transcript", async () => {
  const ctx = await fixture();
  try {
    const writer = await ctx.sessionPersistence.create(header);
    await writer.append([event(0)]);
    await writer.close();
    for (const invalid of [
      [{ ...event(1), seq: SessionSeq(0) }],
      [
        event(0),
        {
          type: "image/offload",
          seq: SessionSeq(1),
          time: 0,
          data: { targets: [{ seq: 0, imageIndexes: [0] }] },
        },
      ],
    ]) {
      await expect(
        withDownloadedTranscript(
          header.id,
          (async function* () {
            yield [header, ...invalid] as never;
          })(),
          () => {},
          async (remote) =>
            ctx.transcriptStore.replace(header.id, remote, () => {}),
        ),
      ).rejects.toThrow();
      const reader = await ctx.sessionPersistence.open(header.id, "read");
      expect((await reader.read()).events).toEqual([event(0)]);
      await reader.close();
    }
  } finally {
    await ctx.fiber.dispose();
  }
});

test("handles reject conflicting owners, gaps, read-only writes and use after close", async () => {
  const ctx = await fixture();
  try {
    const writer = await ctx.sessionPersistence.create(header);
    await expect(ctx.sessionPersistence.create(header)).rejects.toThrow();
    await expect(
      ctx.sessionPersistence.open(header.id, "write"),
    ).rejects.toThrow();
    await expect(writer.append([event(1)])).rejects.toThrow("seq mismatch");
    const reader = await ctx.sessionPersistence.open(header.id, "read");
    await expect(reader.append([event(0)])).rejects.toThrow();
    await expect(reader.read(-1)).rejects.toThrow();
    await reader.close();
    await expect(reader.read()).rejects.toThrow();
    await writer.close();
  } finally {
    await ctx.fiber.dispose();
  }
});

test("a timed out flush rejects without acknowledging the buffered event", async () => {
  let failing = true;
  const ctx = await fixture(async (_id, _through, signal) => {
    if (failing)
      await new Promise<void>((_resolve, reject) => {
        signal!.addEventListener("abort", () => reject(signal!.reason), {
          once: true,
        });
      });
  });
  try {
    const writer = await ctx.sessionPersistence.create(header);
    await writer.append([event(0)]);
    await expect(
      writer.flush({ signal: AbortSignal.timeout(5) }),
    ).rejects.toThrow();
    expect((await writer.read()).events).toEqual([event(0)]);
    failing = false;
    await writer.flush();
    await writer.close();
  } finally {
    await ctx.fiber.dispose();
  }
});

test("closing a reopened writer confirms a previously failed prefix", async () => {
  let attempts = 0;
  const ctx = await fixture(async () => {
    if (++attempts === 1) throw new Error("remote unavailable");
  });
  try {
    const writer = await ctx.sessionPersistence.create(header);
    await writer.append([event(0)]);
    await expect(writer.close()).rejects.toThrow("remote unavailable");
    const retry = await ctx.sessionPersistence.open(header.id, "write");
    await retry.close();
    expect(attempts).toBe(2);
  } finally {
    await ctx.fiber.dispose();
  }
});

test("append remains available during remote confirmation and flush captures its prefix", async () => {
  const gate = Promise.withResolvers<void>();
  const targets: number[] = [];
  const ctx = await fixture(async (_id, through) => {
    targets.push(through);
    await gate.promise;
  });
  try {
    const writer = await ctx.sessionPersistence.create(header);
    const original = event(0);
    await writer.append([original]);
    if (original.type === "turn/start") original.data.turn = 99;
    let done = false;
    const flushed = writer.flush().then(() => {
      done = true;
    });
    await writer.append([event(1)]);
    expect(done).toBe(false);
    expect(targets).toEqual([1]);
    const reader = await ctx.sessionPersistence.open(header.id, "read");
    const read = await reader.read();
    expect(read.eventState).toBe("shared-frozen");
    expect(read.events).toEqual([event(0), event(1)]);
    expect(Object.isFrozen(read.events[0].data)).toBe(true);
    expect(
      (await ctx.sessionPersistence.stat(header.id))!.location,
    ).toBeUndefined();
    await reader.close();
    gate.resolve();
    await flushed;
    await writer.close();
    expect(targets).toEqual([1, 2]);
  } finally {
    gate.resolve();
    await ctx.fiber.dispose();
  }
});

test("remote-only history preserves image offload projection and attachment references", async () => {
  const ctx = await fixture();
  try {
    const source = Session.create(header.id, undefined, undefined, undefined, [
      imageOffloadProjection,
    ]);
    source.append("turn/start", { turn: 1 });
    const attachment = {
      attachmentId: AttachmentId(`sha256:${"a".repeat(64)}`),
      mediaType: "image/png" as const,
      bytes: 100,
      width: 1,
      height: 1,
    };
    source.append(
      "user/message",
      createUserMessage({
        source: { kind: "user" },
        content: [{ type: "image", attachment }],
      }),
      { surfaceOp: "append" },
    );
    source.append("image/offload", {
      targets: [{ seq: SessionSeq(1), imageIndexes: [0] }],
    });
    await withDownloadedTranscript(
      header.id,
      (async function* () {
        yield [header, ...source.snapshotEvents()];
      })(),
      () => {},
      async (remote) =>
        ctx.transcriptStore.replace(header.id, remote, () => {}),
    );
    const reader = await ctx.sessionPersistence.open(header.id, "read");
    const loaded = await reader.read();
    const restored = Session.fromRestore(
      header.id,
      loaded.events,
      reader.header,
      SessionLogOffset(0),
      loaded.eventState,
      [imageOffloadProjection],
    );
    const image = restored.deriveEventMessage(restored.snapshotEvents()[1]!)!
      .content[0];
    expect(image).toMatchObject({ type: "image", offloaded: true, attachment });
    await reader.close();
  } finally {
    await ctx.fiber.dispose();
  }
});
