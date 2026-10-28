import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC";
const cleanups: (() => void | Promise<unknown>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

function readCalls(count: number): Payload[] {
  const events = toolEvents();
  return [events[0]!, ...Array.from({ length: count }, (_, index) => [
    { type: "content_block_start", index, content_block: { type: "tool_use", id: `read_${index}`, name: "Read", input: {} } },
    { type: "content_block_delta", index, delta: { type: "input_json_delta", partial_json: JSON.stringify({ file_path: `/workspace/image_${index}.png` }) } },
    { type: "content_block_stop", index },
  ]).flat(), ...events.slice(-2)];
}

async function fixture(backendType: "messages" | "chat_completions" = "messages", calls = 1) {
  const backend = new Backend();
  await backend.ready;
  cleanups.push(() => backend.stop());
  backend.a.launch.models!["test-model"] = { id: "test-model", api_backend: backendType, input_modalities: ["text", "image"] };
  backend.a.launch.tool_permissions = { read: "allow" };
  backend.onTool = async () => ({ content: [
    { type: "text", text: "before image" }, { type: "image", mimeType: "image/png", data: png }, { type: "text", text: "after image" },
  ] });
  const requests: Payload[] = backendType === "messages" ? backend.requests : [];
  if (backendType === "messages") backend.onModel = async () => requests.length === 1 ? readCalls(calls) : textEvents("image inspected");
  else {
    const original = backend.route.bind(backend);
    backend.route = async request => {
      if (new URL(request.url).pathname !== "/v1/chat/completions") return original(request);
      requests.push(await request.json());
      const first = requests.length === 1;
      const delta = first ? { tool_calls: Array.from({ length: calls }, (_, index) => ({ index, id: `read_${index}`, type: "function",
        function: { name: "Read", arguments: JSON.stringify({ file_path: `/workspace/image_${index}.png` }) } })) } : { content: "image inspected" };
      return new Response(`data: ${JSON.stringify({ id: `chat_read_${requests.length}`, choices: [{ index: 0, delta, finish_reason: first ? "tool_calls" : "stop" }] })}\n\ndata: [DONE]\n\n`,
        { headers: { "content-type": "text/event-stream" } });
    };
  }
  const open = async () => {
    const home = await mkdtemp(join(tmpdir(), "oma-read-images-"));
    cleanups.push(() => rm(home, { recursive: true, force: true }));
    const ctx = new Context();
    const api = new OmaApi(backend.url);
    await mountOma(ctx, api, {}, undefined, join(home, "attachments", "v1"));
    const session = createOmaSession(ctx, api, backend.a);
    await session.open();
    cleanups.push(async () => { await session.close(); await ctx.fiber.dispose(); });
    return { session, ctx };
  };
  const first = await open();
  return { backend, requests, open, ...first };
}

function toolResults(request: Payload, backendType: string): Payload[] {
  return backendType === "messages" ? request.messages.flatMap((message: Payload) => Array.isArray(message.content)
    ? message.content.filter((part: Payload) => part.type === "tool_result") : [])
    : request.messages.filter((message: Payload) => message.role === "tool");
}

test.each(["invalid image", "storage unavailable"])("Read %s produces a tool error without leaking bytes or repeating execution", async failure => {
  const f = await fixture();
  if (failure === "invalid image") f.backend.onTool = async () => ({ content: [{ type: "image", mimeType: "image/png", data: Buffer.from("bad image bytes").toString("base64") }] });
  else f.backend.imageStatus = 503;
  await f.session.receive("bad-read", { content: "Read /workspace/chart.png" });
  expect(f.requests).toHaveLength(2);
  expect(f.backend.executions).toHaveLength(1);
  expect(f.backend.imageObjects.size).toBe(0);
  expect(toolResults(f.requests[1]!, "messages")[0]!.is_error).toBe(true);
  expect(JSON.stringify([...f.backend.privateLog.values()])).not.toContain(png);
  expect(f.backend.publicLog.get("bad-read_result")?.is_error).toBe(false);
});

test("Read image with a text-only model returns a tool error before attachment storage", async () => {
  const f = await fixture();
  f.backend.a.launch.models!["test-model"]!.input_modalities = ["text"];
  await f.session.receive("text-route", { content: "Read /workspace/chart.png" });
  expect(toolResults(f.requests[1]!, "messages")[0]!.is_error).toBe(true);
  expect(JSON.stringify(toolResults(f.requests[1]!, "messages"))).toContain("image");
  expect(f.backend.imageObjects.size).toBe(0);
});

test("plain text Read retains rawOutput and its existing wire text", async () => {
  const f = await fixture();
  const text = " exact text\nwith whitespace ";
  f.backend.onTool = async () => ({ structuredContent: { rawOutput: text }, content: [{ type: "text", text: "formatted metadata" }] });
  await f.session.receive("text-read", { content: "Read /workspace/file.txt" });
  expect(toolResults(f.requests[1]!, "messages")[0]!.content).toBe(text);
  expect(f.backend.imageObjects.size).toBe(0);
  expect(f.backend.executions).toHaveLength(1);
});

test("denied Read never executes or saves an image", async () => {
  const f = await fixture();
  f.backend.a.launch.tool_permissions = {};
  f.backend.onApproval = async payload => {
    await f.session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id,
      response: { behavior: "deny", message: "Read denied" } } });
  };
  await f.session.receive("denied-read", { content: "Read /workspace/chart.png" });
  expect(f.backend.executions).toHaveLength(0);
  expect(f.backend.imageObjects.size).toBe(0);
  expect(toolResults(f.requests[1]!, "messages")[0]!.is_error).toBe(true);
});

for (const backendType of ["messages", "chat_completions"] as const) {
  test(`${backendType} Read persists native image output and sends it to the next model request`, async () => {
    const f = await fixture(backendType);
    await f.session.receive("read-image", { content: "Read /workspace/chart.png" });
    expect(f.requests).toHaveLength(2);
    const native = f.session.runtime.agent.session.snapshotEvents().find(event => event.type === "tool/result");
    expect(native?.type).toBe("tool/result");
    if (native?.type !== "tool/result") throw new Error("Missing tool result");
    expect(native.data.message.isError).toBe(false);
    expect(native.data.message.content.map(block => block.type)).toEqual(["text", "image", "text"]);
    expect(f.backend.imageObjects.size).toBe(1);
    expect(f.backend.executions[0]!.name).toBe("read");
    const result = toolResults(f.requests[1]!, backendType)[0]!;
    if (backendType === "messages") {
      expect(result.tool_use_id).toBe("read_0");
      expect(result.content.map((block: Payload) => block.type)).toEqual(["text", "image", "text"]);
      expect(result.content[1].source.type).toBe("base64");
    } else {
      expect(result.tool_call_id).toBe("read_0");
      expect(typeof result.content).toBe("string");
      const attached = f.requests[1]!.messages.find((message: Payload) => message.role === "user" && Array.isArray(message.content)
        && message.content.some((block: Payload) => block.type === "image_url"));
      expect(attached).toBeDefined();
      expect(JSON.stringify(attached)).toContain("read_0");
    }
    expect(JSON.stringify([...f.backend.privateLog.values()])).not.toContain(png);
    expect(JSON.stringify([...f.backend.publicLog.values()])).not.toContain(png);
    expect(f.backend.publicLog.get("read-image_result")?.is_error).toBe(false);
    await f.session.close();
    const recovered = await f.open();
    await recovered.session.receive("remember-read", { content: "Describe the same image again" });
    expect(f.backend.executions).toHaveLength(1);
    expect(f.backend.imageReads).toBeGreaterThan(0);
    const restored = JSON.stringify(f.requests[2]);
    expect(restored).toContain(backendType === "messages" ? '"type":"image"' : '"type":"image_url"');
    expect(f.backend.publicLog.get("remember-read_result")?.is_error).toBe(false);
  });
}

test("Chat places image input after every result in a parallel Read batch", async () => {
  const f = await fixture("chat_completions", 2);
  await f.session.receive("parallel-read", { content: "Read both images" });
  expect(f.requests).toHaveLength(2);
  const messages: Payload[] = f.requests[1]!.messages;
  const results = messages.flatMap((message, index) => message.role === "tool" ? [index] : []);
  expect(results).toHaveLength(2);
  const images = messages.findIndex(message => message.role === "user" && Array.isArray(message.content)
    && message.content.some((part: Payload) => part.type === "image_url"));
  expect(images).toBeGreaterThan(results[1]!);
  expect(messages[images]!.content.filter((part: Payload) => part.type === "image_url")).toHaveLength(2);
});

test("Read tool images participate in native image offload", async () => {
  const f = await fixture("messages", 2);
  f.backend.a.launch.models!["test-model"]!.image_input = { max_images: 1 };
  await f.session.receive("offload-read", { content: "Read both images" });
  expect(f.requests).toHaveLength(2);
  const results = toolResults(f.requests[1]!, "messages");
  expect(results).toHaveLength(2);
  expect(results.flatMap(result => result.content).filter((part: Payload) => part.type === "image")).toHaveLength(1);
  expect(f.session.runtime.agent.session.snapshotEvents().some(event => String(event.type) === "image/offload")).toBe(true);
  expect(f.backend.executions).toHaveLength(2);
});

test.each([500, 502, 503, 504])("Read retries temporary image admission HTTP %i without repeating MCP", async status => {
  const f = await fixture();
  const original = f.backend.route.bind(f.backend);
  let puts = 0;
  f.backend.route = request => request.method === "PUT" && new URL(request.url).pathname.includes("/image-attachments/") && ++puts === 1
    ? Promise.resolve(new Response(null, { status })) : original(request);
  await f.session.receive("recover-read", { content: "Read /workspace/chart.png" });
  expect(puts).toBe(2);
  expect(f.backend.executions).toHaveLength(1);
  expect(toolResults(f.requests[1]!, "messages")[0]!.is_error).not.toBe(true);
  expect(toolResults(f.requests[1]!, "messages")[0]!.content.some((block: Payload) => block.type === "image")).toBe(true);
});

test.each([503, 409])("Read retains the recovery meaning of persistent HTTP %i", async status => {
  const f = await fixture();
  f.backend.imageStatus = status;
  let errorCode: string | undefined;
  f.ctx.on("tools/result", (_exec, result) => { errorCode = result.error?.info?.code; return undefined; });
  const original = f.backend.route.bind(f.backend);
  let puts = 0;
  f.backend.route = request => {
    if (request.method === "PUT" && new URL(request.url).pathname.includes("/image-attachments/")) puts++;
    return original(request);
  };
  await f.session.receive("failed-read", { content: "Read /workspace/chart.png" });
  const result = JSON.stringify(toolResults(f.requests[1]!, "messages"));
  expect(result).not.toContain("unsupported result");
  expect(result).toContain(status === 409 ? "reassignment" : "retry");
  expect(puts).toBe(status === 409 ? 1 : 3);
  expect(errorCode).toBe("RETRYABLE_INPUT");
  expect(f.backend.executions).toHaveLength(1);
});
