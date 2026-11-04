import { afterEach,expect,test } from "bun:test";





import { OmaApi } from "../src/api.ts";

import { AttachmentId,ImageVariantId,type AttachmentStore,type ImageAttachmentRef,type ImageRequestTarget } from "@deepseek-ai/dsh-attachment";

import { MessageId,ToolCallId,type AssistantMessage,type ContentBlock,type GenerateOptions,type ToolResultMessage } from "@deepseek-ai/dsh-llm";

import { SessionId } from "@deepseek-ai/dsh-session";

import { callOptions } from "../src/llm-ai-sdk/prompt.ts";

import { omaRoute } from "../src/model-provider.ts";

import { assignment,textEvents,toolEvents } from "./backend.ts";

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

test("Messages replaces every offloaded tool image with a text placeholder without reading bytes", async () => {
  const image = projectionImage("a", true);
  const f = await projectToolMessages("messages", [projectionCalls(["read_omitted"]), projectionResult("read_omitted", [image])], 1);
  expect(projectionImageParts(f.body)).toEqual([]);
  expect(f.reads).toEqual([]);
  expect(toolResults(f.body, "messages")[0]).toMatchObject({ tool_use_id: "read_omitted", content: expect.stringContaining(`[Image ${image.attachment.attachmentId}, 1x1: omitted`) });
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
