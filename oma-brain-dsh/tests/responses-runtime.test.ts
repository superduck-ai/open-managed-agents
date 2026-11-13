import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function completion(index: number, tool: boolean) {
  const id = `resp_${index}`;
  const item = tool ? { id: `fc_${index}`, type: "function_call", call_id: "call_responses", name: "Bash", arguments: '{"command":"pwd"}', status: "completed" }
    : { id: `msg_${index}`, type: "message", role: "assistant", status: "completed", content: [{ type: "output_text", text: "Responses 恢复成功", annotations: [] }] };
  const response = { id, object: "response", model: "test-model", created_at: 1, status: "completed", output: [item], usage: { input_tokens: 20, output_tokens: 10, input_tokens_details: { cached_tokens: 5 } }, gateway: { amount: "7.2" } };
  const events = [
    { type: "response.created", response },
    { type: "response.output_item.added", output_index: 0, item: tool ? { ...item, arguments: "" } : item },
    tool ? { type: "response.function_call_arguments.delta", item_id: item.id, output_index: 0, delta: '{"command":"pwd"}' }
      : { type: "response.output_text.delta", item_id: item.id, output_index: 0, content_index: 0, delta: "Responses 恢复成功" },
    { type: "response.output_item.done", output_index: 0, item },
    { type: "response.completed", response },
  ];
  return new Response(events.map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream", "Request-Id": `span_responses_${index}` } });
}

test("Responses 经过原生 Brain 工具循环并保留重放身份", async () => {
  const backend = new Backend(); await backend.ready;
  backend.a.launch.models = { "test-model": { id: "test-model", api_backend: "responses" } };
  const route = backend.route.bind(backend); const requests: Payload[] = [];
  backend.route = async request => {
    if (new URL(request.url).pathname !== "/v1/responses") return route(request);
    const body = await request.json() as Payload; requests.push(body);
    expect(request.headers.get("Authorization")).toBe(`Bearer ${backend.a.model_token}`);
    expect(request.headers.get("X-Api-Key")).toBeNull();
    expect(body.stream).toBe(true); expect(body.store).toBe(false);
    return completion(requests.length, requests.length === 1);
  };
  const ctx = new Context(); const api = new OmaApi(backend.url);
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    await mountOma(ctx, api); session = createOmaSession(ctx, api, backend.a); await session.open();
    backend.onApproval = async request => { await session!.control({ type: "control_response", response: { subtype: "success", request_id: request.request_id, response: { behavior: "allow" } } }); };
    await session.receive("responses_turn", { content: "run pwd" });
    expect(requests).toHaveLength(2); expect(backend.executions).toHaveLength(1);
    expect(requests[1]!.input.some((item: Payload) => item.type === "function_call_output" && item.call_id === "call_responses")).toBe(true);
    expect(backend.publicLog.get("responses_turn_result")?.is_error).toBe(false);
    expect([...backend.publicLog.values()].some(event => event.type === "agent.message" && event.content?.some((block: Payload) => block.text === "Responses 恢复成功"))).toBe(true);
  } finally { await session?.close(); await ctx.fiber.dispose(); backend.stop(); }
});
