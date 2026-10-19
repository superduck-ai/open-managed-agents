import type { JSONSchema7, LanguageModelV4CallOptions, LanguageModelV4Message, LanguageModelV4Prompt } from "@ai-sdk/provider";
import {
  LlmError, attributionHeaders, type AssistantMessage, type ContentBlock, type GenerateOptions, type ToolResultMessage,
} from "@deepseek-ai/dsh-llm";
import type { AiSdkRoute } from "./route.ts";

type AssistantPart = Extract<LanguageModelV4Message, { role: "assistant" }>["content"][number];

interface ReplayBlocks {
  response?: { format?: string };
  blocks?: { signature?: string; redactedData?: string }[];
}

interface SignedReasoning {
  signature?: string;
  redactedData?: string;
}

export function callOptions(options: GenerateOptions, route: AiSdkRoute): LanguageModelV4CallOptions {
  const maxOutputTokens = options.maxTokens ?? route.maxOutputTokens;
  return {
    prompt: prompt(options, route),
    headers: attributionHeaders(),
    ...(options.tools?.length ? { tools: options.tools.map((tool) => ({ type: "function" as const, name: tool.name,
      description: tool.description, inputSchema: tool.parameters as JSONSchema7 })) } : {}),
    ...(maxOutputTokens !== undefined ? { maxOutputTokens } : {}),
    ...(options.temperature !== undefined ? { temperature: options.temperature } : {}),
    ...(options.stop?.length ? { stopSequences: options.stop } : {}),
    ...(options.signal ? { abortSignal: options.signal } : {}),
    ...(route.providerOptions ? { providerOptions: route.providerOptions } : {}),
  };
}

function prompt(options: GenerateOptions, route: AiSdkRoute): LanguageModelV4Prompt {
  const system = options.system ? [options.system] : [];
  const toolNames = new Map<string, string>();
  const messages: LanguageModelV4Prompt = [];
  for (const message of options.messages) {
    if (message.role === "system") system.push(...message.content.map(textOf));
    else if (message.role === "assistant") messages.push({ role: "assistant", content: assistantParts(message, route, toolNames) });
    else if (message.role === "tool") messages.push({ role: "tool", content: [toolResult(message, toolNames)] });
    else if (message.role === "user") messages.push({ role: "user", content: message.content.map((block) => ({ type: "text", text: textOf(block) })) });
    else throw new LlmError(`Unsupported model input role: ${message.role}`, "INVALID_ARGS");
  }
  return system.length ? [{ role: "system", content: system.join("\n\n") }, ...messages] : messages;
}

function assistantParts(message: AssistantMessage, route: AiSdkRoute, toolNames: Map<string, string>): AssistantPart[] {
  const replay = message.source.replayState as ReplayBlocks | undefined;
  const signed = replay?.response?.format === route.replayFormat ? replay.blocks : undefined;
  return message.content.map((block, index): AssistantPart => {
    if (block.type === "text") return { type: "text", text: block.text };
    if (block.type === "reasoning") return reasoningPart(block.text, signed?.[index] ?? {}, route);
    if (block.type === "tool-call") {
      toolNames.set(block.id, block.name);
      return { type: "tool-call", toolCallId: block.id, toolName: block.name, input: toolInput(block.arguments) };
    }
    throw new LlmError(`Unsupported model input block: ${block.type}`, "INVALID_ARGS");
  });
}

function reasoningPart(text: string, replay: SignedReasoning, route: AiSdkRoute): AssistantPart {
  if (!route.signedReasoning) return { type: "reasoning", text };
  if (replay.signature) return { type: "reasoning", text, providerOptions: { anthropic: { signature: replay.signature } } };
  if (replay.redactedData) return { type: "reasoning", text, providerOptions: { anthropic: { redactedData: replay.redactedData } } };
  return { type: "text", text };
}

function toolResult(message: ToolResultMessage, toolNames: Map<string, string>) {
  const value = message.content.map(textOf).join("\n");
  return { type: "tool-result" as const, toolCallId: message.toolCallId, toolName: toolNames.get(message.toolCallId) ?? "",
    output: message.isError ? { type: "error-text" as const, value } : { type: "text" as const, value } };
}

function textOf(block: ContentBlock): string {
  if (block.type !== "text") throw new LlmError(`Unsupported model input block: ${block.type}`, "INVALID_ARGS");
  return block.text;
}

function toolInput(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return { __unparsedToolInput: { raw, len: raw.length } };
  }
}
