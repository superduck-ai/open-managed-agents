import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";
import type { OmaApi } from "./api.ts";
import type { AiSdkAdapterOptions } from "./llm-ai-sdk/index.ts";
import { modelSessionID, omaRoute } from "./model-provider.ts";
import { logSystemPrompt } from "./prompt/debug.ts";

interface PiAiReplay {
  response?: { kind?: string };
  blocks?: { thinkingSignature?: string }[];
}

export function omaModelOptions(api: OmaApi, attachments?: AttachmentStore, modelInfo?: AiSdkAdapterOptions["modelInfo"]): AiSdkAdapterOptions {
  return {
    attachments,
    modelInfo,
    route: (options) => ({ ...omaRoute(api, options),
      maxOutputTokens: modelInfo?.(options.provider, options.model).defaultMaxTokens }),
    history: legacyReplay,
    onCallStart: (options) => { if (options.purpose !== "compaction") api.modelResponses.delete(modelSessionID(options)); },
    onRequestBody: (options, body) => { logSystemPrompt(modelSessionID(options), body); },
    onMessageID: (options, messageID) => { if (options.purpose !== "compaction") api.modelResponses.set(modelSessionID(options), messageID); },
  };
}

function legacyReplay(options: GenerateOptions): GenerateOptions {
  return { ...options, messages: options.messages.map((message) => {
    if (message.role !== "assistant") return message;
    const replay = message.source.replayState as PiAiReplay | undefined;
    if (replay?.response?.kind !== "pi-ai") return message;
    const replayState = { response: { format: "oma-messages-v1" },
      blocks: (replay.blocks ?? []).map((block) => ({ signature: block.thinkingSignature })) };
    return { ...message, source: { ...message.source, replayState } };
  }) };
}
