import { expect, test } from "bun:test";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import * as JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function completion(index: number, tool: boolean) {
  const delta = tool ? { tool_calls: [{ index: 0, id: "call_chat_runtime", type: "function",
    function: { name: "Bash", arguments: '{"command":"pwd"}' } }] } : { content: "finished" };
  return new Response(`data: ${JSON.stringify({ id: `chat_runtime_${index}`, choices: [
    { index: 0, delta, finish_reason: tool ? "tool_calls" : "stop" },
  ] })}\n\ndata: [DONE]\n\n`, { headers: { "Content-Type": "text/event-stream", "Request-Id": `span_chat_${index}` } });
}

test("native Brain runs a Chat tool round-trip and restores its protocol history", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.models = { "test-model": { id: "test-model", api_backend: "chat_completions" } };
  const original = backend.route.bind(backend);
  const requests: Payload[] = [];
  backend.route = async (request) => {
    if (new URL(request.url).pathname !== "/v1/chat/completions") return original(request);
    requests.push(await request.json());
    expect(request.headers.get("Authorization")).toBe(`Bearer ${backend.a.model_token}`);
    return completion(requests.length, requests.length === 1);
  };
  const localRoot = await mkdtemp(join(tmpdir(), "brain-chat-transcript-"));
  let context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    const initialApi = new OmaApi(backend.url);
    await mountOma(context, initialApi, {}, localRoot);
    session = createOmaSession(context, initialApi, backend.a);
    await session.open();
    backend.onApproval = async (request) => {
      await session!.control({ type: "control_response", response: { subtype: "success", request_id: request.request_id,
        response: { behavior: "allow" } } });
    };
    await session.receive("chat_turn", { content: "run pwd" });
    expect(requests).toHaveLength(2);
    expect(backend.executions).toHaveLength(1);
    expect(requests[1]!.messages.some((message: Payload) => message.role === "tool" && message.tool_call_id === "call_chat_runtime")).toBe(true);
    expect(requests[1]!.messages.some((message: Payload) => message.role === "assistant" && message.tool_calls?.[0]?.id === "call_chat_runtime")).toBe(true);
    await session.close();
    await context.fiber.dispose();
    context = new Context();
    const api = new OmaApi(backend.url);
    await mountOma(context, api, {}, localRoot);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("chat_restored_turn", { content: "continue" });
    const replicas = await readdir(localRoot);
    expect(replicas).toHaveLength(1);
    {
      const local = new Context();
      try {
        await local.plugin(JsonlPersistence, { root: localRoot, compression: "none" });
        const snapshots = await local.sessionPersistence.list();
        expect(snapshots).toHaveLength(1);
        const log = await local.sessionPersistence.open(snapshots[0]!.header.id, "read");
        expect((await log.read()).events.some((event) => event.type === "tool/result")).toBe(true);
        await log.close();
      } finally {
        await local.fiber.dispose();
      }
    }
    expect(requests).toHaveLength(3);
    expect(backend.executions).toHaveLength(1);
    expect(requests[2]!.messages.filter((message: Payload) => message.role === "tool" && message.tool_call_id === "call_chat_runtime")).toHaveLength(1);
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
    await rm(localRoot, { recursive: true, force: true });
  }
});
