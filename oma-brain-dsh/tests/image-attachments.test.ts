import { afterEach,expect,test } from "bun:test";

import { Context } from "@deepseek-ai/cordis";

import { mkdtemp,rm } from "node:fs/promises";

import { tmpdir } from "node:os";

import { join } from "node:path";

import RemoteAttachmentStore,{ AttachmentTransportError,withAttachmentSession } from "../src/attachments/remote.ts";

import { AttachmentId } from "@deepseek-ai/dsh-attachment";

import { SessionId } from "@deepseek-ai/dsh-session";

import { IMAGE_OFFLOAD_REQUIRED_CODE } from "@deepseek-ai/dsh-llm";

import { MAX_MESSAGES_REQUEST_BODY_BYTES } from "../src/model-defaults.ts";

import { AiSdkAdapter } from "../src/llm-ai-sdk/adapter.ts";

import { omaModelOptions } from "../src/model.ts";

import { OmaApi } from "../src/api.ts";

import { Backend } from "./backend.ts";

import { imagePlaceholder,imagePricing } from "../src/llm-ai-sdk/images.ts";

import { userInputContent } from "../src/runtime/user-content.ts";

const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC";

const cleanups: (() => Promise<unknown> | void)[] = [];

afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

async function storeFixture() {
  const backend = new Backend();
  await backend.ready;
  cleanups.push(() => backend.stop());
  const api = new OmaApi(backend.url);
  api.assignments.set(backend.a.session_id, backend.a);
  const ctx = new Context();
  const home = await mkdtemp(join(tmpdir(), "oma-images-"));
  const cacheRoot = join(home, "attachments", "v1");
  cleanups.push(() => rm(home, { recursive: true, force: true }));
  await ctx.plugin(RemoteAttachmentStore, { api, cacheRoot });
  cleanups.push(() => ctx.fiber.dispose());
  const store = ctx.get("attachments") as RemoteAttachmentStore;
  return { backend, api, store, cacheRoot, run: <T>(action: () => Promise<T>) => withAttachmentSession(backend.a.session_id, action) };
}

test("Brain attachment requests match the Go DSR routes", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  f.api.assignments.set(f.backend.a.session_id, { ...f.backend.a });
  const routes: { method: string; path: string; status: number }[] = await Bun.file(
    new URL("../../internal/codesessions/testdata/dsr_attachment_routes.json", import.meta.url),
  ).json();
  const requests: string[] = [];
  f.backend.route = async request => {
    const path = new URL(request.url).pathname
      .replace(f.backend.a.session_id, "cse_route")
      .replace(/image-attachments\/[0-9a-f]{64}$/, `image-attachments/${"a".repeat(64)}`);
    const route = routes.find(route => route.method === request.method && route.path === path);
    expect(route).toBeDefined();
    expect(route!.status).not.toBe(404);
    requests.push(`${request.method} ${path}`);
    return new Response(null, { status: route!.status });
  };
  await expect(f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }))).rejects.toThrow();
  await expect(f.run(() => f.store.readImage(ref))).rejects.toThrow();
  await expect(f.run(() => f.store.source("file_image"))).rejects.toThrow();
  expect(requests).toEqual([
    `PUT /v1/dsr/sessions/cse_route/attachments/image-attachments/${"a".repeat(64)}`,
    `HEAD /v1/dsr/sessions/cse_route/attachments/image-attachments/${"a".repeat(64)}`,
    "GET /v1/dsr/sessions/cse_route/attachments/message-attachments/file_image/content",
  ]);
});

test("transport failures remain retryable and unavailable sources degrade one block", async () => {
  const f = await storeFixture();
  f.backend.imageStatus = 503;
  await expect(f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }))).rejects.toBeInstanceOf(AttachmentTransportError);
  f.backend.imageStatus = undefined;
  const content = await f.run(() => userInputContent({ content: [
    { type: "text", text: "before" }, { type: "image", source: { type: "file", file_id: "file_missing" } },
    { type: "image", source: { type: "base64", media_type: "image/png", data: "bad" } }, { type: "text", text: "after" },
  ] }, f.store));
  expect(content.map(block => block.type)).toEqual(["text", "text", "text", "text"]);
  expect(content[0]).toEqual({ type: "text", text: "before" });
  expect(content[3]).toEqual({ type: "text", text: "after" });
});

test("cache hits still authorize the session and corrupted remote bytes are rejected", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  f.api.assignments.set(f.backend.a.session_id, { ...f.backend.a, worker_epoch: "2", ingress_token: "stale-token" });
  await expect(f.run(() => f.store.readImage(ref))).rejects.toBeInstanceOf(AttachmentTransportError);
  f.api.assignments.set(f.backend.a.session_id, f.backend.a);
  await rm(f.cacheRoot, { recursive: true, force: true });
  f.backend.imageObjects.set(ref.attachmentId.slice(7), { data: new Uint8Array([1, 2]), mediaType: "image/png" });
  await expect(f.run(() => f.store.readImage(ref))).rejects.toThrow();
});

test("normalized images survive a new store with an empty cache and retain native pricing", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  await rm(f.cacheRoot, { recursive: true, force: true });
  const ctx = new Context();
  await ctx.plugin(RemoteAttachmentStore, { api: f.api, cacheRoot: f.cacheRoot });
  cleanups.push(() => ctx.fiber.dispose());
  const other = ctx.get("attachments");
  const image = await f.run(() => other!.readImageRequest(ref, { width: 1, height: 1, maxBytes: 1024 }));
  expect(image.width).toBe(1);
  expect(other!.imageHostPath(ref)).toBeUndefined();
  const priced = imagePricing().priceImages([{ type: "image", attachment: ref }, { type: "image", attachment: ref, offloaded: true }]);
  expect(priced[0]!.visualTokens).toBeGreaterThan(0);
  expect(priced[1]).toEqual({ visualTokens: 0, text: imagePlaceholder(ref) });
});

test("Chat Completions serializes a native image as a data URL", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  const bodies: any[] = [];
  f.backend.a.launch.models!["test-model"]!.api_backend = "chat_completions";
  const api = new OmaApi(f.backend.url, (async (input, init) => {
    const request = new Request(input, init);
    if (new URL(request.url).pathname !== "/v1/chat/completions") return f.backend.route(request);
    bodies.push(await request.json());
    return new Response('data: {"id":"chat_image","choices":[{"index":0,"delta":{"content":"seen"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n',
      { headers: { "content-type": "text/event-stream" } });
  }) as typeof fetch);
  api.assignments.set(f.backend.a.session_id, f.backend.a);
  const adapter = new AiSdkAdapter(omaModelOptions(api, f.store, () => f.backend.a.session_id));
  for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(f.backend.a.session_id),
    messages: [{ role: "user", content: [{ type: "image", attachment: ref }] }] })) {}
  expect(bodies[0].messages[0].content[0].type).toBe("image_url");
  expect(bodies[0].messages[0].content[0].image_url.url).toStartWith("data:image/png;base64,");
});

test("oversized text without retained images fails before any proxy call", async () => {
  const f = await storeFixture();
  let calls = 0;
  const api = new OmaApi("http://model.test", (async (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> => { calls++; return new Response(null, { status: 500 }); }) as typeof fetch);
  api.assignments.set(f.backend.a.session_id, f.backend.a);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  const run = async () => {
    for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(f.backend.a.session_id),
      messages: [{ role: "user", content: [{ type: "text", text: "x".repeat(33 * 1024 * 1024) }] }] })) {}
  };
  await expect(run()).rejects.toThrow("Serialized model request exceeds the proxy limit");
  expect(calls).toBe(0);
});

test.each([400, 404, 413])("permanent source rejection %i preserves neighboring text", async status => {
  const f = await storeFixture();
  f.backend.imageStatus = status;
  const content = await f.run(() => userInputContent({ content: [
    { type: "text", text: "before" }, { type: "image", source: { type: "file", file_id: "file_rejected" } }, { type: "text", text: "after" },
  ] }, f.store));
  expect(content).toEqual([{ type: "text", text: "before" }, { type: "text", text: "[Image attachment unavailable]" }, { type: "text", text: "after" }]);
});

test.each([409, 503])("attachment status %i remains retryable", async status => {
  const f = await storeFixture();
  f.backend.imageStatus = status;
  await expect(f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }))).rejects.toBeInstanceOf(AttachmentTransportError);
});

test.each(["count", "bytes"])("message image %s limit degrades excess blocks without saving them", async limit => {
  const f = await storeFixture();
  if (limit === "count") f.store.imageLimits.maxImagesPerMessage = 1;
  else f.store.imageLimits.maxMessageImageBytes = Buffer.from(png, "base64").byteLength;
  let saves = 0;
  const save = f.store.saveImage.bind(f.store);
  f.store.saveImage = async input => { saves++; return save(input); };
  const content = await f.run(() => userInputContent({ content: [
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
    { type: "text", text: "after" },
  ] }, f.store));
  expect(content.map(block => block.type)).toEqual(["image", "text", "text"]);
  expect(saves).toBe(1);
});

test("image authorization is reused only within one session assignment and epoch", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  f.backend.imageStatus = 503;
  await f.run(() => f.store.readImage(ref));
  await f.run(() => f.store.readImage(ref));
  f.backend.a.worker_epoch = String(BigInt(f.backend.a.worker_epoch) + 1n);
  await expect(f.run(() => f.store.readImage(ref))).rejects.toBeInstanceOf(AttachmentTransportError);
  f.backend.imageStatus = undefined;
  await f.run(() => f.store.readImage(ref));
  f.backend.imageStatus = 503;
  f.api.assignments.set("other", { ...f.backend.a, session_id: "other" });
  await expect(withAttachmentSession("other", () => f.store.readImage(ref))).rejects.toBeInstanceOf(AttachmentTransportError);
});

test("compression hands a released slot to the waiting caller before a newcomer", async () => {
  const f = await storeFixture();
  const { store } = f;
  const compress = (store as unknown as { compress(action: () => Promise<void>): Promise<void> }).compress.bind(store);
  let active = 0;
  let maximum = 0;
  const releases: (() => void)[] = [];
  const tasks: Promise<void>[] = [];
  const action = () => { active++; maximum = Math.max(maximum, active); return new Promise<void>(resolve => releases.push(() => { active--; resolve(); })); };
  tasks.push(compress(action), compress(action));
  tasks.push(compress(action));
  const waiting = (store as unknown as { waiting: (() => void)[] }).waiting;
  const wake = waiting[0]!;
  waiting[0] = () => { wake(); tasks.push(compress(action)); };
  releases.shift()!();
  for (let index = 0; index < 10; index++) {
    await Promise.resolve();
    while (releases.length) releases.shift()!();
  }
  await Promise.all(tasks);
  expect(maximum).toBe(2);
});

test("adapter pricing defaults unspecified modalities to text", () => {
  const adapter = new AiSdkAdapter({ route: () => { throw new Error("pricing must not request a route"); } });
  const result = adapter.imageRequestPricing("test", "test").priceImages([{ type: "image", attachment: {
    attachmentId: AttachmentId(`sha256:${"a".repeat(64)}`), mediaType: "image/png", bytes: 100, width: 1024, height: 1024,
  } }]);
  expect(result[0]!.visualTokens).toBe(0);
});

test("serialized image overflow preserves the native offload error contract", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  const adapter = new AiSdkAdapter(omaModelOptions(f.api, f.store, () => f.backend.a.session_id));
  const run = async () => {
    for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(f.backend.a.session_id),
      messages: [{ role: "user", content: [{ type: "text", text: "x".repeat(MAX_MESSAGES_REQUEST_BODY_BYTES) }, { type: "image", attachment: ref }] }] })) {}
  };
  await expect(run()).rejects.toMatchObject({ code: IMAGE_OFFLOAD_REQUIRED_CODE, failure: { offloadImages: 1 } });
  expect(f.backend.requests).toHaveLength(0);
});

test("Brain serialized request limit matches the Go proxy contract", async () => {
  const source = await Bun.file(new URL("../../internal/llmproviders/request.go", import.meta.url)).text();
  const limit = source.match(/const MaxMessagesRequestBodyBytes int64 = (\d+) << (\d+)/);
  expect(limit).not.toBeNull();
  expect(MAX_MESSAGES_REQUEST_BODY_BYTES).toBe(Number(limit![1]) * 2 ** Number(limit![2]));
});

test.each([401, 403, 408, 410, 429, 500, 502, 504])("attachment HTTP %i preserves input for recovery", async status => {
  const f = await storeFixture();
  f.backend.imageStatus = status;
  await expect(f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }))).rejects.toBeInstanceOf(AttachmentTransportError);
});

test("rejected oversized images do not consume the budget of later small images", async () => {
  const f = await storeFixture();
  f.store.imageLimits.maxMessageImageBytes = Buffer.from(png, "base64").byteLength;
  const content = await f.run(() => userInputContent({ content: [
    { type: "image", source: { type: "base64", media_type: "image/png", data: Buffer.alloc(1000).toString("base64") } },
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
  ] }, f.store));
  expect(content.map(block => block.type)).toEqual(["text", "image"]);
  expect(f.backend.imageObjects.size).toBe(1);
});

test("a failed image admission does not consume bytes reserved for later images", async () => {
  const f = await storeFixture();
  f.store.imageLimits.maxMessageImageBytes = Buffer.from(png, "base64").byteLength;
  const original = f.backend.route.bind(f.backend);
  let puts = 0;
  f.backend.route = request => request.method === "PUT" && new URL(request.url).pathname.includes("/image-attachments/") && ++puts === 1
    ? Promise.resolve(new Response(null, { status: 400 })) : original(request);
  const content = await f.run(() => userInputContent({ content: Array.from({ length: 2 }, () => (
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } }
  )) }, f.store));
  expect(content.map(block => block.type)).toEqual(["text", "image"]);
});

test("a missing stored image uses the reference error code without a legacy migration", async () => {
  const f = await storeFixture();
  const ref = await f.run(() => f.store.saveImage({ data: Buffer.from(png, "base64"), mediaType: "image/png" }));
  f.api.assignments.set(f.backend.a.session_id, { ...f.backend.a });
  f.backend.imageObjects.clear();
  await expect(f.run(() => f.store.readImage(ref))).rejects.toMatchObject({ code: "ATTACHMENT_NOT_FOUND" });
  expect(f.backend.imageObjects.size).toBe(0);
});

test("Brain rejects unmaterialized image sources instead of creating legacy custom blocks", async () => {
  const f = await storeFixture();
  await expect(f.run(() => userInputContent({ content: [
    { type: "image", source: { type: "url", url: "https://images.example/image.png" } },
  ] }, f.store))).rejects.toThrow("Unsupported image source");
});

test("legacy custom image blocks have no request encoder compatibility", async () => {
  const f = await storeFixture();
  const legacy = JSON.parse('{"type":"oma-image","source":{"type":"file","file_id":"file_legacy"}}');
  const adapter = new AiSdkAdapter(omaModelOptions(f.api, f.store));
  await expect((async () => {
    for await (const _chunk of adapter.stream({ provider: "oma", model: "test-model", sessionId: SessionId(f.backend.a.session_id),
      messages: [{ role: "user", content: [legacy] }] })) {}
  })()).rejects.toMatchObject({ code: "INVALID_ARGS", message: "Unsupported model input block: oma-image" });
  expect(f.backend.requests).toHaveLength(0);
});
