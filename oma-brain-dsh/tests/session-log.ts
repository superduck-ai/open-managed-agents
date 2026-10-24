import { SessionId } from "@deepseek-ai/dsh-session";
import type { CcrSession } from "../src/ccrv2/session.ts";

export function nativeOutcomes(session: CcrSession, threadId = session.id): Record<string, { text: string; failed: boolean }> {
  const outcomes: Record<string, { text: string; failed: boolean }> = {};
  for (const event of session.ctx.sessions.get(SessionId(threadId))?.snapshotEvents() ?? []) {
    if (event.type !== "tool/result" || event.surfaceOp !== "append") continue;
    outcomes[event.data.message.toolCallId] = {
      text: event.data.message.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"),
      failed: !!event.data.message.isError,
    };
  }
  return outcomes;
}

export async function seedInterruptedTool(session: CcrSession, inputId: string, callId = "uncertain") {
  const { createMessage, ToolCallId } = await import("@deepseek-ai/dsh-llm");
  session.delivery.inputId = inputId;
  session.delivery.logOffsets = { [session.id]: session.agent.session.snapshotEvents().length };
  await session.saveDelivery();
  session.agent.session.append("turn/start", { turn: 1 });
  session.agent.session.append("step/start", { turn: 1, step: 1 });
  session.agent.session.append("assistant/message", {
    turn: 1, step: 1, stream: [],
    message: createMessage({ role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
      content: [{ type: "tool-call", id: ToolCallId(callId), name: "Bash", arguments: '{"command":"write"}' }] }),
  }, { surfaceOp: "append" });
  session.agent.session.append("tool/call", { turn: 1, step: 1, callId: ToolCallId(callId), name: "Bash", arguments: '{"command":"write"}' });
  await session.runtime.flushLog();
}
