import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { AttachmentId, ImageVariantId, type AttachmentStore, type ImageAttachmentRef, type ImageRequestTarget } from "@deepseek-ai/dsh-attachment";
import { MessageId, ToolCallId, type ContentBlock, type GenerateOptions, type AssistantMessage, type ToolResultMessage } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { callOptions } from "../src/llm-ai-sdk/prompt.ts";
import { omaRoute } from "../src/model-provider.ts";
import { Backend, assignment, textEvents, toolEvents } from "./backend.ts";
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

function projectionImage(hex: string, offloaded = false): Extract<ContentBlock, { type: "image" }> {
  return { type: "image", attachment: { attachmentId: AttachmentId(`sha256:${hex.repeat(64)}`),
    mediaType: "image/png", bytes: 1, width: 1, height: 1 }, ...(offloaded ? { offloaded: true } : {}) };
}

function projectionCalls(ids: string[]): AssistantMessage {
  return { id: MessageId(`assistant_${ids.join("_")}`), role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
    content: ids.map(id => ({ type: "tool-call", id: ToolCallId(id), name: "Read", arguments: "{}" })) };
}

function projectionResult(id: string, content: ContentBlock[], isError = false): ToolResultMessage {
  return { id: MessageId(`result_${id}`), role: "tool", toolCallId: ToolCallId(id),
    source: { kind: "tool", callId: ToolCallId(id) }, content, isError };
}

async function projectToolMessages(backendType: "messages" | "chat_completions" | "default",
  messages: GenerateOptions["messages"], maxImages = 20) {
  const requests: Payload[] = [];
  const paths: string[] = [];
  const reads: string[] = [];
  const api = new OmaApi("http://projection.test", (async (input, init) => {
    paths.push(new URL(String(input)).pathname);
    requests.push(JSON.parse(String(init?.body)));
    return backendType === "chat_completions"
      ? new Response('data: {"id":"chat_projection","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n',
        { headers: { "content-type": "text/event-stream" } })
      : new Response(textEvents("done").map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""),
        { headers: { "content-type": "text/event-stream" } });
  }) as typeof fetch);
  const a = assignment();
  if (backendType === "default") delete a.launch.models;
  else a.launch.models!["test-model"] = { id: "test-model", api_backend: backendType, image_input: { max_images: maxImages } };
  api.assignments.set(a.session_id, a);
  const attachments = { async readImageRequest(ref: ImageAttachmentRef, _target: ImageRequestTarget) {
    reads.push(ref.attachmentId);
    return { attachment: ref, variantId: ImageVariantId(`variant_${ref.attachmentId}`), data: new Uint8Array([ref.attachmentId.charCodeAt(7)]),
      mediaType: ref.mediaType, bytes: 1, width: 1, height: 1, depth: "uchar" as const, space: "srgb" as const, hasAlpha: false };
  } } as AttachmentStore;
  const snapshot = structuredClone(messages);
  const options: GenerateOptions = { provider: "oma", model: "test-model", sessionId: SessionId(a.session_id), messages };
  const route = omaRoute(api, options);
  const call = await callOptions(options, route, attachments);
  const response = await route.model.doStream(call);
  await response.stream.cancel();
  expect(messages).toEqual(snapshot);
  expect(requests).toHaveLength(1);
  return { body: requests[0]!, paths, reads, prompt: call.prompt };
}

function projectionImageParts(request: Payload): Payload[] {
  return request.messages.flatMap((message: Payload) => Array.isArray(message.content)
    ? message.content.flatMap((part: Payload) => part.type === "tool_result" ? (Array.isArray(part.content) ? part.content : []) : [part]) : [])
    .filter((part: Payload) => part.type === "image" || part.type === "image_url");
}

test.each(["messages", "chat_completions"] as const)("%s error tool results do not send images or synthetic user messages", async backendType => {
  const f = await projectToolMessages(backendType, [projectionCalls(["read_error"]),
    projectionResult("read_error", [{ type: "text", text: "Read failed" }, projectionImage("a")], true)]);
  expect(toolResults(f.body, backendType)[0]).toMatchObject(backendType === "messages"
    ? { tool_use_id: "read_error", is_error: true, content: "Read failed" }
    : { tool_call_id: "read_error", content: "Read failed" });
  expect(projectionImageParts(f.body)).toEqual([]);
  expect(f.prompt.filter(message => message.role === "user")).toEqual([]);
  const result = f.prompt.find(message => message.role === "tool");
  expect(result?.content[0]).toMatchObject({ output: { type: "error-text", value: "Read failed" } });
});

test("unknown model backends reject projection with the configuration reload error", () => {
  const api = new OmaApi("http://projection.test", (async (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> => { throw new Error("Invalid routes must not request a model"); }) as typeof fetch);
  const a = assignment();
  Object.assign(a.launch.models!["test-model"]!, { api_backend: "unknown" });
  api.assignments.set(a.session_id, a);
  let failure: unknown;
  try { omaRoute(api, { provider: "oma", model: "test-model", sessionId: SessionId(a.session_id), messages: [] }); }
  catch (error) { failure = error; }
  expect(failure).toMatchObject({ code: "INVALID_ARGS", message: "Model API backend is unavailable; reload the session configuration" });
});

test.each(["messages", "chat_completions"] as const)("%s excludes error-result images from attachment reads and image budgets", async backendType => {
  const retained = projectionImage("c");
  const f = await projectToolMessages(backendType, [projectionCalls(["read_error", "read_ok"]),
    projectionResult("read_error", [{ type: "text", text: "Read failed" }, projectionImage("a"), projectionImage("b")], true),
    projectionResult("read_ok", [retained])], 1);
  expect(f.reads).toEqual([retained.attachment.attachmentId]);
  expect(projectionImageParts(f.body)).toHaveLength(1);
  expect(JSON.stringify(f.body)).not.toContain("Image output for tool call read_error:");
  const result = f.prompt.flatMap(message => message.role === "tool" ? message.content : [])
    .filter(part => part.type === "tool-result").find(part => part.toolCallId === "read_error");
  expect(result?.output).toEqual({ type: "error-text", value: "Read failed" });
});

test.each([false, true])("Chat omits offloaded image-only calls from image user messages (retained=%s)", async retained => {
  const omitted = projectionImage("a", true);
  const ids = retained ? ["read_omitted", "read_kept"] : ["read_omitted"];
  const f = await projectToolMessages("chat_completions", [projectionCalls(ids), projectionResult("read_omitted", [omitted]),
    ...(retained ? [projectionResult("read_kept", [projectionImage("b")])] : [])], 1);
  const results = toolResults(f.body, "chat_completions");
  expect(results[0]!.content).toContain(`[Image ${omitted.attachment.attachmentId}, 1x1: omitted`);
  expect(projectionImageParts(f.body)).toHaveLength(retained ? 1 : 0);
  const users = f.body.messages.filter((message: Payload) => message.role === "user");
  expect(users).toHaveLength(retained ? 1 : 0);
  expect(JSON.stringify(users)).not.toContain("Image output for tool call read_omitted:");
  if (retained) expect(users[0].content[0].text).toBe("Image output for tool call read_kept:");
  expect(f.reads).toEqual(retained ? [projectionImage("b").attachment.attachmentId] : []);
});

test.each(["messages", "chat_completions"] as const)("%s Read errors discard MCP images before writing native tool results", async backendType => {
  const f = await fixture(backendType);
  f.backend.onTool = async () => ({ isError: true, content: [{ type: "text", text: "MCP read failed" }, { type: "image", mimeType: "image/png", data: png }] });
  await f.session.receive("error-image-read", { content: "Read the image" });
  expect(projectionImageParts(f.requests[1]!)).toEqual([]);
  expect(f.backend.imageObjects.size).toBe(0);
  const result = f.session.agent.session.snapshotEvents().find(event => event.type === "tool/result");
  if (result?.type !== "tool/result") throw new Error("Missing tool result");
  expect(result.data.message.isError).toBe(true);
  expect(result.data.message.content.map(block => block.type)).toEqual(["text"]);
});

test("Messages replaces every offloaded tool image with a text placeholder without reading bytes", async () => {
  const image = projectionImage("a", true);
  const f = await projectToolMessages("messages", [projectionCalls(["read_omitted"]), projectionResult("read_omitted", [image])], 1);
  expect(projectionImageParts(f.body)).toEqual([]);
  expect(f.reads).toEqual([]);
  expect(toolResults(f.body, "messages")[0]).toMatchObject({ tool_use_id: "read_omitted", content: expect.stringContaining(`[Image ${image.attachment.attachmentId}, 1x1: omitted`) });
});

test("Chat native Read offload keeps placeholders in tools and promotes only retained images", async () => {
  const f = await fixture("chat_completions", 2);
  f.backend.a.launch.models!["test-model"]!.image_input = { max_images: 1 };
  await f.session.receive("chat-offload", { content: "Read both images" });
  expect(f.requests).toHaveLength(2);
  const results = toolResults(f.requests[1]!, "chat_completions");
  expect(results).toHaveLength(2);
  const omitted = results.filter(result => result.content.includes("[Image sha256:"));
  expect(omitted).toHaveLength(1);
  expect(omitted[0]!.content).toContain("1x1: omitted");
  expect(projectionImageParts(f.requests[1]!)).toHaveLength(1);
  const images = f.requests[1]!.messages.filter((message: Payload) => message.role === "user" && Array.isArray(message.content)
    && message.content.some((part: Payload) => part.type === "image_url"));
  expect(images).toHaveLength(1);
  expect(images[0].content[0].text).toBe(`Image output for tool call ${results.find(result => result !== omitted[0])!.tool_call_id}:`);
  expect(JSON.stringify(images)).not.toContain(`Image output for tool call ${omitted[0]!.tool_call_id}:`);
  const native = f.session.agent.session.snapshotEvents();
  expect(native.some(event => String(event.type) === "image/offload")).toBe(true);
  expect(native.flatMap(event => event.type === "tool/result" ? event.data.message.content.filter(block => block.type === "image") : [])).toHaveLength(2);
});

test("Chat flushes each image batch before the following assistant and clears it for the next round", async () => {
  const f = await projectToolMessages("chat_completions", [projectionCalls(["read_first"]), projectionResult("read_first", [projectionImage("a")]),
    projectionCalls(["read_second"]), projectionResult("read_second", [projectionImage("b")])]);
  expect(f.body.messages.map((message: Payload) => message.role)).toEqual(["assistant", "tool", "user", "assistant", "tool", "user"]);
  const users = f.body.messages.filter((message: Payload) => message.role === "user");
  expect(users.map((message: Payload) => message.content[0].text)).toEqual(["Image output for tool call read_first:", "Image output for tool call read_second:"]);
  expect(users.map((message: Payload) => message.content[1].image_url.url)).toEqual(["data:image/png;base64,YQ==", "data:image/png;base64,Yg=="]);
});

test("Chat flushes trailing image results once at the end of consecutive tool messages", async () => {
  const f = await projectToolMessages("chat_completions", [projectionCalls(["read_a", "read_b"]),
    projectionResult("read_a", [projectionImage("a")]), projectionResult("read_b", [projectionImage("b")])]);
  expect(f.body.messages.map((message: Payload) => message.role)).toEqual(["assistant", "tool", "tool", "user"]);
  expect(f.body.messages.at(-1).content.map((part: Payload) => part.type === "text" ? part.text : part.image_url.url)).toEqual([
    "Image output for tool call read_a:", "data:image/png;base64,YQ==", "Image output for tool call read_b:", "data:image/png;base64,Yg==",
  ]);
});

test.each(["messages", "chat_completions"] as const)("%s preserves mixed text and image order within one tool result", async backendType => {
  const f = await projectToolMessages(backendType, [projectionCalls(["read_mixed"]), projectionResult("read_mixed", [
    { type: "text", text: "first text" }, projectionImage("a"), { type: "text", text: "second text" }, projectionImage("b"),
  ])]);
  const result = toolResults(f.body, backendType)[0]!;
  if (backendType === "messages") {
    expect(result.tool_use_id).toBe("read_mixed");
    expect(result.content.map((part: Payload) => part.type === "text" ? part.text : part.source.data)).toEqual(["first text", "YQ==", "second text", "Yg=="]);
  } else {
    expect(result).toMatchObject({ tool_call_id: "read_mixed", content: "first text\nsecond text" });
    expect(projectionImageParts(f.body).map(part => part.image_url.url)).toEqual(["data:image/png;base64,YQ==", "data:image/png;base64,Yg=="]);
    expect(f.body.messages.at(-1).content[0].text).toBe("Image output for tool call read_mixed:");
  }
});

test("an assignment without model catalog defaults to Messages image tool projection", async () => {
  const f = await projectToolMessages("default", [projectionCalls(["read_default"]), projectionResult("read_default", [projectionImage("a")])]);
  expect(f.paths).toEqual(["/v1/messages"]);
  expect(toolResults(f.body, "messages")[0]).toMatchObject({ tool_use_id: "read_default", content: [{ type: "image", source: { type: "base64", data: "YQ==" } }] });
  expect(f.prompt.filter(message => message.role === "user")).toEqual([]);
});

test("Chat flushes only Read images after a parallel mixed Read and Bash batch", async () => {
  const f = await fixture("chat_completions");
  f.backend.a.launch.tool_permissions = { read: "allow", bash: "allow" };
  const imageTool = f.backend.onTool!;
  f.backend.onTool = async params => params.name === "read" ? imageTool(params) : { content: [{ type: "text", text: "bash stdout" }] };
  const original = f.backend.route.bind(f.backend);
  f.backend.route = async request => {
    if (new URL(request.url).pathname !== "/v1/chat/completions" || f.requests.length) return original(request);
    f.requests.push(await request.json());
    const calls = [
      { index: 0, id: "read_image", type: "function", function: { name: "Read", arguments: JSON.stringify({ file_path: "/workspace/chart.png" }) } },
      { index: 1, id: "bash_text", type: "function", function: { name: "Bash", arguments: JSON.stringify({ command: "echo marker" }) } },
    ];
    return new Response(`data: ${JSON.stringify({ id: "chat_mixed", choices: [{ index: 0, delta: { tool_calls: calls }, finish_reason: "tool_calls" }] })}\n\ndata: [DONE]\n\n`,
      { headers: { "content-type": "text/event-stream" } });
  };
  await f.session.receive("mixed-tool-images", { content: "Read the chart and run Bash" });
  expect(f.requests).toHaveLength(2);
  expect(f.backend.executions).toHaveLength(2);
  const messages: Payload[] = f.requests[1]!.messages;
  const results = toolResults(f.requests[1]!, "chat_completions");
  expect(results).toHaveLength(2);
  expect(results.find(result => result.tool_call_id === "bash_text")).toMatchObject({ content: "bash stdout" });
  expect(results.every(result => typeof result.content === "string")).toBe(true);
  const userIndex = messages.findIndex(message => message.role === "user" && Array.isArray(message.content)
    && message.content.some((part: Payload) => part.type === "image_url"));
  expect(userIndex).toBeGreaterThan(messages.findLastIndex(message => message.role === "tool"));
  expect(messages[userIndex]!.content[0].text).toBe("Image output for tool call read_image:");
  expect(messages[userIndex]!.content.filter((part: Payload) => part.type === "image_url")).toHaveLength(1);
  expect(JSON.stringify(messages[userIndex])).not.toContain("bash_text");
});

test.each(["messages", "chat_completions"] as const)("%s stores native image tool results without storing projected user messages", async backendType => {
  const f = await fixture(backendType);
  await f.session.receive("native-image-log", { content: "Read /workspace/chart.png" });
  const events = f.session.agent.session.snapshotEvents();
  const result = events.find(event => event.type === "tool/result");
  if (result?.type !== "tool/result") throw new Error("Missing native tool result");
  expect(result.data.message.content.map(block => block.type)).toEqual(["text", "image", "text"]);
  const users = events.flatMap(event => event.type === "user/message" ? [event.data] : []);
  expect(users.filter(message => message.source.kind === "user").map(message => message.id)).toEqual([MessageId("native-image-log")]);
  expect(users.flatMap(message => message.content).some(block => block.type === "image")).toBe(false);
  expect(JSON.stringify(users)).not.toContain("Image output for tool call");
});

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
