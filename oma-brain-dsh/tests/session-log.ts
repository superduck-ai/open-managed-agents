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
  session.inputId = inputId;
  session.agent.session.append("turn/start", { turn: 1 });
  session.agent.session.append("step/start", { turn: 1, step: 1 });
  session.agent.session.append("system/message", {
    turn: 1, step: 1, message: createMessage({ role: "system", source: { kind: "system-prompt" }, content: [{ type: "text", text: session.config.systemPrompt || "Test" }] }),
  }, { surfaceOp: "append" });
  const { freezeMessage, MessageId } = await import("@deepseek-ai/dsh-llm");
  session.agent.session.append("user/message", freezeMessage({ id: MessageId(inputId), role: "user", source: { kind: "user" }, content: [{ type: "text", text: "Interrupted input" }] }), { surfaceOp: "append" });
  session.agent.session.append("assistant/message", {
    turn: 1, step: 1, stream: [],
    message: createMessage({ role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
      content: [{ type: "tool-call", id: ToolCallId(callId), name: "Bash", arguments: '{"command":"write"}' }] }),
  }, { surfaceOp: "append" });
  session.agent.session.append("tool/call", { turn: 1, step: 1, callId: ToolCallId(callId), name: "Bash", arguments: '{"command":"write"}' });
  await session.runtime.flushLog();
}


export function completedInputs(session: CcrSession): string[] {
  const completed = new Set<string>();
  let id: string | undefined;
  let inputSeen = false;
  for (const event of session.agent.session.snapshotEvents()) {
    if (event.type === "turn/start") inputSeen = false;
    if (!inputSeen && event.type === "user/message" && event.data.source.kind === "user") { id = event.data.id; inputSeen = true; }
    if (event.type === "turn/end" && id) completed.add(id);
  }
  return [...completed];
}
