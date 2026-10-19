import type { LanguageModelV4StreamResult } from "@ai-sdk/provider";
import { LlmAdapter, LlmError, type GenerateOptions, type StreamChunk, type LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "./api.ts";
import { languageModel, modelBackend } from "./model-provider.ts";
import { modelCall } from "./model-prompt.ts";
import { ModelStream, modelError } from "./model-stream.ts";
import { logSystemPrompt } from "./prompt/debug.ts";

export class OmaModelAdapter extends LlmAdapter {
  constructor(readonly api: OmaApi) { super(); }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    options.signal?.throwIfAborted();
    if (!options.sessionId) throw new LlmError("Model request has no session identity", "INVALID_ARGS");
    const sessionId = String(options.sessionId);
    const assignment = this.api.assignment(this.api.modelSessions.get(sessionId) ?? sessionId);
    const backend = modelBackend(assignment, options.model);
    const call = modelCall(options, backend);
    this.api.modelResponses.delete(sessionId);
    let result: LanguageModelV4StreamResult;
    try {
      result = await languageModel(this.api, assignment, backend, sessionId, options.model).doStream(call);
    } catch (error) { throw modelError(error, options.signal); }
    if (result.request?.body && typeof result.request.body === "object") logSystemPrompt(sessionId, result.request.body);
    const stream = new ModelStream(backend, result.response?.headers?.["request-id"]);
    const reader = result.stream.getReader();
    const cancel = () => { reader.cancel(options.signal?.reason).catch(() => undefined); };
    options.signal?.addEventListener("abort", cancel, { once: true });
    try {
      for (let next = await reader.read(); !next.done; next = await reader.read()) {
        yield* stream.accept(next.value);
        if (stream.messageID) this.api.modelResponses.set(sessionId, stream.messageID);
      }
      options.signal?.throwIfAborted();
      yield* stream.finish();
    } catch (error) {
      throw modelError(error, options.signal);
    } finally {
      options.signal?.removeEventListener("abort", cancel);
      cancel();
    }
  }
}
