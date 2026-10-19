import type { GenerateOptions, ReplayEnvelope } from "@deepseek-ai/dsh-llm";

interface LegacyReplay {
  response?: { format?: string; messageID?: string; stopReason?: string };
  blocks?: { signature?: string }[];
}

export function modelHistory(options: GenerateOptions): GenerateOptions {
  return { ...options, messages: options.messages.map((message) => {
    if (message.role !== "assistant") return message;
    const legacy = message.source.replayState as LegacyReplay | undefined;
    const format = legacy?.response?.format;
    if (format !== "oma-messages-v1" && format !== "oma-chat-completions-v1") return message;
    const reason = legacy?.response?.stopReason;
    const replayState: ReplayEnvelope = {
      response: {
        kind: "pi-ai", version: 2, api: format === "oma-messages-v1" ? "anthropic-messages" : "openai-completions",
        provider: message.source.provider, model: message.source.model,
        ...(legacy?.response?.messageID ? { responseId: legacy.response.messageID } : {}),
        stopReason: reason === "tool_use" || reason === "tool_calls" ? "toolUse"
          : reason === "max_tokens" || reason === "length" ? "length" : "stop",
      },
      blocks: message.content.map((block, index) => block.type === "reasoning"
        ? { type: "reasoning", ...(legacy?.blocks?.[index]?.signature ? { thinkingSignature: legacy.blocks[index].signature } : {}) }
        : { type: block.type }),
    };
    return { ...message, source: { ...message.source, replayState } };
  }) };
}
