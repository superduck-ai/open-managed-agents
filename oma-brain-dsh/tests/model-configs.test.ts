import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { validateModels, type ModelConfig } from "../src/assignment.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import type { LlmCallConfig } from "@deepseek-ai/dsh-llm";

function trackModelBindings(session: ReturnType<typeof createOmaSession>, bindings: string[]): void {
  const configure = session.config.configureModel!;
  const tracked = (call: LlmCallConfig) => { bindings.push(call.model); return configure(call); };
  session.config.configureModel = tracked;
  session.runtime.config.configureModel = tracked;
}

test.each(["models", "permissions"] as const)("invalid refreshed %s reject the request without partially replacing session context", async invalid => {
  const backend = new Backend(); await backend.ready;
  const initial: Record<string, ModelConfig> = { "test-model": { id: "test-model", api_backend: "messages", context_window: 200000, max_output_tokens: 16000 } };
  backend.a.launch.models = structuredClone(initial);
  backend.onModel = async () => textEvents("done");
  let corrupt = false;
  const api = new OmaApi(backend.url, (async (input, init) => {
    const request = new Request(input, init);
    const response = await backend.route(request);
    if (!corrupt || new URL(request.url).pathname !== `/v2/sessions/${backend.a.session_id}`) return response;
    const body = await response.json();
    body.session_context.models = { "test-model": { id: "test-model", api_backend: "messages", context_window: 300000, max_output_tokens: invalid === "models" ? -1 : 32000 } };
    body.session_context.runtime_tool_permissions = { bash: invalid === "permissions" ? "invalid" : "deny" };
    return Response.json(body);
  }) as typeof fetch);
  const ctx = new Context();
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, backend.a);
    await session.open();
    corrupt = true;
    await session.receive("invalid-context", { content: "hello" });
    expect([...backend.publicLog.values()].some(event => event.type === "result" && event.is_error)).toBe(true);
    expect(backend.requests).toHaveLength(0);
    expect(backend.a.launch.models).toEqual(initial);
    expect(session.runtime.config.toolPermissions).toEqual(backend.a.launch.tool_permissions ?? {});
    corrupt = false;
    await session.receive("valid-context", { content: "try again" });
    expect(backend.requests).toHaveLength(1);
    expect(backend.requests[0]!.max_tokens).toBe(16000);
  } finally { corrupt = false; await ctx.fiber.dispose(); backend.stop(); }
});

test("model budgets bind exactly once after selection for live and restored requests", async () => {
  const backend = new Backend(); await backend.ready;
  backend.a.launch.models = { "test-model": { id: "test-model", api_backend: "messages", context_window: 200000, max_output_tokens: 16000 }, "other-model": { id: "other-model", api_backend: "messages", context_window: 300000, max_output_tokens: 32000 } };
  backend.onModel = async () => textEvents("done");
  const api = new OmaApi(backend.url);
  let ctx = new Context();
  const bindings: string[] = [];
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, backend.a);
    trackModelBindings(session, bindings);
    await session.open();
    await session.receive("first", { content: "hello" });
    await session.control({ type: "control_request", request_id: "select", request: { subtype: "set_model", model: "other-model" } });
    await session.receive("selected", { content: "continue" });
    expect(bindings).toEqual(["test-model", "other-model"]);
    await session.close(); await ctx.fiber.dispose();
    ctx = new Context(); await mountOma(ctx, api);
    const resumed = createOmaSession(ctx, api, backend.a);
    trackModelBindings(resumed, bindings);
    await resumed.open();
    await resumed.receive("restored", { content: "continue after restart" });
    expect(bindings).toEqual(["test-model", "other-model", "other-model"]);
    expect(backend.requests.map(request => [request.model, request.max_tokens])).toEqual([
      ["test-model", 16000], ["other-model", 32000], ["other-model", 32000],
    ]);
  } finally { await ctx.fiber.dispose(); backend.stop(); }
});

for (const value of [null, [], { model: null }, ...[
  { max_output_tokens: 0 }, { context_window: -1 }, { api_backend: "invalid" },
  { max_output_tokens: 1.5 }, { max_output_tokens: Number.MAX_SAFE_INTEGER + 1 }, { input_modalities: [] },
].map(config => ({ model: { id: "model", api_backend: "messages", ...config } }))]) {
  test(`reject invalid model configs ${JSON.stringify(value)}`, () => expect(() => validateModels(value)).toThrow());
}

test("session-scoped model configs isolate shared model IDs and follow selection and updates", async () => {
  const firstBackend = new Backend(); const secondBackend = new Backend();
  await Promise.all([firstBackend.ready, secondBackend.ready]);
  firstBackend.a.model_token = "first-model"; secondBackend.a.model_token = "second-model";
  firstBackend.a.ingress_token = "first-ingress"; secondBackend.a.ingress_token = "second-ingress";
  firstBackend.a.launch.models = { "test-model": { id: "test-model", api_backend: "messages", context_window: 200000, max_output_tokens: 16000 }, "other-model": { id: "other-model", api_backend: "messages", context_window: 400000, max_output_tokens: 64000 } };
  secondBackend.a.launch.models = { "test-model": { id: "test-model", api_backend: "messages", context_window: 300000, max_output_tokens: 32000 } };
  firstBackend.onModel = secondBackend.onModel = async () => textEvents("done");
  const api = new OmaApi("http://model-configs.test", (async (input, init) => {
    const request = new Request(input, init);
    const selected = [firstBackend, secondBackend].find(backend => request.url.includes(backend.a.session_id)
      || request.headers.get("Authorization") === `Bearer ${backend.a.model_token}`);
    if (!selected) throw new Error("Request has no session owner");
    return selected.route(request);
  }) as typeof fetch);
  const ctx = new Context();
  try {
    await mountOma(ctx, api);
    const first = createOmaSession(ctx, api, firstBackend.a); const second = createOmaSession(ctx, api, secondBackend.a);
    await Promise.all([first.open(), second.open()]);
    const firstInfo = await ctx.llm.resolveModelInfo(first.config.provider, "test-model");
    const secondInfo = await ctx.llm.resolveModelInfo(second.config.provider, "test-model");
    expect(firstInfo.context?.contextWindow).toBe(200000);
    expect(secondInfo.context?.contextWindow).toBe(300000);
    await Promise.all([first.receive("first", { content: "hello" }), second.receive("second", { content: "hello" })]);
    expect(firstBackend.requests[0]!.max_tokens).toBe(16000);
    expect(secondBackend.requests[0]!.max_tokens).toBe(32000);
    await first.control({ type: "control_request", request_id: "switch", request: { subtype: "set_model", model: "other-model" } });
    await first.receive("switched", { content: "continue" });
    expect(firstBackend.requests.at(-1)).toMatchObject({ model: "other-model", max_tokens: 64000 });
    expect(first.runtime.agent.session.requestHeader()?.config.maxTokens).toBe(64000);
    firstBackend.a.launch.models["other-model"] = { id: "other-model", api_backend: "messages", context_window: 250000, max_output_tokens: 24000 };
    await first.receive("updated", { content: "continue" });
    expect(firstBackend.requests.at(-1)!.max_tokens).toBe(24000);
    expect((await ctx.llm.resolveModelInfo(first.config.provider, "other-model")).context?.contextWindow).toBe(250000);
    firstBackend.a.launch.models["other-model"] = { id: "other-model", api_backend: "messages" };
    await first.receive("removed", { content: "continue" });
    expect(first.runtime.agent.session.requestHeader()?.config.maxTokens).toBe(32768);
    expect((await ctx.llm.resolveModelInfo(first.config.provider, "other-model")).context?.contextWindow).toBe(262144);
    await Promise.all([first.close(), second.close()]);
  } finally { await ctx.fiber.dispose(); firstBackend.stop(); secondBackend.stop(); }
});

test("child requests and restored sessions use their selected model config", async () => {
  const backend = new Backend(); await backend.ready;
  backend.a.launch.models = { "test-model": { id: "test-model", api_backend: "messages", context_window: 200000, max_output_tokens: 32000 }, "child-model": { id: "child-model", api_backend: "messages", context_window: 128000, max_output_tokens: 8000 } };
  backend.a.launch.subagents = { inlineAgents: false, agents: [{ name: "specialist", description: "Specialist", systemPrompt: "Help", model: "child-model", tools: [] }] };
  let parentCalls = 0;
  backend.onModel = async request => {
    if (request.model === "child-model") return textEvents("child done");
    if (parentCalls++ === 0) return toolEvents().map(event => event.type === "content_block_start"
      ? { ...event, content_block: { ...event.content_block, name: "send_to_agent" } }
      : event.type === "content_block_delta" ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify({ agent_name: "specialist", message: "help" }) } } : event);
    return textEvents("parent done");
  };
  let ctx = new Context();
  const api = new OmaApi(backend.url);
  const bindings: string[] = [];
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, backend.a);
    trackModelBindings(session, bindings);
    backend.onDelegationApproval = async payload => { await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } }); };
    await session.open(); await session.receive("delegate", { content: "delegate" });
    expect(backend.requests.filter(request => request.model === "test-model").every(request => request.max_tokens === 32000)).toBe(true);
    expect(backend.requests.filter(request => request.model === "child-model").map(request => request.max_tokens)).toEqual([8000]);
    expect(bindings).toEqual(backend.requests.map(request => request.model));
    await session.close(); await ctx.fiber.dispose();
    backend.a.launch.models["test-model"] = { id: "test-model", api_backend: "messages", context_window: 300000, max_output_tokens: 16000 };
    ctx = new Context(); await mountOma(ctx, api);
    const resumed = createOmaSession(ctx, api, backend.a); await resumed.open();
    trackModelBindings(resumed, bindings);
    await resumed.receive("resumed", { content: "continue after restart" });
    expect(backend.requests.at(-1)).toMatchObject({ model: "test-model", max_tokens: 16000 });
    expect(bindings).toEqual(backend.requests.map(request => request.model));
    await resumed.close();
  } finally { await ctx.fiber.dispose(); backend.stop(); }
});
