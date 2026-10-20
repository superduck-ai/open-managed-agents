import { createAnthropic } from "@ai-sdk/anthropic";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import { LlmError, type GenerateOptions } from "@deepseek-ai/dsh-llm";
import type { OmaApi } from "./api.ts";
import type { Assignment } from "./assignment.ts";
import { normalizeChatReasoning } from "./chat-reasoning.ts";
import type { AiSdkRoute } from "./llm-ai-sdk/index.ts";
import { traceHeaders } from "./trace-context.ts";

type ModelBackend = "messages" | "chat_completions";

const MODEL_TIMEOUT_MS = 300_000;
const MESSAGES_DEFAULT_MAX_TOKENS = 8192;

export function modelSessionID(options: GenerateOptions): string {
  if (!options.sessionId) throw new LlmError("Model request has no session identity", "INVALID_ARGS");
  return String(options.sessionId);
}

export function omaRoute(api: OmaApi, options: GenerateOptions): AiSdkRoute {
  const sessionId = modelSessionID(options);
  const assignment = api.assignment(api.modelSessions.get(sessionId) ?? sessionId);
  const backend = modelBackend(assignment, options.model);
  const root = `${api.base.replace(/\/$/, "")}/v1`;
  const agentID = sessionId === assignment.session_id ? undefined : sessionId;
  const extraHeaders = { ...traceHeaders(assignment.trace_parent), ...(agentID ? { "X-Claude-Code-Agent-Id": agentID } : {}) };
  if (backend === "messages") {
    const fetcher = proxyFetch(api, `${root}/messages`, extraHeaders);
    return { model: createAnthropic({ baseURL: root, authToken: assignment.model_token, fetch: fetcher })(options.model),
      replayFormat: "oma-messages-v1", signedReasoning: true, maxOutputTokens: MESSAGES_DEFAULT_MAX_TOKENS,
      providerOptions: { anthropic: { toolStreaming: false } } };
  }
  const fetcher = proxyFetch(api, `${root}/chat/completions`, extraHeaders, normalizeChatReasoning);
  return { model: createOpenAICompatible({ name: "oma", baseURL: root, apiKey: assignment.model_token, includeUsage: true,
    fetch: fetcher, transformRequestBody: chatRequestBody })(options.model), replayFormat: "oma-chat-completions-v1" };
}

function modelBackend(assignment: Assignment, model: string): ModelBackend {
  const backend = assignment.launch.model_backends ? assignment.launch.model_backends[model] : "messages";
  if (backend !== "messages" && backend !== "chat_completions")
    throw new LlmError("Model API backend is unavailable; reload the session configuration", "INVALID_ARGS");
  return backend;
}

function chatRequestBody({ max_tokens: maxTokens, ...body }: Record<string, any>): Record<string, any> {
  return maxTokens === undefined ? body : { ...body, max_completion_tokens: maxTokens };
}

function proxyFetch(api: OmaApi, endpoint: string, extraHeaders: Record<string, string>,
  response: (value: Response) => Response = (value) => value): typeof fetch {
  return (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = input instanceof Request ? input.url : String(input);
    if (url !== endpoint || init?.method !== "POST")
      throw new LlmError("Model request must use the OMA proxy endpoint", "INVALID_ARGS");
    const headers = new Headers(init.headers);
    headers.delete("x-api-key");
    for (const [name, value] of Object.entries(extraHeaders)) headers.set(name, value);
    const timeout = AbortSignal.timeout(MODEL_TIMEOUT_MS);
    return response(await api.fetcher(endpoint, { method: "POST", headers, body: init.body, redirect: "error",
      signal: init.signal ? AbortSignal.any([init.signal, timeout]) : timeout }));
  }) as typeof fetch;
}
