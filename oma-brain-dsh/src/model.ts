import {
  LlmAdapter, LlmError, ToolCallId, isContextWindowExceededError, isQuotaExceededError,
  type GenerateOptions, type StreamChunk, type LlmResolvedModelInfo, type ContentBlock, type TokenUsage,
} from "@deepseek-ai/dsh-llm";
import { OmaApi } from "./api.ts";
import { object, type Payload } from "./json.ts";
import { sse } from "./sse.ts";
import { workerDiagnostic } from "./ccrv2/diagnostics.ts";
import { logSystemPrompt } from "./prompt/debug.ts";

export class MessagesAdapter extends LlmAdapter {
  constructor(readonly api: OmaApi) { super(); }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    const debugStart = Date.now();
    options.signal?.throwIfAborted();
    if (!options.sessionId) throw new Error("Model request has no session identity");
    const assignment = this.api.assignment(this.api.modelSessions.get(options.sessionId) ?? options.sessionId);
    this.api.modelResponses.delete(options.sessionId);
    if (process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_model_started", {
      session_id: options.sessionId, timestamp: new Date().toISOString(), tools: options.tools?.length ?? 0,
    });
    const request = messagesRequest(options);
    logSystemPrompt(options.sessionId, request);
    const response = await this.api.requestResponse("/v1/messages", assignment.model_token, "POST", request,
      { "anthropic-version": "2023-06-01", Accept: "text/event-stream",
        ...(options.sessionId !== assignment.session_id ? { "X-Claude-Code-Agent-Id": options.sessionId } : {}),
      }, options.signal, 300_000);
    if (!response.ok) {
      let detail = `Messages API HTTP ${response.status}`;
      try { detail = (await response.json()).error?.message ?? detail; } catch { /* No JSON error body. */ }
      throw providerError(detail, response.status);
    }
    if (!response.body) throw new LlmError("Messages API response has no stream", "TRANSPORT");
    const blocks = new Map<number, { block: ContentBlock; signature: string; closed: boolean }>();
    const usage: TokenUsage = { inputTokens: 0, outputTokens: 0 };
    let stopReason: string | undefined;
    let started = false;
    let messageID: string | undefined;
    for await (const frame of sse(response.body, options.signal)) {
      if (!frame.data) continue;
      const event = object(JSON.parse(frame.data));
      if (event.type === "error") throw providerError(event.error?.message ?? "Messages API stream error");
      if (event.type === "message_start") {
        started = true;
        messageID = typeof event.message?.id === "string" ? event.message.id : undefined;
        if (messageID) this.api.modelResponses.set(options.sessionId, messageID);
        updateUsage(usage, event.message?.usage);
      }
      else if (event.type === "message_delta") {
        updateUsage(usage, event.usage);
        stopReason = event.delta?.stop_reason ?? stopReason;
      } else if (event.type === "content_block_start") {
        if (!Number.isSafeInteger(event.index) || event.index < 0 || blocks.has(event.index))
          throw new LlmError("Invalid Messages block index", "TRANSPORT");
        const native = object(event.content_block);
        let block: ContentBlock;
        if (native.type === "text") block = { type: "text", text: native.text ?? "" };
        else if (native.type === "thinking") block = { type: "reasoning", text: native.thinking ?? "" };
        else if (native.type === "tool_use") block = {
          type: "tool-call", id: ToolCallId(native.id), name: native.name,
          arguments: native.input && Object.keys(native.input).length ? JSON.stringify(native.input) : "",
        };
        else throw new LlmError(`Unsupported Messages block type: ${native.type}`, "TRANSPORT");
        blocks.set(event.index, { block, signature: native.signature ?? "", closed: false });
        yield { type: "block-start", index: event.index, blockType: block.type };
        if (block.type === "text" && block.text) yield { type: "text-delta", index: event.index, text: block.text };
        if (block.type === "reasoning" && block.text) yield { type: "reasoning-delta", index: event.index, text: block.text };
      } else if (event.type === "content_block_delta") {
        const entry = blocks.get(event.index);
        if (!entry || entry.closed) throw new LlmError("Messages delta has no open block", "TRANSPORT");
        const delta = object(event.delta);
        if (delta.type === "text_delta" && entry.block.type === "text") {
          entry.block.text += delta.text;
          yield { type: "text-delta", index: event.index, text: delta.text };
        } else if (delta.type === "thinking_delta" && entry.block.type === "reasoning") {
          entry.block.text += delta.thinking;
          yield { type: "reasoning-delta", index: event.index, text: delta.thinking };
        } else if (delta.type === "signature_delta" && entry.block.type === "reasoning") entry.signature += delta.signature;
        else if (delta.type === "input_json_delta" && entry.block.type === "tool-call") {
          entry.block.arguments += delta.partial_json;
          yield { type: "tool-call-delta", index: event.index, id: entry.block.id, name: entry.block.name, argumentsDelta: delta.partial_json };
        } else throw new LlmError("Messages delta does not match block type", "TRANSPORT");
      } else if (event.type === "content_block_stop") {
        const entry = blocks.get(event.index);
        if (!entry || entry.closed) throw new LlmError("Messages stop has no open block", "TRANSPORT");
        entry.closed = true;
        if (entry.block.type === "tool-call" && !entry.block.arguments) entry.block.arguments = "{}";
        yield { type: "block-end", index: event.index, block: entry.block };
      } else if (event.type === "message_stop") {
        if (process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_model_finished", {
          session_id: options.sessionId, timestamp: new Date().toISOString(), duration_ms: Date.now() - debugStart,
          stop_reason: stopReason, block_types: [...blocks.values()].map(entry => entry.block.type),
        });
        if (!started || !stopReason || [...blocks.values()].some((entry) => !entry.closed))
          throw new LlmError("Messages stream has incomplete terminal state", "TRANSPORT");
        if (!blocks.size) throw new LlmError("Messages API returned no content", "EMPTY_RESPONSE");
        yield { type: "usage", usage };
        yield {
          type: "finish", reason: stopReason === "tool_use" ? { kind: "tool-calls" }
            : stopReason === "max_tokens" ? { kind: "max-tokens" } : { kind: "stop" },
          replayState: { response: { format: "oma-messages-v1", stopReason,
            ...(messageID ? { messageID } : {}),
            ...(response.headers.has("Request-Id") ? { requestID: response.headers.get("Request-Id")! } : {}),
          },
            blocks: [...blocks.entries()].map(([index, entry]) => ({ signature: entry.signature, index })) },
        };
        return;
      }
    }
    throw new LlmError("Messages stream ended without terminal marker message_stop", "TRANSPORT");
  }
}

function updateUsage(usage: TokenUsage, native?: Payload): void {
  if (!native) return;
  if (typeof native.input_tokens === "number") usage.inputTokens = native.input_tokens;
  if (typeof native.output_tokens === "number") usage.outputTokens = native.output_tokens;
  if (typeof native.cache_read_input_tokens === "number") usage.cacheReadTokens = native.cache_read_input_tokens;
  if (typeof native.cache_creation_input_tokens === "number") usage.cacheWriteTokens = native.cache_creation_input_tokens;
}

function providerError(message: string, status?: number): LlmError {
  const code = isContextWindowExceededError(message) ? "CONTEXT_WINDOW_EXCEEDED"
    : isQuotaExceededError(message) ? "QUOTA" : status === 429 ? "RATE_LIMIT"
    : status === 401 || status === 403 ? "AUTH" : status && status >= 500 ? "SERVER" : "TRANSPORT";
  return new LlmError(message, code, { status });
}

function messagesRequest(options: GenerateOptions): Payload {
  const system: string[] = options.system ? [options.system] : [];
  const messages: { role: string; content: Payload[] }[] = [];
  for (const message of options.messages) {
    if (message.role === "system") {
      system.push(...message.content.filter((block) => block.type === "text").map((block) => block.text));
      continue;
    }
    const replay = message.role === "assistant" ? message.source.replayState as Payload | undefined : undefined;
    const content = message.content.flatMap((block, index): Payload[] => {
      if (block.type === "text") return [{ type: "text", text: block.text }];
      if (block.type === "reasoning") {
        const signature = replay?.response?.format === "oma-messages-v1" ? replay.blocks?.[index]?.signature
          : replay?.response?.kind === "pi-ai" ? replay.blocks?.[index]?.thinkingSignature : undefined;
        return typeof signature === "string" && signature
          ? [{ type: "thinking", thinking: block.text, signature }] : [{ type: "text", text: block.text }];
      }
      if (block.type === "tool-call") return [{ type: "tool_use", id: block.id, name: block.name, input: replayToolInput(block.arguments) }];
      if (block.type === "message-media" && message.role === "user") return [structuredClone(block.block)];
      throw new LlmError(`Unsupported Messages input block: ${block.type}`, "INVALID_ARGS");
    });
    if (message.role === "tool") {
      const toolResult = { type: "tool_result", tool_use_id: message.source.callId, content, is_error: message.isError ?? false };
      appendMessage(messages, "user", [toolResult]);
    } else appendMessage(messages, message.role === "assistant" ? "assistant" : "user", content);
  }
  return {
    model: options.model, max_tokens: options.maxTokens ?? 8192, stream: true, messages,
    ...(system.length ? { system: system.join("\n\n") } : {}),
    ...(options.tools?.length ? { tools: options.tools.map((tool) => ({ name: tool.name, description: tool.description,
      input_schema: tool.parameters, ...(tool.deferLoading ? { defer_loading: true } : {}) })) } : {}),
    ...(options.temperature !== undefined ? { temperature: options.temperature } : {}),
    ...(options.stop?.length ? { stop_sequences: options.stop } : {}),
  };
}

function appendMessage(messages: { role: string; content: Payload[] }[], role: string, content: Payload[]): void {
  if (!content.length) return;
  const last = messages.at(-1);
  if (last?.role === role) last.content.push(...content);
  else messages.push({ role, content });
}

function replayToolInput(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return { __unparsedToolInput: { raw, len: raw.length } };
  }
}
