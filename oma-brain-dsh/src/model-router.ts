import { LlmAdapter, LlmError, type GenerateOptions, type StreamChunk, type LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "./api.ts";
import { MessagesAdapter } from "./model.ts";
import { ChatCompletionsAdapter } from "./chat-model.ts";

export class OmaModelAdapter extends LlmAdapter {
  private readonly messages: MessagesAdapter;
  private readonly chat: ChatCompletionsAdapter;

  constructor(readonly api: OmaApi) {
    super();
    this.messages = new MessagesAdapter(api);
    this.chat = new ChatCompletionsAdapter(api);
  }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    if (!options.sessionId) throw new LlmError("Model request has no session identity", "INVALID_ARGS");
    const assignment = this.api.assignment(this.api.modelSessions.get(options.sessionId) ?? options.sessionId);
    const backends = assignment.launch.model_backends;
    const backend = backends ? backends[options.model] : "messages";
    if (backend === "messages") return this.messages.stream(options);
    if (backend === "chat_completions") return this.chat.stream(options);
    throw new LlmError("Model API backend is unavailable; reload the session configuration", "INVALID_ARGS");
  }
}
