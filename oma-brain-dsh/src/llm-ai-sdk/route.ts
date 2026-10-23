import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
import type { LanguageModelV4, SharedV4ProviderOptions } from "@ai-sdk/provider";
import type { GenerateOptions, LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";

export interface AiSdkRoute {
  model: LanguageModelV4;
  replayFormat: string;
  signedReasoning?: boolean;
  maxOutputTokens?: number;
  providerOptions?: SharedV4ProviderOptions;
}

export interface AiSdkAdapterOptions {
  modelInfo?(provider: string, model: string): LlmResolvedModelInfo;
  attachments?: AttachmentStore;
  route(options: GenerateOptions): AiSdkRoute;
  modelInfo?(provider: string, model: string): LlmResolvedModelInfo;
  history?(options: GenerateOptions): GenerateOptions;
  onCallStart?(options: GenerateOptions): void;
  onRequestBody?(options: GenerateOptions, body: Record<string, unknown>): void;
  onMessageID?(options: GenerateOptions, messageID: string): void;
}
