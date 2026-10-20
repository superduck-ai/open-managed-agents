import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
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

export async function callOptions(options: GenerateOptions, route: AiSdkRoute, attachments?: AttachmentStore): Promise<LanguageModelV4CallOptions> {
  const maxOutputTokens = options.maxTokens ?? route.maxOutputTokens;
  return {
    prompt: await prompt(options, route, attachments),
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

async function prompt(options: GenerateOptions, route: AiSdkRoute, attachments?: AttachmentStore): Promise<LanguageModelV4Prompt> {
  const system = options.system ? [options.system] : [];
  const toolNames = new Map<string, string>();
  const messages: LanguageModelV4Prompt = [];
  for (const message of options.messages) {
    if (message.role === "system") system.push(...message.content.map(textOf));
    else if (message.role === "assistant") messages.push({ role: "assistant", content: assistantParts(message, route, toolNames) });
    else if (message.role === "tool") messages.push({ role: "tool", content: [toolResult(message, toolNames)] });
    else if (message.role === "user") messages.push({ role: "user", content: await Promise.all(message.content.map(block => userPart(block, attachments, options.signal))) });
    else throw new LlmError(`Unsupported model input role: ${message.role}`, "INVALID_ARGS");
  }
  return system.length ? [{ role: "system", content: system.join("\n\n") }, ...messages] : messages;
}

type UserPart = Extract<LanguageModelV4Message, { role: "user" }>["content"][number];

async function userPart(block: ContentBlock, attachments?: AttachmentStore, signal?: AbortSignal): Promise<UserPart> {
  if (block.type === "text") return { type: "text", text: block.text };
  if (block.type === "image") {
    if (!attachments) throw new LlmError("Image attachment storage is unavailable", "INVALID_ARGS");
    const image = await attachments.readImage(block.attachment, signal);
    return { type: "file", mediaType: image.ref.mediaType, data: { type: "data", data: image.data } };
  }
  if (block.type === "oma-image") {
    const { source } = block;
    return { type: "file", mediaType: "image/*", data: source.type === "url"
      ? { type: "url", url: new URL(source.url) } : { type: "reference", reference: { anthropic: source.file_id } } };
  }
  if (block.type === "oma-document") {
    const { source, title, context, citations } = block.document;
    if (source.type === "text" && !citations?.enabled)
      return { type: "text", text: JSON.stringify({ type: "document", title, context, content: source.data }) };
    const data = source.type === "text" ? { type: "text" as const, text: source.data }
      : source.type === "base64" ? { type: "data" as const, data: source.data }
      : source.type === "url" ? { type: "url" as const, url: new URL(source.url) }
      : { type: "reference" as const, reference: { anthropic: source.file_id } };
    return { type: "file", mediaType: source.type === "text" ? "text/plain" : "application/pdf", data,
      providerOptions: { anthropic: { ...(title ? { title } : {}), ...(context ? { context } : {}), ...(citations ? { citations } : {}) } } };
  }
  throw new LlmError(`Unsupported model input block: ${block.type}`, "INVALID_ARGS");
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
