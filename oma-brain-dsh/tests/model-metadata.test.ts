import { expect, test } from "bun:test";
import { AttachmentId } from "@deepseek-ai/dsh-attachment";
import { SessionId } from "@deepseek-ai/dsh-session";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { OmaApi } from "../src/api.ts";
import { assignment, textEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

test("unconfigured model limits use the unified catalog defaults", async () => {
  const api = new OmaApi("http://model.test");
  const task = assignment();
  api.assignments.set(task.session_id, task);
  const config = omaModelOptions(api);
  const adapter = new AiSdkAdapter(config);
  expect(await adapter.resolveModel("oma", "test-model")).toEqual({ provider: "oma", id: "test-model", name: "test-model", inputModalities: ["text"], context: { contextWindow: 262144 } });
  expect(config.route({ provider: "oma", model: "test-model", sessionId: SessionId(task.session_id), messages: [] }).maxOutputTokens).toBe(32768);
});

test.each(["messages", "chat_completions"] as const)("injected model metadata and actual %s request share per-model limits and explicit overrides", async backend => {
  const bodies: Payload[] = [];
  const api = new OmaApi("http://model.test", (async (_input, init) => {
    bodies.push(JSON.parse(String(init!.body)));
    if (backend === "chat_completions") return new Response('data: {"id":"reply","choices":[{"delta":{"content":"done"},"finish_reason":null}]}\n\ndata: {"choices":[{"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n', { headers: { "Content-Type": "text/event-stream" } });
    return new Response(textEvents("done").map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch);
  const task = assignment();
  task.launch.models = {
    "test-model": { id: "test-model", api_backend: backend, context_window: 200000, max_output_tokens: 32000 },
    "larger-model": { id: "larger-model", api_backend: backend, context_window: 400000, max_output_tokens: 64000 },
  };
  api.assignments.set(task.session_id, task);
  const adapter = new AiSdkAdapter(omaModelOptions(api, undefined, () => task.session_id));
  expect(await adapter.resolveModel("oma", "test-model")).toMatchObject({ context: { contextWindow: 200000 }, defaultMaxTokens: 32000 });
  expect(await adapter.resolveModel("oma", "larger-model")).toMatchObject({ context: { contextWindow: 400000 }, defaultMaxTokens: 64000 });
  for (const [model, maxTokens] of [["test-model", undefined], ["larger-model", undefined], ["larger-model", 1234]] as const) {
    for await (const _chunk of adapter.stream({ provider: "oma", model, sessionId: SessionId(task.session_id), maxTokens,
      messages: [{ role: "user", content: [{ type: "text", text: "hello" }] }] })) {}
  }
  const field = backend === "messages" ? "max_tokens" : "max_completion_tokens";
  expect(bodies.map(body => body[field])).toEqual([32000, 64000, 1234]);
});

test("auxiliary compaction requests preserve the business response identity", async () => {
  const api = new OmaApi("http://model.test", (async (_input, _init) => {
    const events = textEvents("summary");
    events[0]!.message.id = "summary-response";
    return new Response(events.map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch);
  const task = assignment();
  api.assignments.set(task.session_id, task);
  api.modelResponses.set(task.session_id, "business-response");
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(task.session_id),
    purpose: "compaction", maxTokens: 65536, messages: [{ role: "user", content: [{ type: "text", text: "summarize" }] }] })) {}
  expect(api.modelResponses.get(task.session_id)).toBe("business-response");
});

test("offloaded images become DSH placeholders without reading attachment bytes", async () => {
  let body: Payload = {};
  const api = new OmaApi("http://model.test", (async (_input, init) => {
    body = JSON.parse(String(init!.body));
    return new Response(textEvents("done").map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch);
  const task = assignment();
  api.assignments.set(task.session_id, task);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(task.session_id),
    messages: [{ role: "user", content: [{ type: "image", offloaded: true, attachment: {
      attachmentId: AttachmentId("offloaded-test"), mediaType: "image/png", bytes: 100, width: 1, height: 1,
    } }] }] })) {}
  expect(body.messages[0].content[0].type).toBe("text");
  expect(body.messages[0].content[0].text).toContain("offloaded");
});
