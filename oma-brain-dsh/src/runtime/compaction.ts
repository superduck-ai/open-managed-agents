import type { LlmFailure } from "@deepseek-ai/dsh-llm";
import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";

export function compactReadHistory(session: Session): number {
  const reads: SessionEvent<"tool/result">[] = [];
  const names = new Map<string, string>();
  for (const node of session.surface.nodes) {
    const event = session.eventAt(node);
    if (event?.type === "assistant/message") {
      for (const block of event.data.message.content)
        if (block.type === "tool-call") names.set(block.id, block.name);
    } else if (event?.type === "tool/result" && names.get(event.data.message.toolCallId) === "Read"
      && !event.data.message.isError && event.surfaceOp === "append"
      && event.data.message.content.every(block => block.type === "text")) {
      reads.push(event);
    }
  }
  let reduced = 0;
  for (const event of reads.slice(0, -2)) {
    const text = event.data.message.content.map(block => block.type === "text" ? block.text : "").join("\n");
    if (text.length < 16384) continue;
    const prefix = text.slice(0, 1024).replace(/[\uD800-\uDBFF]$/u, "");
    const suffix = text.slice(-1024).replace(/^[\uDC00-\uDFFF]/u, "");
    const summary = `[Earlier Read output compacted: ${text.length} characters. The original result remains in history. Read the original file again if omitted details are needed.]\nFirst excerpt:\n${prefix}\nLast excerpt:\n${suffix}`;
    session.append("tool/result", {
      ...event.data,
      message: { ...event.data.message, content: [{ type: "text", text: summary }] },
    }, { surfaceOp: { op: "replace", startSeq: event.seq, endSeq: event.seq }, sourceEventSeqs: [event.seq] });
    reduced += text.length - summary.length;
  }
  return reduced;
}

export class ContextRecovery {
  private lastAttempt = "";

  async recover(session: Session, failure: LlmFailure, turn: number, step: number,
    persist: () => Promise<unknown>, publish: (tokens?: number) => Promise<void>): Promise<boolean> {
    if (failure.code !== "CONTEXT_WINDOW_EXCEEDED") return false;
    const attempt = `${turn}:${step}`;
    if (this.lastAttempt === attempt) return false;
    this.lastAttempt = attempt;
    if (!compactReadHistory(session)) return false;
    await persist();
    const match = failure.message.match(/\((\d+) in the messages,/);
    const tokens = match ? Number(match[1]) : undefined;
    await publish(tokens !== undefined && Number.isSafeInteger(tokens) ? tokens : undefined);
    return true;
  }
}
