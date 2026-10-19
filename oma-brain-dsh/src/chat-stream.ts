import { LlmError, ToolCallId, type ContentBlock, type StreamChunk, type TokenUsage } from "@deepseek-ai/dsh-llm";

export interface ChatChunk {
  id?: string;
  error?: { message?: string };
  choices?: {
    index: number;
    delta: {
      content?: string | null;
      refusal?: string | null;
      reasoning_content?: string | null;
      tool_calls?: { index: number; id?: string; type?: string; function?: { name?: string; arguments?: string } }[];
    };
    finish_reason?: string | null;
  }[];
  usage?: { prompt_tokens: number; completion_tokens: number; prompt_tokens_details?: { cached_tokens?: number } } | null;
}

type ToolDelta = NonNullable<ChatChunk["choices"]>[number]["delta"]["tool_calls"];

interface BlockState {
  block: ContentBlock;
  started: boolean;
  emittedArguments: number;
}

export class ChatStream {
  readonly blocks = new Map<number, BlockState>();
  messageID?: string;
  private stopReason?: string;
  private usage?: TokenUsage;
  private contentBytes = 0;
  private readonly startOrder: number[] = [];

  accept(chunk: ChatChunk): StreamChunk[] {
    if (chunk.choices && chunk.choices.length > 1) throw new LlmError("Multiple Chat choices are unsupported", "TRANSPORT");
    if (chunk.id) {
      if (this.messageID && this.messageID !== chunk.id) throw new LlmError("Chat response identity changed", "TRANSPORT");
      this.messageID = chunk.id;
    }
    if (chunk.usage) this.setUsage(chunk.usage);
    const output: StreamChunk[] = [];
    for (const choice of chunk.choices ?? []) {
      if (choice.index !== 0 || this.stopReason || !this.messageID)
        throw new LlmError("Invalid Chat Completions choice", "TRANSPORT");
      if (choice.delta.refusal || choice.delta.reasoning_content)
        throw new LlmError("Chat refusal or reasoning extension is unsupported", "TRANSPORT");
      if (choice.delta.content) output.push(...this.text(choice.delta.content));
      output.push(...this.tools(choice.delta.tool_calls));
      if (choice.finish_reason) this.stopReason = choice.finish_reason;
    }
    if (this.contentBytes > 4 * 1024 * 1024) throw new LlmError("Chat response exceeds content limit", "TRANSPORT");
    return output;
  }

  finish(requestID: string | null): StreamChunk[] {
    const entries = [...this.blocks.entries()].sort(([left], [right]) => left - right);
    const hasTools = entries.some(([, state]) => state.block.type === "tool-call");
    const validReason = this.stopReason === "tool_calls" ? hasTools
      : (this.stopReason === "stop" || this.stopReason === "length") && !hasTools;
    if (!this.messageID || !validReason) throw new LlmError("Chat stream has incomplete terminal state", "TRANSPORT");
    if (!entries.length) throw new LlmError("Chat Completions returned no content", "EMPTY_RESPONSE");
    const output: StreamChunk[] = [];
    for (const [index, state] of entries) {
      if (state.block.type === "tool-call") {
        if (!state.started) throw new LlmError("Chat tool call has no identity", "TRANSPORT");
        state.block.arguments ||= "{}";
        try {
          const input = JSON.parse(state.block.arguments);
          if (!input || typeof input !== "object" || Array.isArray(input)) throw new Error("Expected tool object");
        } catch { throw new LlmError("Chat tool call arguments are invalid", "TRANSPORT"); }
      }
      output.push({ type: "block-end", index, block: state.block });
    }
    if (this.usage) output.push({ type: "usage", usage: this.usage });
    output.push({ type: "finish", reason: this.stopReason === "tool_calls" ? { kind: "tool-calls" }
      : this.stopReason === "length" ? { kind: "max-tokens" } : { kind: "stop" },
      replayState: { response: { format: "oma-chat-completions-v1", messageID: this.messageID,
        ...(requestID ? { requestID } : {}), stopReason: this.stopReason },
        blocks: this.startOrder.map((index) => ({ index })) },
    });
    return output;
  }

  private setUsage(usage: NonNullable<ChatChunk["usage"]>): void {
    const cached = usage.prompt_tokens_details?.cached_tokens ?? 0;
    if (![usage.prompt_tokens, usage.completion_tokens, cached].every((value) => Number.isSafeInteger(value) && value >= 0)
      || cached > usage.prompt_tokens) throw new LlmError("Invalid Chat token usage", "TRANSPORT");
    this.usage = { inputTokens: usage.prompt_tokens - cached, outputTokens: usage.completion_tokens,
      ...(usage.prompt_tokens_details?.cached_tokens !== undefined ? { cacheReadTokens: cached } : {}) };
  }

  private text(text: string): StreamChunk[] {
    const output: StreamChunk[] = [];
    let state = this.blocks.get(0);
    if (!state) {
      state = { block: { type: "text", text: "" }, started: true, emittedArguments: 0 };
      this.blocks.set(0, state);
      this.startOrder.push(0);
      output.push({ type: "block-start", index: 0, blockType: "text" });
    }
    if (state.block.type !== "text") throw new LlmError("Invalid Chat text block", "TRANSPORT");
    state.block.text += text;
    this.contentBytes += new TextEncoder().encode(text).length;
    output.push({ type: "text-delta", index: 0, text });
    return output;
  }

  private tools(tools: ToolDelta): StreamChunk[] {
    const output: StreamChunk[] = [];
    for (const tool of tools ?? []) {
      if (!Number.isSafeInteger(tool.index) || tool.index < 0 || tool.index >= 1024
        || (tool.type && tool.type !== "function")) throw new LlmError("Invalid Chat tool index or type", "TRANSPORT");
      const index = tool.index + 1;
      let state = this.blocks.get(index);
      if (!state) {
        state = { block: { type: "tool-call", id: ToolCallId(""), name: "", arguments: "" }, started: false, emittedArguments: 0 };
        this.blocks.set(index, state);
      }
      if (state.block.type !== "tool-call") throw new LlmError("Invalid Chat tool block", "TRANSPORT");
      const block = state.block;
      this.contentBytes += new TextEncoder().encode((tool.id ?? "") + (tool.function?.name ?? "") + (tool.function?.arguments ?? "")).length;
      block.id = ToolCallId(block.id + (tool.id ?? ""));
      block.name += tool.function?.name ?? "";
      block.arguments += tool.function?.arguments ?? "";
      if (!block.id || !block.name) continue;
      if (!state.started) {
        state.started = true;
        this.startOrder.push(index);
        output.push({ type: "block-start", index, blockType: "tool-call" });
      }
      output.push({ type: "tool-call-delta", index, id: block.id, name: block.name,
        argumentsDelta: block.arguments.slice(state.emittedArguments) });
      state.emittedArguments = block.arguments.length;
    }
    return output;
  }
}
