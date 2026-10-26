import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { ACCOUNT_QUOTA_EXCEEDED_CODE, QUOTA_EXCEEDED_CODE } from "@deepseek-ai/dsh-llm";
import { isShutdownCancel } from "../runtime/contracts.ts";
import { assistantMessages } from "./assistant.ts";
import type { Payload } from "./protocol.ts";

export function terminalOutput(inputId: string, reason: Extract<SessionEvent, { type: "turn/end" }>["data"]["reason"]): Payload[] {
  if (reason.kind === "completed" || (reason.kind === "aborted" && reason.reason.kind === "user"))
    return [{ type: "result", uuid: `${inputId}_result`, subtype: "success", is_error: false }];
  const recovery = reason.kind === "interrupted";
  const quota = reason.kind === "error" && [QUOTA_EXCEEDED_CODE, ACCOUNT_QUOTA_EXCEEDED_CODE].includes(reason.error.code);
  const shutdown = reason.kind === "aborted" && isShutdownCancel(reason.reason);
  const text = shutdown ? "The Brain worker shut down during this turn. Unfinished tool calls were interrupted and their outcomes are unknown."
    : recovery ? "Previous turn was interrupted and was not replayed. See the recovered tool results before retrying operations."
    : quota ? "Model provider balance or quota is exhausted. Top up the provider account or switch model credentials."
    : "The turn stopped before successful completion.";
  return failedOutput(inputId, text, recovery ? "recovery" : "failed");
}

export function failedOutput(inputId: string, text: string, suffix = "failed"): Payload[] {
  const id = `${inputId}_${suffix}`;
  return [
    { type: "system.message", uuid: `${id}_message`, content: [{ type: "text", text }] },
    { type: "result", uuid: `${id}_result`, subtype: "error_during_execution", is_error: true },
  ];
}

export function nativeOutput(sessionId: string, inputId: string, event: SessionEvent, toolName?: string, threadId?: string): Payload[] {
  const prefix = threadId ? `${sessionId}_${threadId}` : inputId;
  if (!prefix) return [];
  if (event.type === "assistant/message") {
    const uuid = `${prefix}_message_${event.data.turn}_${event.data.step}`;
    return assistantMessages(sessionId, uuid, event.data.message, threadId);
  }
  if (event.type === "tool/result" && event.surfaceOp === "append") return [{
    type: "user", uuid: `${prefix}_dsh_${event.seq}`,
    ...(threadId ? { session_thread_id: threadId } : {}),
    message: { role: "user", content: [{
      type: "tool_result", tool_use_id: event.data.message.toolCallId,
      ...(toolName ? { tool_name: toolName } : {}), is_error: !!event.data.message.isError,
      content: event.data.message.content,
    }] },
  }];
  if (!threadId && event.type === "turn/end") return terminalOutput(inputId, event.data.reason);
  return [];
}
