import { createHash } from "node:crypto";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import {
  expandAssistantStream,
  LlmAttemptId,
  type AssistantStreamRecord,
} from "@deepseek-ai/dsh-llm";
import type { AssistantMessage, ContentBlock } from "@deepseek-ai/dsh-llm";
import type { Payload } from "../json.ts";
export interface AssistantBlockOutput {
  type: "agent.thinking" | "agent.message";
  id: string;
  uuid: string;
  content?: {
    type: "text";
    text: string;
  }[];
  model_request_start_id?: string;
  session_thread_id?: string;
}
declare module "@deepseek-ai/dsh-session" {
  interface PluginRecordMap {
    "plugin:oma/assistant-block": {
      output: AssistantBlockOutput;
    };
  }
}
function assistantBlockOutput(
  id: string,
  block: ContentBlock,
  requestID?: string,
  threadId?: string,
): AssistantBlockOutput | undefined {
  if (block.type !== "text" && block.type !== "reasoning") return undefined;
  return {
    type: block.type === "reasoning" ? "agent.thinking" : "agent.message",
    id,
    uuid: id,
    ...(block.type === "text"
      ? { content: [{ type: "text", text: block.text }] }
      : {}),
    ...(requestID ? { model_request_start_id: requestID } : {}),
    ...(threadId ? { session_thread_id: threadId } : {}),
  };
}
interface MessageReplay {
  response?: {
    format?: string;
    messageID?: string;
    requestID?: string;
  };
  blocks?: {
    index?: number;
  }[];
}
function assistantEventID(
  codeSessionID: string,
  messageID: string | undefined,
  index: number,
  type: string,
  fallback: string,
): string {
  if (!messageID)
    return type === "agent.message" && index === 0
      ? fallback
      : `${fallback}_${type}_${index}`;
  const seed = `${codeSessionID}\0public\0assistant-preview-v1\0${messageID}\0${index}\0${type}`;
  return `sevt_${createHash("sha256").update(seed).digest("hex").slice(0, 32)}`;
}
export function assistantMessages(
  codeSessionID: string,
  fallback: string,
  message: AssistantMessage,
  threadId?: string,
): Payload[] {
  const replay = message.source.replayState as MessageReplay | undefined;
  const identity = ["oma-messages-v1", "oma-chat-completions-v1"].includes(
    replay?.response?.format ?? "",
  )
    ? replay
    : undefined;
  return message.content.flatMap((block, index): Payload[] => {
    if (block.type !== "text" && block.type !== "reasoning") return [];
    const type =
      block.type === "reasoning" ? "agent.thinking" : "agent.message";
    const id = assistantEventID(
      codeSessionID,
      identity?.response?.messageID,
      identity?.blocks?.[index]?.index ?? index,
      type,
      fallback,
    );
    const output = assistantBlockOutput(
      id,
      block,
      identity?.response?.requestID,
      threadId,
    );
    return output ? [output] : [];
  });
}
export interface AssistantAttemptIdentity {
  turn: number;
  step: number;
  messageID?: string;
  requestID?: string;
}
declare module "@deepseek-ai/dsh-session" {
  interface PluginRecordMap {
    "plugin:oma/assistant-identity": AssistantAttemptIdentity;
  }
}
export function interruptedAssistantMessages(
  codeSessionID: string,
  inputId: string,
  message: AssistantMessage,
  records: readonly AssistantStreamRecord[],
  identity: AssistantAttemptIdentity,
  threadId?: string,
): Payload[] {
  const stream = new AssistantStream(
    codeSessionID,
    () => identity.messageID,
    () => identity.requestID,
  );
  stream.record(
    {
      type: "start",
      attemptId: LlmAttemptId("replay"),
      revision: 1,
      turn: identity.turn,
      step: identity.step,
    },
    inputId,
    threadId,
  );
  const completed: Payload[] = [];
  for (const [index, timed] of expandAssistantStream(records).entries()) {
    for (const output of stream.record(
      {
        type: "chunk",
        attemptId: LlmAttemptId("replay"),
        revision: index + 2,
        index,
        ...timed,
      },
      inputId,
      threadId,
    ))
      if (
        (output.type === "agent.message" || output.type === "agent.thinking") &&
        !(
          timed.chunk.type === "block-end" &&
          (timed.chunk.block.type === "text" ||
            timed.chunk.block.type === "reasoning") &&
          timed.chunk.block.text.trim() === ""
        )
      )
        completed.push(output);
  }
  return [...completed, ...stream.interrupted(message, threadId)];
}
export class AssistantStream {
  private readonly streams = new Map<
    string,
    {
      turn: number;
      step: number;
      fallback: string;
      blocks: Map<number, string>;
      seen: Set<number>;
      prefixes: Map<
        number,
        { id: string; type: "text" | "reasoning"; text: string }
      >;
    }
  >();
  private readonly completed = new Map<string, Set<string>>();
  constructor(
    private readonly codeSessionID: string,
    private readonly messageID?: (sessionId: string) => string | undefined,
    private readonly requestID?: (sessionId: string) => string | undefined,
  ) {}
  complete(payloads: Payload[], threadId?: string): Payload[] {
    const key = threadId ?? this.codeSessionID;
    const completed = this.completed.get(key);
    this.completed.delete(key);
    return payloads.filter((payload) => !completed?.has(payload.id));
  }
  identity(threadId?: string): AssistantAttemptIdentity {
    const key = threadId ?? this.codeSessionID;
    const stream = this.streams.get(key);
    if (!stream) throw new Error("Assistant attempt identity is unavailable");
    return {
      turn: stream.turn,
      step: stream.step,
      messageID: this.messageID?.(key),
      requestID: this.requestID?.(key),
    };
  }
  interrupted(message: AssistantMessage, threadId?: string): Payload[] {
    const key = threadId ?? this.codeSessionID;
    const stream = this.streams.get(key);
    if (!stream) throw new Error("Interrupted assistant stream is unavailable");
    const prefixes = [...stream.prefixes.values()].filter(
      (prefix) => prefix.text.trim() !== "",
    );
    const blocks = message.content.filter(
      (block) => block.type === "text" || block.type === "reasoning",
    );
    if (prefixes.length !== blocks.length)
      throw new Error(
        "Interrupted assistant transcript differs from streamed prefix",
      );
    return blocks.flatMap((block, index) => {
      const prefix = prefixes[index]!;
      if (
        (block.type !== "text" && block.type !== "reasoning") ||
        block.type !== prefix.type ||
        block.text !== prefix.text
      )
        throw new Error(
          "Interrupted assistant transcript differs from streamed prefix",
        );
      if (this.completed.get(key)?.has(prefix.id)) return [];
      const output = assistantBlockOutput(
        prefix.id,
        block,
        this.requestID?.(key),
        threadId,
      );
      return output ? [output] : [];
    });
  }
  record(
    frame: AssistantStreamFrame,
    inputId: string,
    threadId?: string,
  ): Payload[] {
    const key = threadId ?? this.codeSessionID;
    if (frame.type === "start") {
      this.completed.set(key, new Set());
      this.streams.set(key, {
        turn: frame.turn,
        step: frame.step,
        fallback: `${inputId}_${threadId ? `${threadId}_` : ""}message_${frame.turn}_${frame.step}`,
        blocks: new Map(),
        seen: new Set(),
        prefixes: new Map(),
      });
      return [];
    }
    const stream = this.streams.get(key);
    if (!stream) return [];
    if (frame.type === "end") {
      this.streams.delete(key);
      return [];
    }
    if (frame.type !== "chunk") return [];
    const chunk = frame.chunk;
    if (
      chunk.type === "block-start" &&
      (chunk.blockType === "reasoning" || chunk.blockType === "text")
    ) {
      if (stream.seen.has(chunk.index)) return [];
      const type =
        chunk.blockType === "reasoning" ? "agent.thinking" : "agent.message";
      const id = assistantEventID(
        this.codeSessionID,
        this.messageID?.(key),
        chunk.index,
        type,
        stream.fallback,
      );
      stream.seen.add(chunk.index);
      stream.blocks.set(chunk.index, id);
      stream.prefixes.set(chunk.index, {
        id,
        type: chunk.blockType as "text" | "reasoning",
        text: "",
      });
      return [{ type: "event_start", event: { type, id } }];
    }
    if (chunk.type === "text-delta" || chunk.type === "reasoning-delta") {
      const prefix = stream.prefixes.get(chunk.index);
      if (prefix) prefix.text += chunk.text;
    }
    if (chunk.type === "block-end") {
      const type =
        chunk.block.type === "reasoning" ? "agent.thinking" : "agent.message";
      const id =
        stream.blocks.get(chunk.index) ??
        assistantEventID(
          this.codeSessionID,
          this.messageID?.(key),
          chunk.index,
          type,
          stream.fallback,
        );
      stream.blocks.delete(chunk.index);
      if (chunk.block.type === "text" || chunk.block.type === "reasoning")
        stream.prefixes.set(chunk.index, {
          id,
          type: chunk.block.type,
          text: chunk.block.text,
        });
      const output = assistantBlockOutput(
        id,
        chunk.block,
        this.requestID?.(key),
        threadId,
      );
      if (!output || this.completed.get(key)?.has(id)) return [];
      this.completed.get(key)?.add(id);
      return [output];
    }
    if (chunk.type !== "text-delta" || !chunk.text) return [];
    const type = "agent.message";
    const payloads: Payload[] = [];
    let id = stream.blocks.get(chunk.index);
    if (!id) {
      id = assistantEventID(
        this.codeSessionID,
        this.messageID?.(key),
        chunk.index,
        type,
        stream.fallback,
      );
      if (stream.seen.has(chunk.index)) return [];
      stream.seen.add(chunk.index);
      stream.blocks.set(chunk.index, id);
      stream.prefixes.set(chunk.index, { id, type: "text", text: chunk.text });
      payloads.push({ type: "event_start", event: { type, id } });
    }
    payloads.push({
      type: "event_delta",
      event_id: id,
      delta: {
        type: "content_delta",
        index: 0,
        content: { type: "text", text: chunk.text },
      },
    });
    return payloads;
  }
}
