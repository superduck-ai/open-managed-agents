import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { CompactionId } from "@deepseek-ai/dsh-compaction";
import { SessionId, type Session } from "@deepseek-ai/dsh-session";
import { mountKernel } from "../src/kernel.ts";
import { CcrCompaction } from "../src/ccrv2/compaction.ts";

function ended(session: Session, id: string) {
  return session.append("compaction/end", { compactionId: CompactionId(id), turn: null });
}

test("CCRv2 retains a newer compaction while the prior notification is publishing", async () => {
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    const session = ctx.sessions.create(SessionId("compaction-race"));
    const started = Promise.withResolvers<void>();
    const release = Promise.withResolvers<void>();
    const sequences: number[] = [];
    const tracker = new CcrCompaction(ctx, () => undefined, async (_id, _tokens, sequence) => {
      sequences.push(sequence);
      if (sequences.length === 1) {
        started.resolve();
        await release.promise;
      }
    });
    const first = ended(session, "first");
    tracker.record(session, first);
    const flushing = tracker.flush(session);
    await started.promise;
    const second = ended(session, "second");
    tracker.record(session, second);
    release.resolve();
    await flushing;
    await tracker.flush(session);
    await tracker.flush(session);
    expect(sequences).toEqual([first.seq, second.seq]);
  } finally { await ctx.fiber.dispose(); }
});

test("CCRv2 keeps root and child compaction queues independent", async () => {
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    const root = ctx.sessions.create(SessionId("root"));
    const child = ctx.sessions.create(SessionId("child"));
    const notifications: string[] = [];
    const tracker = new CcrCompaction(ctx, () => undefined, async id => { notifications.push(id); });
    tracker.record(root, ended(root, "root"));
    tracker.record(child, ended(child, "child"));
    await tracker.flush(child);
    expect(notifications).toEqual([child.id]);
    await tracker.flush(root);
    await tracker.flush(child);
    expect(notifications).toEqual([child.id, root.id]);
  } finally { await ctx.fiber.dispose(); }
});
