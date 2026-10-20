import type { LanguageModelV4StreamResult } from "@ai-sdk/provider";
import { LlmAdapter, type GenerateOptions, type LlmResolvedModelInfo, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { callOptions } from "./prompt.ts";
import type { AiSdkAdapterOptions } from "./route.ts";
import { AiSdkStream, modelError } from "./stream.ts";

export class AiSdkAdapter extends LlmAdapter {
  constructor(private readonly options: AiSdkAdapterOptions) { super(); }

  async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> {
    return { provider, id: model, name: model };
  }

  async *stream(request: GenerateOptions): AsyncIterable<StreamChunk> {
    request.signal?.throwIfAborted();
    const route = this.options.route(request);
    const call = await callOptions(this.options.history?.(request) ?? request, route, this.options.attachments);
    this.options.onCallStart?.(request);
    let result: LanguageModelV4StreamResult;
    try {
      result = await route.model.doStream(call);
    } catch (error) { throw modelError(error, request.signal); }
    const body = result.request?.body;
    if (body && typeof body === "object") this.options.onRequestBody?.(request, body as Record<string, unknown>);
    const headers = result.response?.headers;
    yield* this.read(request, new AiSdkStream(route.replayFormat, headers?.["request-id"] ?? headers?.["x-request-id"]), result);
  }

  private async *read(request: GenerateOptions, stream: AiSdkStream, result: LanguageModelV4StreamResult): AsyncIterable<StreamChunk> {
    const reader = result.stream.getReader();
    const cancel = () => { reader.cancel(request.signal?.reason).catch(() => undefined); };
    request.signal?.addEventListener("abort", cancel, { once: true });
    let messageID: string | undefined;
    try {
      for (let next = await reader.read(); !next.done; next = await reader.read()) {
        yield* stream.accept(next.value);
        if (stream.messageID && stream.messageID !== messageID) this.options.onMessageID?.(request, messageID = stream.messageID);
      }
      request.signal?.throwIfAborted();
      yield* stream.finish();
    } catch (error) {
      throw modelError(error, request.signal);
    } finally {
      request.signal?.removeEventListener("abort", cancel);
      cancel();
    }
  }
}
