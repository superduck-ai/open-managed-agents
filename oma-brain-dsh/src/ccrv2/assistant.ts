import { createHash } from "node:crypto";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { AssistantMessage } from "@deepseek-ai/dsh-llm";
import type { Payload } from "./protocol.ts";

interface MessageReplay {
  response?: { format?: string; messageID?: string; requestID?: string };
  blocks?: { index?: number }[];
}

function assistantEventID(codeSessionID: string, messageID: string | undefined, index: number, type: string, fallback: string): string {
  if (!messageID) return type === "agent.message" && index === 0 ? fallback : `${fallback}_${type}_${index}`;
  const seed = `${codeSessionID}\0public\0assistant-preview-v1\0${messageID}\0${index}\0${type}`;
  return `sevt_${createHash("sha256").update(seed).digest("hex").slice(0, 32)}`;
}

export function assistantMessages(codeSessionID: string, fallback: string, message: AssistantMessage, threadId?: string): Payload[] {
  const replay = message.source.replayState as MessageReplay | undefined;
  const identity = replay?.response?.format === "oma-messages-v1" ? replay : undefined;
  return message.content.flatMap((block, index): Payload[] => {
    if (block.type !== "text" && block.type !== "reasoning") return [];
    const type = block.type === "reasoning" ? "agent.thinking" : "agent.message";
    const id = assistantEventID(codeSessionID, identity?.response?.messageID, identity?.blocks?.[index]?.index ?? index, type, fallback);
    return [{ type, id, uuid: id,
      ...(block.type === "text" ? { content: [{ type: "text", text: block.text }] } : {}),
      ...(identity?.response?.requestID ? { model_request_start_id: identity.response.requestID } : {}),
      ...(threadId ? { session_thread_id: threadId } : {}),
    }];
  });
}

export class CcrAssistantStream {
  private readonly streams = new Map<string, { fallback: string; blocks: Map<number, string>; seen: Set<number> }>();

  constructor(private readonly codeSessionID: string, private readonly messageID?: (sessionId: string) => string | undefined) {}

  record(frame: AssistantStreamFrame, inputId: string, threadId?: string): Payload[] {
    const key = threadId ?? this.codeSessionID;
    if (frame.type === "start") {
      this.streams.set(key, { fallback: `${inputId}_${threadId ? `${threadId}_` : ""}message_${frame.turn}_${frame.step}`, blocks: new Map(), seen: new Set() });
      return [];
    }
    const stream = this.streams.get(key);
    if (!stream) return [];
    if (frame.type === "end") {
      this.streams.delete(key);
      return [...stream.blocks.values()].map((id) => ({ type: "event_stop", event_id: id }));
    }
    if (frame.type !== "chunk") return [];
    const chunk = frame.chunk;
    if (chunk.type === "block-start" && chunk.blockType === "reasoning") {
      if (stream.seen.has(chunk.index)) return [];
      const id = assistantEventID(this.codeSessionID, this.messageID?.(key), chunk.index, "agent.thinking", stream.fallback);
      stream.seen.add(chunk.index);
      stream.blocks.set(chunk.index, id);
      return [{ type: "event_start", event: { type: "agent.thinking", id } }];
    }
    if (chunk.type === "block-end") {
      const id = stream.blocks.get(chunk.index);
      stream.blocks.delete(chunk.index);
      return id ? [{ type: "event_stop", event_id: id }] : [];
    }
    if (chunk.type !== "text-delta" || !chunk.text) return [];
    const type = "agent.message";
    const payloads: Payload[] = [];
    let id = stream.blocks.get(chunk.index);
    if (!id) {
      id = assistantEventID(this.codeSessionID, this.messageID?.(key), chunk.index, type, stream.fallback);
      if (stream.seen.has(chunk.index)) return [];
      stream.seen.add(chunk.index);
      stream.blocks.set(chunk.index, id);
      payloads.push({ type: "event_start", event: { type, id } });
    }
    payloads.push({ type: "event_delta", event_id: id, delta: {
      type: "content_delta", content: { type: "text", text: chunk.text },
    } });
    return payloads;
  }
}
