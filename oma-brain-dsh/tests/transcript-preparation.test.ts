import { Context } from "@deepseek-ai/cordis";
import { Session, SessionId, SessionLogOffset } from "@deepseek-ai/dsh-session";
import { expect, spyOn, test } from "bun:test";
import { mountKernel } from "../src/kernel.ts";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { withDownloadedTranscript } from "../src/runtime/transcript-download.ts";

async function fixture() {
  const ctx = new Context();
  await mountKernel(ctx);
  await ctx.plugin(TranscriptPersistence);
  ctx.transcriptStore.connect({ changed: () => {}, flush: async () => {} });
  const source = Session.create(SessionId("prepared-transcript"));
  source.append("turn/start", { turn: 1 });
  source.append("turn/end", { turn: 1, reason: { kind: "completed" } });
  return {
    ctx,
    source,
    async download() {
      await withDownloadedTranscript(
        source.id,
        (async function* () {
          yield [source.header, ...source.snapshotEvents()];
        })(),
        () => {},
        async (remote) => {
          ctx.transcriptStore.replace(source.id, remote, () => {});
        },
      );
    },
  };
}

for (const change of [
  "events",
  "header",
  "projections",
  "live-session",
] as const) {
  test(`prepared history is not reused after changing ${change}`, async () => {
    const { ctx, source, download } = await fixture();
    const restored = spyOn(Session, "fromRestore");
    try {
      await download();
      const reader = await ctx.sessionPersistence.open(source.id, "read");
      const { events } = await reader.read();
      await reader.close();
      const options = {
        seed: [...events],
        meta: structuredClone(source.header),
        inheritedEventCount: SessionLogOffset(0),
        eventState: "shared-frozen" as const,
      };
      if (change === "events") {
        const writer = await ctx.sessionPersistence.open(source.id, "write");
        source.append("turn/start", { turn: 2 });
        await writer.append(
          source.snapshotEvents(SessionLogOffset(events.length)),
        );
        options.seed = [...(await writer.read()).events];
        await writer.close();
      }
      if (change === "header")
        options.meta = { ...options.meta, createdAt: 42 };
      if (change === "projections")
        ctx.sessions.registerMessageProjection({
          type: "turn/end",
          project: () => new Map(),
        });
      if (change === "live-session") {
        ctx.sessions.create(source.id, {
          seed: source.snapshotEvents(),
          meta: source.header,
        });
        expect(() => ctx.sessions.prepare(source.id, options)).toThrow(
          "already exists",
        );
        expect(restored).toHaveBeenCalledTimes(1);
      } else {
        ctx.sessions.prepare(source.id, options);
        expect(restored).toHaveBeenCalledTimes(2);
      }
    } finally {
      restored.mockRestore();
      await ctx.fiber.dispose();
    }
  });
}

test("native agents.resume reuses the validated Session exactly once", async () => {
  const { ctx, source, download } = await fixture();
  const restored = spyOn(Session, "fromRestore");
  try {
    await download();
    expect(restored).toHaveBeenCalledTimes(1);
    const prepared = restored.mock.results[0]!.value as Session;
    expect(ctx.sessions.get(source.id)).toBeUndefined();
    const handle = await ctx.agents.resume({ resumeSessionId: source.id });
    expect(handle.agent.session).toBe(prepared);
    expect(restored).toHaveBeenCalledTimes(1);
    await handle.dispose();
    const retry = await ctx.agents.resume({ resumeSessionId: source.id });
    expect(retry.agent.session).not.toBe(prepared);
    expect(restored).toHaveBeenCalledTimes(2);
    await retry.dispose();
  } finally {
    restored.mockRestore();
    await ctx.fiber.dispose();
  }
});

test("native interrupted-turn repair rebuilds the prepared history", async () => {
  const { ctx, source, download } = await fixture();
  source.append("turn/start", { turn: 2 });
  const restored = spyOn(Session, "fromRestore");
  try {
    await download();
    const handle = await ctx.agents.resume({ resumeSessionId: source.id });
    expect(restored).toHaveBeenCalledTimes(2);
    expect(
      handle.agent.session
        .snapshotEvents()
        .find((event) => event.type === "turn/end" && event.data.turn === 2),
    ).toMatchObject({ type: "turn/end", data: { turn: 2 } });
    await handle.dispose();
  } finally {
    restored.mockRestore();
    await ctx.fiber.dispose();
  }
});

test("failed agent setup releases ownership and never reuses its mutated Session", async () => {
  const { ctx, source, download } = await fixture();
  const restored = spyOn(Session, "fromRestore");
  try {
    await download();
    await expect(
      ctx.agents.resume({
        resumeSessionId: source.id,
        setup: () => {
          throw new Error("setup failed");
        },
      }),
    ).rejects.toThrow("setup failed");
    expect(ctx.sessions.get(source.id)).toBeUndefined();
    const retry = await ctx.agents.resume({ resumeSessionId: source.id });
    expect(restored).toHaveBeenCalledTimes(2);
    await retry.dispose();
  } finally {
    restored.mockRestore();
    await ctx.fiber.dispose();
  }
});
