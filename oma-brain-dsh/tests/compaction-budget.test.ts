import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import type { StreamChunk } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { assignment, Backend, textEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

type ModelBackend = "messages" | "chat_completions";

function modelResponse(backend: ModelBackend, text: string): Response {
  const data = backend === "messages"
    ? textEvents(text).map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join("")
    : [
      { id: "budget-response", choices: [{ delta: { content: text }, finish_reason: null }] },
      { id: "budget-response", choices: [{ delta: {}, finish_reason: "stop" }] },
      "[DONE]",
    ].map(event => `data: ${typeof event === "string" ? event : JSON.stringify(event)}\n\n`).join("");
  return new Response(data, { headers: { "Content-Type": "text/event-stream" } });
}

function rejectedBudget(requested: number, limit: number): Response {
  return Response.json({ type: "error", error: {
    type: "invalid_request_error", message: `Requested output budget ${requested} exceeds model limit ${limit}`,
  } }, { status: 400 });
}

const budgetCases = [
  { name: "8k model", limit: 8192, requested: 65536, expected: 8192 },
  { name: "32k model", limit: 32768, requested: 65536, expected: 32768 },
  { name: "64k model", limit: 64000, requested: 65536, expected: 64000 },
  { name: "default limit", limit: undefined, requested: 65536, expected: 32768 },
  { name: "smaller explicit budget", limit: 8192, requested: 1234, expected: 1234 },
  { name: "larger model limit", limit: 131072, requested: 65536, expected: 65536 },
] as const;

for (const backend of ["messages", "chat_completions"] as const) {
  for (const child of [false, true]) {
    test.each(budgetCases)(`compaction summary budget ${backend} ${child ? "child" : "root"}: $name`, async scenario => {
      const field = backend === "messages" ? "max_tokens" : "max_completion_tokens";
      const task = assignment();
      const model = child ? "child-model" : "test-model";
      const sessionId = child ? "budget-child" : task.session_id;
      const provider = child ? "oma" : `oma:${task.session_id}`;
      task.launch.models = {
        "test-model": { id: "test-model", api_backend: backend, context_window: 400000, max_output_tokens: 131072 },
        [model]: { id: model, api_backend: backend, context_window: 400000,
          ...(scenario.limit === undefined ? {} : { max_output_tokens: scenario.limit }) },
      };
      const bodies: Payload[] = [];
      const api = new OmaApi("http://budget.test", (async (input, init) => {
        const request = new Request(input, init);
        expect(request.headers.has("X-OMA-Model-Purpose")).toBe(false);
        expect(new URL(request.url).pathname).toBe(backend === "messages" ? "/v1/messages" : "/v1/chat/completions");
        expect(request.headers.get("X-Claude-Code-Agent-Id")).toBe(child ? sessionId : null);
        const body = await request.json();
        bodies.push(body);
        const limit = scenario.limit ?? 32768;
        return body[field] > limit ? rejectedBudget(body[field], limit) : modelResponse(backend, "summary");
      }) as typeof fetch);
      api.assignments.set(task.session_id, task);
      if (child) api.modelSessions.set(sessionId, task.session_id);
      api.modelResponses.set(sessionId, "business-response");
      const adapter = new AiSdkAdapter(omaModelOptions(api, undefined, () => task.session_id));
      const chunks: StreamChunk[] = [];
      let failure: unknown;
      try {
        for await (const chunk of adapter.stream({ provider, model, sessionId: SessionId(sessionId),
          purpose: "compaction", maxTokens: scenario.requested,
          messages: [{ role: "user", content: [{ type: "text", text: "summarize" }] }] })) chunks.push(chunk);
      } catch (error) { failure = error; }
      expect(bodies).toHaveLength(1);
      expect(bodies[0]![field]).toBe(scenario.expected);
      expect(failure).toBeUndefined();
      expect(chunks).toContainEqual({ type: "text-delta", index: 0, text: "summary" });
      expect(api.modelResponses.get(sessionId)).toBe("business-response");
    });
  }

  test(`ordinary ${backend} requests retain explicit output-budget precedence`, async () => {
    const task = assignment();
    task.launch.models = {
      "test-model": { id: "test-model", api_backend: backend, context_window: 200000, max_output_tokens: 8192 },
    };
    const bodies: Payload[] = [];
    const api = new OmaApi("http://budget.test", (async (_input, init) => {
      expect(new Headers(init!.headers).has("X-OMA-Model-Purpose")).toBe(false);
      bodies.push(JSON.parse(String(init!.body)));
      return modelResponse(backend, "ordinary reply");
    }) as typeof fetch);
    api.assignments.set(task.session_id, task);
    const adapter = new AiSdkAdapter(omaModelOptions(api, undefined, () => task.session_id));
    for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(task.session_id),
      maxTokens: 16384, messages: [{ role: "user", content: [{ type: "text", text: "hello" }] }] })) {}
    expect(bodies).toHaveLength(1);
    expect(bodies[0]![backend === "messages" ? "max_tokens" : "max_completion_tokens"]).toBe(16384);
  });

  test(`native compaction summary budget ${backend} fits the selected model output limit`, async () => {
    const backendFixture = new Backend();
    await backendFixture.ready;
    backendFixture.a.launch.tools = [];
    backendFixture.a.launch.models = {
      "test-model": { id: "test-model", api_backend: backend, context_window: 200000, max_output_tokens: 8192 },
    };
    const field = backend === "messages" ? "max_tokens" : "max_completion_tokens";
    const bodies: Payload[] = [];
    const api = new OmaApi(backendFixture.url, (async (input, init) => {
      const request = new Request(input, init);
      const path = new URL(request.url).pathname;
      if (path !== "/v1/messages" && path !== "/v1/chat/completions") return backendFixture.route(request);
      const body = await request.json();
      bodies.push(body);
      if (body[field] > 8192) return rejectedBudget(body[field], 8192);
      return modelResponse(backend, bodies.length === 1 ? "a".repeat(12000) : "Previous task details.");
    }) as typeof fetch);
    const ctx = new Context();
    try {
      await mountOma(ctx, api);
      const session = createOmaSession(ctx, api, backendFixture.a);
      await session.open();
      await session.receive("first", { content: "u".repeat(12000) });
      expect(bodies).toHaveLength(1);
      expect(bodies[0]![field]).toBe(8192);
      let failure: unknown;
      let result: unknown;
      try {
        result = await ctx.compaction.compactNow(session.runtime.agent, new AbortController().signal);
      } catch (error) { failure = error; }
      expect(bodies.length).toBeGreaterThan(1);
      expect(bodies.at(-1)![field]).toBe(8192);
      expect(failure).toBeUndefined();
      expect(result).toBeTruthy();
      const events = session.runtime.agent.session.snapshotEvents();
      expect(events.filter(event => event.type === "compaction/summary")).toHaveLength(1);
      expect(events.filter(event => event.type === "compaction/end").every(event => event.data.error === undefined)).toBe(true);
    } finally { await ctx.fiber.dispose(); backendFixture.stop(); }
  });
}
