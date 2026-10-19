import { LlmAdapter, LlmError, type GenerateOptions, type StreamChunk, type LlmResolvedModelInfo } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "./api.ts";
import { modelProvider, type ModelIdentity } from "./model-provider.ts";
import { object } from "./json.ts";
import { modelHistory } from "./model-history.ts";

export class OmaModelAdapter extends LlmAdapter {
  constructor(readonly api: OmaApi) { super(); }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    options.signal?.throwIfAborted();
    if (!options.sessionId) throw new LlmError("Model request has no session identity", "INVALID_ARGS");
    const assignment = this.api.assignment(this.api.modelSessions.get(options.sessionId) ?? options.sessionId);
    const backend = assignment.launch.model_backends ? assignment.launch.model_backends[options.model] : "messages";
    if (backend !== "messages" && backend !== "chat_completions")
      throw new LlmError("Model API backend is unavailable; reload the session configuration", "INVALID_ARGS");
    this.api.modelResponses.delete(options.sessionId);
    const identity: ModelIdentity = { indices: [] };
    const adapter = modelProvider(this.api, options, assignment, backend, identity);
    for await (const chunk of adapter.stream(modelHistory(options))) {
      if (chunk.type !== "finish" || !chunk.replayState) {
        yield "index" in chunk ? { ...chunk, index: identity.indices[chunk.index] ?? chunk.index } : chunk;
        continue;
      }
      const blocks = chunk.replayState.blocks?.map((block, index) => ({ ...object(block), omaIndex: identity.indices[index] ?? index }));
      yield { ...chunk, replayState: { ...chunk.replayState, ...(blocks ? { blocks } : {}), response: { ...object(chunk.replayState.response),
        oma: { ...(identity.messageID ? { messageID: identity.messageID } : {}),
          ...(identity.requestID ? { requestID: identity.requestID } : {}) },
      } } };
    }
  }
}
