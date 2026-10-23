import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";
import type { OmaApi } from "./api.ts";
import type { AiSdkAdapterOptions } from "./llm-ai-sdk/index.ts";
import { DEFAULT_CONTEXT_WINDOW } from "./model-defaults.ts";
import { modelSessionID, omaRoute } from "./model-provider.ts";
import { logSystemPrompt } from "./prompt/debug.ts";

interface PiAiReplay {
  response?: { kind?: string };
  blocks?: { thinkingSignature?: string }[];
}

export function omaModelOptions(api: OmaApi, attachments?: AttachmentStore, currentSession?: () => string | undefined): AiSdkAdapterOptions {
  return {
    attachments,
    modelInfo: (provider, model) => {
      const id = currentSession?.();
      const assignment = id === undefined ? undefined : api.assignment(api.modelSessions.get(id) ?? id);
      const config = assignment?.launch.model_options?.[model];
      const window = assignment?.launch.model_context_windows?.[model] ?? DEFAULT_CONTEXT_WINDOW;
      return {
        provider, id: model, name: model,
        inputModalities: config?.input_modalities ?? ["text"],
        ...(config?.max_output_tokens === undefined ? {} : { defaultMaxTokens: config.max_output_tokens }),
        context: { contextWindow: window },
      };
    },
    route: (options) => omaRoute(api, options),
    history: legacyReplay,
    onCallStart: (options) => { api.modelResponses.delete(modelSessionID(options)); },
    onRequestBody: (options, body) => { logSystemPrompt(modelSessionID(options), body); },
    onMessageID: (options, messageID) => { api.modelResponses.set(modelSessionID(options), messageID); },
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
