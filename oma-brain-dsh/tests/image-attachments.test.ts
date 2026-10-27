import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import RemoteAttachmentStore, { withAttachmentSession, AttachmentTransportError } from "../src/attachments/remote.ts";
import { SessionId } from "@deepseek-ai/dsh-session";
import { AiSdkAdapter } from "../src/llm-ai-sdk/adapter.ts";
import { omaModelOptions } from "../src/model.ts";
import { OmaApi } from "../src/api.ts";
import { Backend, textEvents } from "./backend.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { imagePricing, imagePlaceholder } from "../src/llm-ai-sdk/images.ts";
import { userInputContent } from "../src/ccrv2/user-content.ts";

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
  f.backend.a.worker_epoch++;
  f.api.assignments.set(f.backend.a.session_id, { ...f.backend.a, worker_epoch: 1 });
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

async function openRuntime(backend: Backend) {
  await backend.ready;
  cleanups.push(() => backend.stop());
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  const home = await mkdtemp(join(tmpdir(), "oma-runtime-images-"));
  cleanups.push(() => rm(home, { recursive: true, force: true }));
  await mountOma(ctx, api, {}, undefined, join(home, "attachments", "v1"));
  const session = createOmaSession(ctx, api, backend.a);
  await session.open();
  cleanups.push(async () => { session.stop(); await session.handle?.dispose(); await ctx.fiber.dispose(); });
  return { session, ctx, api };
}

test("a failed image upload is not acknowledged or recorded as a completed input", async () => {
  const backend = new Backend();
  backend.imageStatus = 503;
  const { session } = await openRuntime(backend);
  await expect(session.receive("retry-image", { content: [{ type: "image", source: { type: "base64", media_type: "image/png", data: png } }] })).rejects.toThrow();
  expect(session.receivedInputIds.has("retry-image")).toBe(false);
  expect(backend.processed).not.toContain("retry-image");
  expect(backend.requests).toHaveLength(0);
  backend.imageStatus = undefined;
  const recovered = await openRuntime(backend);
  await recovered.session.receive("retry-image", { content: [{ type: "image", source: { type: "base64", media_type: "image/png", data: png } }] });
  expect(backend.requests).toHaveLength(1);
  expect(recovered.session.receivedInputIds.has("retry-image")).toBe(true);
  expect(backend.publicLog.get("retry-image_result")?.is_error).toBe(false);
});

test.each([
  { name: "count", limits: { max_images: 1 } },
  { name: "bytes", limits: { max_request_image_bytes: 100 } },
])("native image $name budget persists offload and sends only retained images", async ({ limits }) => {
  const backend = new Backend();
  backend.a.launch.models!["test-model"]!.input_modalities = ["text", "image"];
  backend.a.launch.models!["test-model"]!.image_input = limits;
  backend.modelScript = [textEvents("seen")];
  const { session } = await openRuntime(backend);
  await session.receive("budget", { content: [
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
  ] });
  expect(backend.requests).toHaveLength(1);
  const content = backend.requests[0]!.messages.flatMap((message: any) => message.content);
  expect(content.filter((block: any) => block.type === "image")).toHaveLength(1);
  expect(content.some((block: any) => block.text?.includes("remains saved in this session"))).toBe(true);
  expect(JSON.stringify([...backend.privateLog.values()])).toContain("image/offload");
  expect(JSON.stringify([...backend.privateLog.values()])).not.toContain(png);
  expect(backend.publicLog.get("budget_result")?.is_error).toBe(false);
});

test("summary image overflow records offload and finishes compaction", async () => {
  const backend = new Backend();
  backend.a.launch.models!["test-model"]!.input_modalities = ["text", "image"];
  backend.a.launch.models!["test-model"]!.image_input = { max_images: 2 };
  backend.a.launch.models!["test-model"]!.context_window = 200000;
  backend.modelScript = [textEvents("a".repeat(12000)), textEvents("Previous image task details.")];
  const { session, ctx } = await openRuntime(backend);
  await session.receive("summary-images", { content: [
    { type: "text", text: "u".repeat(12000) },
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
    { type: "image", source: { type: "base64", media_type: "image/png", data: png } },
  ] });
  backend.a.launch.models!["test-model"]!.image_input = { max_images: 1 };
  const result = await ctx.compaction.compactNow(session.runtime.agent, new AbortController().signal);
  expect(result).toBeTruthy();
  const events = session.runtime.agent.session.snapshotEvents();
  expect(events.some(event => String(event.type) === "image/offload")).toBe(true);
  expect(events.some(event => event.type === "compaction/summary")).toBe(true);
});

test("a new Brain restores native image history from remote bytes without re-uploading", async () => {
  const backend = new Backend();
  backend.a.launch.models!["test-model"]!.input_modalities = ["text", "image"];
  backend.modelScript = [textEvents("seen"), textEvents("remembered")];
  const first = await openRuntime(backend);
  await first.session.receive("original-image", { content: [{ type: "image", source: { type: "base64", media_type: "image/png", data: png } }] });
  await first.session.close();
  const second = await openRuntime(backend);
  await second.session.receive("remember-image", { content: "Remember that image" });
  expect(backend.imageReads).toBeGreaterThan(0);
  expect(backend.imageObjects.size).toBe(1);
  expect(backend.requests.at(-1)!.messages.flatMap((message: any) => message.content).some((block: any) => block.type === "image")).toBe(true);
  expect(backend.publicLog.get("remember-image_result")?.is_error).toBe(false);
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
