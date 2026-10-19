import { LlmAdapter, LlmError, type GenerateOptions, type StreamChunk, type LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "./api.ts";
import { providerError } from "./model.ts";
import { chatRequest } from "./chat-request.ts";
import { ChatStream, type ChatChunk } from "./chat-stream.ts";
import { sse } from "./sse.ts";

export class ChatCompletionsAdapter extends LlmAdapter {
  constructor(readonly api: OmaApi) { super(); }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    options.signal?.throwIfAborted();
    if (!options.sessionId) throw new LlmError("Model request has no session identity", "INVALID_ARGS");
    const assignment = this.api.assignment(this.api.modelSessions.get(options.sessionId) ?? options.sessionId);
    this.api.modelResponses.delete(options.sessionId);
    const response = await this.api.requestResponse("/v1/chat/completions", assignment.model_token, "POST", chatRequest(options),
      { Accept: "text/event-stream",
        ...(options.sessionId !== assignment.session_id ? { "X-Claude-Code-Agent-Id": options.sessionId } : {}),
      }, options.signal, 300_000);
    if (!response.ok) {
      let detail = `Chat Completions HTTP ${response.status}`;
      try { detail = (await response.json()).error?.message ?? detail; } catch {}
      throw providerError(detail, response.status);
    }
    if (!response.body) throw new LlmError("Chat Completions response has no stream", "TRANSPORT");
    const stream = new ChatStream();
    for await (const frame of sse(response.body, options.signal)) {
      if (!frame.data) continue;
      if (frame.data === "[DONE]") {
        yield* stream.finish(response.headers.get("Request-Id"));
        return;
      }
      const chunk = JSON.parse(frame.data) as ChatChunk;
      if (chunk.error) throw providerError(chunk.error.message ?? "Chat Completions stream error");
      const chunks = stream.accept(chunk);
      if (stream.messageID) this.api.modelResponses.set(options.sessionId, stream.messageID);
      yield* chunks;
    }
    throw new LlmError("Chat stream ended without terminal marker [DONE]", "TRANSPORT");
  }
}
