import { pluginRecordOf, type SessionEvent } from "@deepseek-ai/dsh-session";
import { ACCOUNT_QUOTA_EXCEEDED_CODE, QUOTA_EXCEEDED_CODE } from "@deepseek-ai/dsh-llm";
import { isShutdownCancel } from "../runtime/contracts.ts";
import { assistantMessages, type AssistantBlockOutput } from "./assistant.ts";
import type { Payload } from "./protocol.ts";

export type InputOutcome = { kind: "success" } | { kind: "failed" | "recovery"; text: string };

export interface InputCompletion {
  version: 1;
  inputId: string;
  outcome: InputOutcome;
}

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/input-completion": InputCompletion;
  }
}

export function inputCompletionOf(event: SessionEvent): InputCompletion | undefined {
  const record = pluginRecordOf(event);
  if (record?.type !== "plugin:oma/input-completion") return;
  const data = record.data;
  if (!data || typeof data !== "object" || !("version" in data) || data.version !== 1
    || !("inputId" in data) || typeof data.inputId !== "string" || !data.inputId
    || !("outcome" in data) || !data.outcome || typeof data.outcome !== "object" || !("kind" in data.outcome))
    throw new Error("Invalid persisted input completion");
  const outcome = data.outcome;
  if (outcome.kind === "success") return { version: 1, inputId: data.inputId, outcome: { kind: "success" } };
  if ((outcome.kind !== "failed" && outcome.kind !== "recovery") || !("text" in outcome) || typeof outcome.text !== "string")
    throw new Error("Invalid persisted input completion outcome");
  return { version: 1, inputId: data.inputId, outcome: { kind: outcome.kind, text: outcome.text } };
}

export function completionOutput(inputId: string, outcome: InputOutcome): Payload[] {
  return outcome.kind === "success"
    ? [{ type: "result", uuid: `${inputId}_result`, subtype: "success", is_error: false }]
    : failedOutput(inputId, outcome.text, outcome.kind);
}

export function turnOutcome(reason: Extract<SessionEvent, { type: "turn/end" }>["data"]["reason"]): InputOutcome {
  if (reason.kind === "completed" || (reason.kind === "aborted" && reason.reason.kind === "user")) return { kind: "success" };
  const recovery = reason.kind === "interrupted";
  const quota = reason.kind === "error" && [QUOTA_EXCEEDED_CODE, ACCOUNT_QUOTA_EXCEEDED_CODE].includes(reason.error.code);
  const shutdown = reason.kind === "aborted" && isShutdownCancel(reason.reason);
  const text = shutdown ? "The Brain worker shut down during this turn. Unfinished tool calls were interrupted and their outcomes are unknown."
    : recovery ? "Previous turn was interrupted and was not replayed. See the recovered tool results before retrying operations."
    : quota ? "Model provider balance or quota is exhausted. Top up the provider account or switch model credentials."
    : "The turn stopped before successful completion.";
  return { kind: recovery ? "recovery" : "failed", text };
}


export function failedOutput(inputId: string, text: string, suffix = "failed"): Payload[] {
  const id = `${inputId}_${suffix}`;
  return [
    { type: "system.message", uuid: `${id}_message`, content: [{ type: "text", text }] },
    { type: "result", uuid: `${id}_result`, subtype: "error_during_execution", is_error: true },
  ];
}

export function sessionEventOutput(sessionId: string, inputId: string, event: SessionEvent, toolName?: string, threadId?: string): Payload[] {
  const record = pluginRecordOf(event);
  if (record) {
    if (record.type !== "plugin:oma/assistant-block") return [];
    const { output } = record.data as { output: AssistantBlockOutput };
    return [output];
  }
  const prefix = threadId ? `${sessionId}_${threadId}` : inputId;
  if (!prefix) return [];
  if (event.type === "assistant/message") {
    if (event.data.interrupted) return [];
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
  return [];
}
