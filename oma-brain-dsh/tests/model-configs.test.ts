import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { validateModelConfigs } from "../src/model-configs.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";

for (const value of [null, [], { model: null }, { model: { max_tokens: 0 } }, { model: { context_window: -1 } },
  { model: { context_window: 10, max_tokens: 10 } }, { model: { max_tokens: 1.5 } }, { model: { max_tokens: Number.MAX_SAFE_INTEGER + 1 } },
  { model: { contextWindow: 200000 } }]) {
  test(`reject invalid model configs ${JSON.stringify(value)}`, () => expect(() => validateModelConfigs(value)).toThrow());
}

test("session-scoped model configs isolate shared model IDs and follow selection and updates", async () => {
  const firstBackend = new Backend(); const secondBackend = new Backend();
  await Promise.all([firstBackend.ready, secondBackend.ready]);
  firstBackend.a.model_token = "first-model"; secondBackend.a.model_token = "second-model";
  firstBackend.a.ingress_token = "first-ingress"; secondBackend.a.ingress_token = "second-ingress";
  firstBackend.a.launch.model_configs = { "test-model": { context_window: 200000, max_tokens: 16000 }, "other-model": { context_window: 400000, max_tokens: 64000 } };
  secondBackend.a.launch.model_configs = { "test-model": { context_window: 300000, max_tokens: 32000 } };
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
    firstBackend.a.launch.model_configs["other-model"] = { context_window: 250000, max_tokens: 24000 };
    await first.receive("updated", { content: "continue" });
    expect(firstBackend.requests.at(-1)!.max_tokens).toBe(24000);
    expect((await ctx.llm.resolveModelInfo(first.config.provider, "other-model")).context?.contextWindow).toBe(250000);
    delete firstBackend.a.launch.model_configs["other-model"];
    await first.receive("removed", { content: "continue" });
    expect(first.runtime.agent.session.requestHeader()?.config.maxTokens).toBeUndefined();
    expect((await ctx.llm.resolveModelInfo(first.config.provider, "other-model")).context).toBeUndefined();
    await Promise.all([first.close(), second.close()]);
  } finally { await ctx.fiber.dispose(); firstBackend.stop(); secondBackend.stop(); }
});

test("child requests and restored sessions use their selected model config", async () => {
  const backend = new Backend(); await backend.ready;
  backend.a.launch.model_configs = { "test-model": { context_window: 200000, max_tokens: 32000 }, "child-model": { context_window: 128000, max_tokens: 8000 } };
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
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, backend.a);
    backend.onDelegationApproval = async payload => { await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } }); };
    await session.open(); await session.receive("delegate", { content: "delegate" });
    expect(backend.requests.filter(request => request.model === "test-model").every(request => request.max_tokens === 32000)).toBe(true);
    expect(backend.requests.filter(request => request.model === "child-model").map(request => request.max_tokens)).toEqual([8000]);
    await session.close(); await ctx.fiber.dispose();
    backend.a.launch.model_configs["test-model"] = { context_window: 300000, max_tokens: 16000 };
    ctx = new Context(); await mountOma(ctx, api);
    const resumed = createOmaSession(ctx, api, backend.a); await resumed.open();
    await resumed.receive("resumed", { content: "continue after restart" });
    expect(backend.requests.at(-1)).toMatchObject({ model: "test-model", max_tokens: 16000 });
    await resumed.close();
  } finally { await ctx.fiber.dispose(); backend.stop(); }
});
