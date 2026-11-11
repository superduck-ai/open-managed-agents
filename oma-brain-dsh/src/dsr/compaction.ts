import { createHash } from "node:crypto";
import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";
import type { PublicEvent } from "./wire.ts";

export function projectCompaction(
  event: SessionEvent,
  session: Session,
  root: string,
): PublicEvent | undefined {
  const replacedTool =
    event.type === "tool/result" &&
    typeof event.surfaceOp === "object" &&
    event.surfaceOp.op === "replace";
  if (event.type !== "compaction/end" && !replacedTool) return;
  let source: SessionEvent | undefined;
  const history = session.snapshotEvents().slice(0, event.seq);
  if (event.type === "compaction/end" && event.data.error === undefined) {
    const start = history.findLast(
      (entry) =>
        entry.type === "compaction/start" &&
        entry.data.compactionId === event.data.compactionId,
    );
    if (
      start &&
      !history.some(
        (entry) =>
          entry.seq > start.seq &&
          entry.type === "compaction/end" &&
          entry.data.compactionId === event.data.compactionId,
      ) &&
      history.some(
        (entry) =>
          entry.seq > start.seq &&
          entry.type === "compaction/summary" &&
          entry.data.compactionId === event.data.compactionId,
      )
    )
      source = event;
  }
  if (
    event.type === "tool/result" &&
    typeof event.surfaceOp === "object" &&
    event.surfaceOp.op === "replace"
  ) {
    const previous = history.at(-1);
    if (
      previous?.type === "compaction/prune" &&
      previous.data.shadowedSeqs.length > 0 &&
      previous.data.shadowedSeqs.length === event.sourceEventSeqs?.length &&
      previous.data.shadowedSeqs.every(
        (seq, index) => seq === event.sourceEventSeqs?.[index],
      )
    )
      source = previous;
  }
  if (!source) return;
  const id = createHash("sha256")
    .update(JSON.stringify([root, session.id, "context_compacted", source.seq]))
    .digest("hex")
    .slice(0, 32);
  return {
    id: `sevt_${id}`,
    type: "agent.thread_context_compacted",
    ...(session.id === root ? {} : { session_thread_id: session.id }),
  };
}
