import type { RequestImageAttachment } from "@deepseek-ai/dsh-attachment";
import { imageTarget, imagePlaceholder, imagePolicy } from "./images.ts";
import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
import type { JSONSchema7, LanguageModelV4CallOptions, LanguageModelV4Message, LanguageModelV4Prompt } from "@ai-sdk/provider";
import {
  LlmError, attributionHeaders, requiredImageOffload, IMAGE_OFFLOAD_REQUIRED_CODE, type AssistantMessage, type ContentBlock, type GenerateOptions, type ToolResultMessage,
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
  const requestedMaxOutputTokens = options.maxTokens ?? route.maxOutputTokens;
  const maxOutputTokens = route.maxOutputTokensLimit === undefined ? requestedMaxOutputTokens
    : Math.min(requestedMaxOutputTokens ?? route.maxOutputTokensLimit, route.maxOutputTokensLimit);
  const versions = new Map<string, RequestImageAttachment>();
  const messages = options.messages.map(message => message.role === "tool" && message.isError
    ? { ...message, content: message.content.filter(block => block.type === "text") } : message);
  const images = messages.flatMap(message => message.content).filter((block): block is Extract<ContentBlock, { type: "image" }> => block.type === "image" && !block.offloaded);
  const policy = imagePolicy(route.imageInput);
  const countOffload = requiredImageOffload(messages, { representation: "base64", maxImages: policy.maxImages }, () => 0);
  if (countOffload > 0) throw new LlmError("Image count budget exceeded", IMAGE_OFFLOAD_REQUIRED_CODE, { offloadImages: countOffload });
  for (const image of images) {
    if (!attachments) throw new LlmError("Image attachment storage is unavailable", "INVALID_ARGS");
    if (!versions.has(image.attachment.attachmentId)) versions.set(image.attachment.attachmentId,
      await attachments.readImageRequest(image.attachment, imageTarget(image.attachment, route.imageInput), options.signal));
  }
  const content = await prompt({ ...options, messages }, route, versions);
  const requestBytes = Buffer.byteLength(JSON.stringify({ prompt: content, tools: options.tools }, (_key, value) => value instanceof Uint8Array ? Buffer.from(value).toString("base64") : value));
  const imageBytes = images.reduce((sum, block) => sum + 4 * Math.ceil(versions.get(block.attachment.attachmentId)!.bytes / 3), 0);
  const available = Math.max(0, (route.maxRequestBodyBytes ?? Infinity) - 128 * 1024 - (requestBytes - imageBytes));
  const offloadImages = requiredImageOffload(messages,
    { representation: "base64", maxImages: policy.maxImages, maxBytes: Math.min(4 * Math.ceil(policy.maxBytes / 3), available) },
    block => versions.get(block.attachment.attachmentId)!.bytes);
  if (offloadImages > 0) throw new LlmError("Image request budget exceeded", IMAGE_OFFLOAD_REQUIRED_CODE, { offloadImages });
  return {
    prompt: content,
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

async function prompt(options: GenerateOptions, route: AiSdkRoute, versions: Map<string, RequestImageAttachment>): Promise<LanguageModelV4Prompt> {
  const system = options.system ? [options.system] : [];
  const toolNames = new Map<string, string>();
  const messages: LanguageModelV4Prompt = [];
  let pendingToolImages: UserPart[] = [];
  for (const message of options.messages) {
    if (message.role !== "tool" && pendingToolImages.length) {
      messages.push({ role: "user", content: pendingToolImages });
      pendingToolImages = [];
    }
    if (message.role === "system") system.push(...message.content.map(textOf));
    else if (message.role === "assistant") messages.push({ role: "assistant", content: assistantParts(message, route, toolNames) });
    else if (message.role === "tool") {
      const content = await Promise.all(message.content.map(block => userPart(block, versions)));
      const images = route.toolImagesAsUser ? content.filter(part => part.type === "file") : [];
      if (images.length) {
        pendingToolImages.push({ type: "text", text: `Image output for tool call ${message.toolCallId}:` }, ...images);
        messages.push({ role: "tool", content: [toolResult(message, toolNames, content.filter(part => part.type === "text"))] });
      } else messages.push({ role: "tool", content: [toolResult(message, toolNames, content)] });
    }
    else if (message.role === "user") {
      messages.push({ role: "user", content: await Promise.all(message.content.map(block => userPart(block, versions))) });
    }
    else throw new LlmError(`Unsupported model input role: ${message.role}`, "INVALID_ARGS");
  }
  if (pendingToolImages.length) messages.push({ role: "user", content: pendingToolImages });
  return system.length ? [{ role: "system", content: system.join("\n\n") }, ...messages] : messages;
}

type UserPart = Extract<LanguageModelV4Message, { role: "user" }>["content"][number];

async function userPart(block: ContentBlock, versions: Map<string, RequestImageAttachment>): Promise<UserPart> {
  if (block.type === "text") return { type: "text", text: block.text };
  if (block.type === "image") {
    if (block.offloaded) return { type: "text", text: imagePlaceholder(block.attachment) };
    const image = versions.get(block.attachment.attachmentId)!;
    return { type: "file", mediaType: image.mediaType, data: { type: "data", data: image.data } };
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

function toolResult(message: ToolResultMessage, toolNames: Map<string, string>, content: UserPart[]): Extract<LanguageModelV4Message, { role: "tool" }>["content"][number] {
  const value = content.filter(part => part.type === "text").map(part => part.text).join("\n");
  return { type: "tool-result", toolCallId: message.toolCallId, toolName: toolNames.get(message.toolCallId) ?? "",
    output: message.isError ? { type: "error-text", value }
      : content.some(part => part.type === "file") ? { type: "content", value: content } : { type: "text", value } };
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
