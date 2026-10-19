import { PiAiAdapter, type ResolvedPiAiProviderProfile } from "@deepseek-ai/dsh-llm-pi-ai";
import { resolveRetryPolicy, type GenerateOptions } from "@deepseek-ai/dsh-llm";
import { InMemoryCredentialStore, type Model, type Provider, type SimpleStreamOptions, type StreamOptions } from "@earendil-works/pi-ai";
import * as messages from "@earendil-works/pi-ai/api/anthropic-messages";
import * as completions from "@earendil-works/pi-ai/api/openai-completions";
import type { OmaApi } from "./api.ts";
import { logSystemPrompt } from "./prompt/debug.ts";
import { object } from "./json.ts";
import type { Assignment } from "./assignment.ts";

export interface ModelIdentity {
  messageID?: string;
  requestID?: string;
  indices: number[];
}

export function modelProvider(api: OmaApi, options: GenerateOptions, assignment: Assignment, backend: "messages" | "chat_completions", identity: ModelIdentity): PiAiAdapter {
  const token = assignment.model_token;
  const path = backend === "messages" ? "/v1/messages" : "/v1/chat/completions";
  const endpoint = `${api.base.replace(/\/$/, "")}${path}`;
  const model: Model<"anthropic-messages" | "openai-completions"> = {
    id: options.model, name: options.model, provider: "oma",
    api: backend === "messages" ? "anthropic-messages" : "openai-completions",
    baseUrl: backend === "messages" ? api.base : `${api.base.replace(/\/$/, "")}/v1`,
    reasoning: false, input: ["text"], contextWindow: 262144, maxTokens: backend === "messages" ? 8192 : 32768,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    ...(backend === "chat_completions" ? { compat: { supportsStore: false, supportsDeveloperRole: true,
      supportsUsageInStreaming: true, supportsFinishReason: true, maxTokensField: "max_completion_tokens" as const } } : {}),
  };
  const fetcher = (async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (`${url.origin}${url.pathname}` !== endpoint || request.method !== "POST") throw new Error("Model request must use the OMA proxy endpoint");
    const headers = new Headers(request.headers);
    headers.delete("x-api-key");
    headers.delete("cookie");
    headers.delete("x-claude-code-agent-id");
    headers.set("Authorization", `Bearer ${token}`);
    if (options.sessionId !== assignment.session_id) headers.set("X-Claude-Code-Agent-Id", String(options.sessionId));
    const response = await api.fetcher(request.url, { ...init, method: request.method, headers,
      body: init?.body ?? request.body, signal: request.signal, redirect: "error" });
    identity.requestID = response.headers.get("Request-Id") ?? undefined;
    return response;
  }) as typeof fetch;
  const transport = <Options extends StreamOptions>(input: Options): Options & StreamOptions => ({
    ...input, fetch: fetcher, maxRetries: 0, timeoutMs: 300000,
    onPayload: (payload) => { logSystemPrompt(String(options.sessionId), object(payload)); },
    onProviderStreamEvent: (event: unknown) => {
      if (event === null || typeof event !== "object") return;
      const metadata = event as { id?: unknown; type?: unknown; index?: unknown; message?: { id?: unknown } };
      const id = backend === "messages" ? metadata.message?.id : metadata.id;
      if (typeof id === "string" && id && !identity.messageID) { identity.messageID = id; api.modelResponses.set(String(options.sessionId), id); }
      if (backend === "messages" && metadata.type === "content_block_start" && typeof metadata.index === "number") identity.indices.push(metadata.index);
    },
  });
  const provider: Provider = {
    id: "oma", name: "OMA", getModels: () => [model],
    auth: { apiKey: { name: "OMA session token", resolve: async ({ credential }) => credential?.key ? { auth: { apiKey: credential.key } } : undefined } },
    stream: (resolved, context, input) => backend === "messages"
      ? messages.stream({ ...resolved, api: "anthropic-messages", compat: undefined }, context, transport(input ?? {}))
      : completions.stream({ ...resolved, api: "openai-completions" }, context, transport(input ?? {})),
    streamSimple: (resolved, context, input?: SimpleStreamOptions) => backend === "messages"
      ? messages.streamSimple({ ...resolved, api: "anthropic-messages", compat: undefined }, context, transport(input ?? {}))
      : completions.streamSimple({ ...resolved, api: "openai-completions" }, context, transport(input ?? {})),
  };
  const profile: ResolvedPiAiProviderProfile = {
    provider: "oma", displayName: "OMA", piProvider: provider, modelErrors: new Map(), configuredMaxTokens: new Map(),
    streamIdleTimeoutMs: 300000, maxRequestImageBytes: 20 * 1024 * 1024,
    requestImagePixelBudget: 2048 * 2048, requestImageMaxBytes: 1024 * 1024,
    retryPolicy: resolveRetryPolicy({ mode: "normal", maxRetries: 0 }, "oma"),
    cacheRetention: "none", transport: "sse",
  };
  const profiles = new Map([["oma", profile]]);
  return new PiAiAdapter({
    profiles: () => profiles, resolveApiKey: async () => token,
    auth: { credentials: new InMemoryCredentialStore(), authContext: { env: async () => undefined, fileExists: async () => false } },
  });
}
