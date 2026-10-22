import type { LanguageModelV4FinishReason, LanguageModelV4StreamPart, LanguageModelV4Usage, SharedV4ProviderMetadata } from "@ai-sdk/provider";
import { APICallError } from "@ai-sdk/provider";
import {
  LlmError, ToolCallId, isContextWindowExceededError, isQuotaExceededError,
  type ContentBlock, type FinishReason, type StreamChunk, type TokenUsage,
} from "@deepseek-ai/dsh-llm";

interface BlockState {
  index: number;
  block: ContentBlock;
  signature?: string;
  redactedData?: string;
}

export class AiSdkStream {
  messageID?: string;
  private readonly blocks = new Map<string, BlockState>();
  private usage?: TokenUsage;
  private finishReason?: LanguageModelV4FinishReason;

  constructor(private readonly replayFormat: string, private readonly requestID?: string) {}

  accept(part: LanguageModelV4StreamPart): StreamChunk[] {
    switch (part.type) {
      case "stream-start": case "raw": case "custom": case "text-end": case "tool-input-end": return [];
      case "response-metadata": this.messageID ??= part.id; return [];
      case "text-start": return this.start(part.id, { type: "text", text: "" });
      case "reasoning-start": return this.start(part.id, { type: "reasoning", text: "" }, part.providerMetadata);
      case "text-delta": return this.text(part.id, part.delta);
      case "reasoning-delta": return this.reasoning(part.id, part.delta, part.providerMetadata);
      case "reasoning-end": this.sign(part.id, part.providerMetadata); return [];
      case "tool-input-start": return this.start(part.id, { type: "tool-call", id: ToolCallId(part.id), name: part.toolName, arguments: "" });
      case "tool-input-delta": return this.toolInput(part.id, part.delta);
      case "tool-call": return this.toolCall(part.toolCallId, part.toolName, part.input);
      case "finish": this.usage = tokenUsage(part.usage); this.finishReason = part.finishReason; return [];
      case "error": throw modelError(part.error);
      default: throw new LlmError(`Unsupported model stream part: ${part.type}`, "TRANSPORT");
    }
  }

  finish(): StreamChunk[] {
    const states = [...this.blocks.values()].sort((left, right) => left.index - right.index);
    const reason = finishReason(this.finishReason, states.some((state) => state.block.type === "tool-call"));
    if (!reason) throw new LlmError("Model stream has incomplete terminal state", "TRANSPORT");
    if (!states.length) throw new LlmError("Model returned no content", "EMPTY_RESPONSE");
    return [
      ...states.map((state): StreamChunk => ({ type: "block-end", index: state.index, block: closed(state.block) })),
      ...(this.usage ? [{ type: "usage" as const, usage: this.usage }] : []),
      { type: "finish", reason, replayState: {
        response: { format: this.replayFormat, stopReason: this.finishReason?.raw,
          ...(this.messageID ? { messageID: this.messageID } : {}), ...(this.requestID ? { requestID: this.requestID } : {}) },
        blocks: states.map((state) => ({ index: state.index,
          ...(state.signature ? { signature: state.signature } : {}), ...(state.redactedData ? { redactedData: state.redactedData } : {}) })),
      } },
    ];
  }

  private start(id: string, block: ContentBlock, metadata?: SharedV4ProviderMetadata): StreamChunk[] {
    if (this.blocks.has(id)) return [];
    const state: BlockState = { index: this.blocks.size, block };
    this.blocks.set(id, state);
    this.sign(id, metadata);
    return [{ type: "block-start", index: state.index, blockType: block.type }];
  }

  private text(id: string, text: string): StreamChunk[] {
    const state = this.open(id, "text");
    if (state.block.type !== "text" || !text) return [];
    state.block.text += text;
    return [{ type: "text-delta", index: state.index, text }];
  }

  private reasoning(id: string, text: string, metadata?: SharedV4ProviderMetadata): StreamChunk[] {
    const state = this.open(id, "reasoning");
    this.sign(id, metadata);
    if (state.block.type !== "reasoning" || !text) return [];
    state.block.text += text;
    return [{ type: "reasoning-delta", index: state.index, text }];
  }

  private toolInput(id: string, delta: string): StreamChunk[] {
    const state = this.open(id, "tool-call");
    if (state.block.type !== "tool-call" || !delta) return [];
    state.block.arguments += delta;
    return [{ type: "tool-call-delta", index: state.index, id: state.block.id, name: state.block.name, argumentsDelta: delta }];
  }

  private toolCall(id: string, name: string, input: string): StreamChunk[] {
    const started = this.start(id, { type: "tool-call", id: ToolCallId(id), name, arguments: "" });
    const state = this.open(id, "tool-call");
    if (state.block.type !== "tool-call" || state.block.arguments) return started;
    return [...started, ...this.toolInput(id, input)];
  }

  private open(id: string, type: ContentBlock["type"]): BlockState {
    const state = this.blocks.get(id);
    if (!state || state.block.type !== type) throw new LlmError(`Model ${type} delta has no open block`, "TRANSPORT");
    return state;
  }

  private sign(id: string, metadata?: SharedV4ProviderMetadata): void {
    const anthropic = metadata?.anthropic as { signature?: unknown; redactedData?: unknown } | undefined;
    const state = this.blocks.get(id);
    if (!state || !anthropic) return;
    if (typeof anthropic.signature === "string") state.signature = (state.signature ?? "") + anthropic.signature;
    if (typeof anthropic.redactedData === "string") state.redactedData = anthropic.redactedData;
  }
}

export function modelError(error: unknown, signal?: AbortSignal): unknown {
  if (signal?.aborted || error instanceof LlmError) return error;
  if (APICallError.isInstance(error)) return billingError(error.responseBody, error.statusCode) ?? providerError(apiErrorMessage(error), error.statusCode);
  if (error instanceof Error) return providerError(error.message);
  const message = typeof error === "object" && error && "message" in error ? String(error.message) : String(error);
  return providerError(message);
}

function apiErrorMessage(error: APICallError): string {
  try {
    const message = JSON.parse(error.responseBody ?? "").error?.message;
    if (typeof message === "string" && message) return message;
  } catch {}
  return error.message || `Model API HTTP ${error.statusCode ?? "error"}`;
}

function providerError(detail: string, status?: number): LlmError {
  const message = detail || "Model stream error";
  const code = isContextWindowExceededError(message) ? "CONTEXT_WINDOW_EXCEEDED"
    : isQuotaExceededError(message) ? "QUOTA" : status === 429 ? "RATE_LIMIT"
    : status === 401 || status === 403 ? "AUTH" : status && status >= 500 ? "SERVER" : "TRANSPORT";
  return new LlmError(message, code, { status });
}

function finishReason(reason: LanguageModelV4FinishReason | undefined, hasTools: boolean): FinishReason | undefined {
  if (reason?.unified === "tool-calls" && hasTools) return { kind: "tool-calls" };
  if (reason?.unified === "stop" && !hasTools) return { kind: "stop" };
  if (reason?.unified === "length") return { kind: "max-tokens" };
  return undefined;
}

function closed(block: ContentBlock): ContentBlock {
  return block.type === "tool-call" && !block.arguments ? { ...block, arguments: "{}" } : block;
}

function tokenUsage(usage: LanguageModelV4Usage): TokenUsage | undefined {
  const input = usage.inputTokens;
  if (input.total === undefined && usage.outputTokens.total === undefined) return undefined;
  return { inputTokens: input.noCache ?? input.total ?? 0, outputTokens: usage.outputTokens.total ?? 0,
    ...(input.cacheRead !== undefined ? { cacheReadTokens: input.cacheRead } : {}),
    ...(input.cacheWrite !== undefined ? { cacheWriteTokens: input.cacheWrite } : {}) };
}

export function billingError(body: string | undefined, status?: number): LlmError | undefined {
  try {
    const error = JSON.parse(body ?? "").error;
    if (error?.code === "session_budget_reached" || error?.code === "model_price_unavailable")
      return new LlmError(typeof error.message === "string" ? error.message : error.code, error.code.toUpperCase(), { status });
  } catch {}
  return undefined;
}
