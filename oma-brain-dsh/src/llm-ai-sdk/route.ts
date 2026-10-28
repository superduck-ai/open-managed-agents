import type { ImageInput } from "./images.ts";
import type { AttachmentStore } from "@deepseek-ai/dsh-attachment";
import type { LanguageModelV4, SharedV4ProviderOptions } from "@ai-sdk/provider";
import type { GenerateOptions, LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";

export interface AiSdkRoute {
  model: LanguageModelV4;
  imageInput?: ImageInput;
  maxRequestBodyBytes?: number;
  toolImagesAsUser?: boolean;
  replayFormat: string;
  signedReasoning?: boolean;
  maxOutputTokens?: number;
  maxOutputTokensLimit?: number;
  providerOptions?: SharedV4ProviderOptions;
}

export interface AiSdkAdapterOptions {
  attachments?: AttachmentStore;
  route(options: GenerateOptions): AiSdkRoute;
  imageInput?(provider: string, model: string): ImageInput | undefined;
  callScope?<T>(request: GenerateOptions, action: () => Promise<T>): Promise<T>;
  modelInfo?(provider: string, model: string): LlmResolvedModelInfo;
  history?(options: GenerateOptions): GenerateOptions;
  onCallStart?(options: GenerateOptions): void;
  onRequestBody?(options: GenerateOptions, body: Record<string, unknown>): void;
  onMessageID?(options: GenerateOptions, messageID: string): void;
}
