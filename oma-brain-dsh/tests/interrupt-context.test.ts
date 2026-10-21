import { test, expect } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { mountKernel } from "../src/kernel.ts";
import type { CcrBackend } from "../src/ccrv2/index.ts";
import type { Payload } from "../src/ccrv2/protocol.ts";
import * as CcrBridge from "../src/ccrv2/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { OmaApi } from "../src/api.ts";

test.each(["event", "control"])("%s interruption context survives recovery in native requests without fabricated public messages", async (entry) => {
  const interrupt = entry === "event" ? { type: "user.interrupt" } : { type: "control_request", request_id: "control", request: { subtype: "interrupt" } };
  const first = Promise.withResolvers<void>();
  const second = Promise.withResolvers<void>();
  const captures: unknown[] = [];
  const captureFetch = async (_url: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    captures.push(JSON.parse(String(init?.body)));
    const signal = init?.signal;
    const response = new Promise<Response>((_resolve, reject) => {
      if (signal?.aborted) reject(signal.reason);
      else signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
    if (captures.length === 1) first.resolve(); else second.resolve();
    return response;
  };
  const api = new OmaApi("https://capture-only.invalid", Object.assign(captureFetch, { preconnect() { throw new Error("network access disabled"); } }));
  const id = "int001-local-input-capture";
  api.assignments.set(id, { session_id: id, model_token: "local-capture-only-not-a-credential", ingress_token: "capture-only", worker_epoch: 1, mcp_config: {}, launch: { version: 1, model: "deepseek-flash", work_dir: "/workspace", system: "", tools: ["Bash"] } });
  const logs = new Map<string, Payload[]>();
  const published: unknown[] = [];
  const backend: CcrBackend = {
    async getWorkerState() { return { metadata: null, durationMs: 0 }; },
    async isToolConfirmationPending() { return false; },
    async privateEvents(id) { return structuredClone(logs.get(id) ?? []); },
    async persist(id: string, subtype: string, data: unknown) { logs.set(id, [...(logs.get(id) ?? []), { subtype, schema_version: 1, data }]); },
    async events(_id: string, events: unknown[]) { published.push(...events); }, async acknowledge() {}, async reportDelivery() {}, async reportMetadata() {}, async setStatus() {},
    sessionIds() { return logs.keys(); },
  };
  let ctx = new Context();
  const timeout = setTimeout(() => { throw new Error("capture deadline exceeded"); }, 15_000);
  const createSession = async (ctx: Context) => {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["oma"], new AiSdkAdapter(omaModelOptions(api)));
    return ctx.ccrv2.createSession({
      id, provider: "oma", model: "deepseek-flash", workDir: "/workspace", systemPrompt: "你是一个简洁的中文助手。", tools: ["Bash"],
      setup: async (scope: Context) => {
        await scope.plugin({ name: "capture-sandbox-disabled", apply(ctx: Context) {
          ctx.provide("sandboxExecutor", {
            async provision() { throw new Error("sandbox must not be used"); },
            async call() { throw new Error("tool must not execute"); },
          });
        } });
      },
    });
  };
  try {
    let session = await createSession(ctx);
    await session.open();
    await session.receive("idle-interrupt", interrupt);
    expect(session.agent.session.deriveMessages().filter(message => message.source.kind === "oma-interruption")).toHaveLength(0);
    const old = session.receive("old", { content: "仅调用一次Bash原样执行：sleep 3; printf '%s\\n' 'INT001-CAPTURE-A'。执行完成后仅回复INT001-CAPTURE-A。不得后台运行。" });
    await first.promise;
    await session.receive("interrupt", interrupt);
    await old;
    const completedAfterInterrupt = session.state.completed.includes("old");
    expect(JSON.stringify(logs.get(id))).toContain("oma-interruption");
    await session.receive("old", { content: "Duplicate delivery must not become another model request" });
    expect(captures).toHaveLength(1);
    await ctx.fiber.dispose();
    ctx = new Context();
    session = await createSession(ctx);
    await session.open();
    const next = session.receive("new", { content: "仅回复INT001-CAPTURE-RESUME-0" });
    await second.promise;
    const nativeMessages = session.agent.session.deriveMessages();
    await session.receive("diagnostic-stop", interrupt);
    await next;
    expect(completedAfterInterrupt).toBe(true);
    expect(captures).toHaveLength(2);
    const secondRequest = captures[1] as { messages: { role: string; content: { type: string; text?: string }[] }[] };
    const texts = secondRequest.messages.flatMap(message => message.content).filter(block => block.type === "text").map(block => block.text ?? "");
    expect(texts[0]).toContain("INT001-CAPTURE-A");
    expect(texts.at(-1)).toBe("仅回复INT001-CAPTURE-RESUME-0");
    const contexts = nativeMessages.filter(message => message.source.kind === "oma-interruption");
    expect(contexts).toHaveLength(1);
    expect(contexts[0]!.source).toMatchObject({ inputId: "old" });
    const contextText = contexts[0]!.content.filter(block => block.type === "text").map(block => block.text).join("\n");
    expect(contextText).toContain("interrupted");
    expect(contextText).toContain("cancelled");
    expect(texts.indexOf(contextText)).toBeGreaterThan(0);
    expect(texts.indexOf(contextText)).toBeLessThan(texts.length - 1);
    expect(JSON.stringify(published)).not.toContain(contextText);
  } finally { clearTimeout(timeout); await ctx.fiber.dispose(); }

}, 15_000);
