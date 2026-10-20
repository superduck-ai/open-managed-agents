import { expect, spyOn, test } from "bun:test";
import { ContextRecovery } from "../src/runtime/compaction.ts";
import { SessionId } from "@deepseek-ai/dsh-session";
import { Context } from "@deepseek-ai/cordis";
import { LlmAdapter, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { mountKernel } from "../src/kernel.ts";
import * as CcrBridge from "../src/ccrv2/index.ts";
import type { CcrBackend } from "../src/ccrv2/index.ts";
import type { SandboxExecutor } from "../src/runtime/contracts.ts";
import type { Payload } from "../src/ccrv2/protocol.ts";
import { emptyState } from "../src/ccrv2/protocol.ts";
import type { ClientEvent, SessionState, RequiresActionDetails } from "../src/ccrv2/wire.ts";
import type { CcrLeaseResult, CcrLeaseFailure } from "../src/ccrv2/index.ts";

class MemoryBackend implements CcrBackend {
  readonly logs = new Map<string, Payload[]>();
  readonly output = new Map<string, Payload[]>();
  readonly acknowledged: string[] = [];
  readonly metadata = new Map<string, Record<string, unknown>>();
  async getWorkerState(id: string) { return { metadata: structuredClone(this.metadata.get(id) ?? null), durationMs: 0 }; }
  async getSubagentThread(id: string, threadId: string) {
    const events = (this.output.get(id) ?? []).filter((event) => event.session_thread_id === threadId);
    if (!events.some((event) => event.type === "session.thread_created")) throw new Error("Unknown subagent thread");
    const archived = events.some((event) => event.type === "session.thread_status_terminated");
    return {id: threadId, status: archived ? "terminated" : "idle", archived};
  }
  async isToolConfirmationPending(_id: string, _requestId: string, _signal?: AbortSignal) { return false; }
  async privateEvents(id: string, options?: { subagents?: boolean }) {
    return structuredClone((this.logs.get(id) ?? []).filter((event) =>
      options?.subagents ? !!event.agent_id : !event.agent_id));
  }
  async persist(id: string, subtype: string, data: unknown, agentId?: string) {
    const log = this.logs.get(id) ?? [];
    log.push(structuredClone({ subtype, schema_version: 1, data, ...(agentId ? { agent_id: agentId } : {}) }));
    this.logs.set(id, log);
  }
  async events(id: string, payloads: Payload[]) {
    this.output.set(id, [...(this.output.get(id) ?? []), ...structuredClone(payloads)]);
  }
  async acknowledge(id: string, inputId: string) { this.acknowledged.push(`${id}:${inputId}`); }
  async reportDelivery() {}
  async setStatus(_id: string, _status: SessionState, _details?: RequiresActionDetails) {}
  async reportMetadata(id: string, patch: Record<string, unknown>) {
    const metadata = { ...this.metadata.get(id) };
    for (const [key, value] of Object.entries(patch)) {
      if (value === null) delete metadata[key];
      else metadata[key] = structuredClone(value);
    }
    this.metadata.set(id, metadata);
  }
  sessionIds() { return this.logs.keys(); }
}

class TextModel extends LlmAdapter {
  readonly requests: GenerateOptions[] = [];
  async resolveModel(provider: string, model: string) {
    return { provider, id: model, name: model };
  }
  async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    this.requests.push(options);
    yield { type: "block-start", index: 0, blockType: "text" };
    yield { type: "text-delta", index: 0, text: "standalone reply" };
    yield { type: "block-end", index: 0, block: { type: "text", text: "standalone reply" } };
    yield { type: "finish", reason: { kind: "stop" } };
  }
}

const sandbox: SandboxExecutor = {
  async provision() { throw new Error("Text turns must not provision a sandbox"); },
  async call() { throw new Error("Text turns must not execute tools"); },
};

async function setup(scope: Context): Promise<void> {
  await scope.plugin({
    name: "test-sandbox-provider",
    apply(ctx: Context) { ctx.provide("sandboxExecutor", sandbox); },
  });
}

test("session previews enter one stream window and terminal publication still waits for their HTTP confirmation", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const timers = new Set<() => void>();
  const batches: ClientEvent[][] = [];
  const completed = Promise.withResolvers<void>();
  const sending = Promise.withResolvers<void>();
  const response = Promise.withResolvers<void>();
  const client = new CcrBridge.CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["buffered"],
    schedule: (callback, delayMs) => {
      expect(delayMs).toBe(100); timers.add(callback); return () => { timers.delete(callback); };
    },
    request: async (request) => {
      const batch = (request.body as { events: ClientEvent[] }).events;
      batches.push(batch);
      if (batch.some((event) => event.ephemeral)) { sending.resolve(); await response.promise; }
      return new Response(null, { status: 204 });
    },
  });
  const detach = client.bindLease("buffered").subscribe!(() => {});
  let previewCalls = 0;
  const events = backend.events.bind(backend);
  backend.events = async (id, payloads) => {
    if (payloads.some((payload) => ["event_start", "event_delta", "event_stop"].includes(payload.type))) previewCalls++;
    await client.events(id, payloads);
    await events(id, payloads);
  };
  const persist = backend.persist.bind(backend);
  backend.persist = async (id, subtype, data, agentId) => {
    await persist(id, subtype, data, agentId);
    if (subtype === "dsh_brain_state" && data && typeof data === "object" && "phase" in data && data.phase === "completed")
      completed.resolve();
  };
  const model = new TextModel();
  model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
    this.requests.push(options);
    yield { type: "block-start", index: 0, blockType: "text" };
    yield { type: "text-delta", index: 0, text: "A" };
    yield { type: "text-delta", index: 0, text: "B" };
    yield { type: "block-end", index: 0, block: { type: "text", text: "AB" } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  let session: ReturnType<Context["ccrv2"]["createSession"]> | undefined;
  let turn: Promise<void> | undefined;
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    session = ctx.ccrv2.createSession({ id: "buffered", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", setup });
    await session.open();
    turn = session.receive("input", { content: "hello" });
    await completed.promise;
    expect(previewCalls).toBe(3);
    expect(timers.size).toBe(1);
    expect(batches.flat().filter((event) => event.ephemeral)).toEqual([]);
    expect(session.state.outbox.length).toBeGreaterThan(0);
    expect(backend.acknowledged).toEqual([]);
    [...timers][0]!();
    await sending.promise;
    const previews = batches.flat().filter((event) => event.ephemeral);
    expect(previews.map((event) => event.payload.type)).toEqual(["event_start", "event_delta", "event_delta", "event_stop"]);
    expect(previews.filter((event) => event.payload.type === "event_delta").map((event) => event.payload.delta))
      .toEqual(["A", "B"].map((text) => ({ type: "content_delta", index: 0, content: { type: "text", text } })));
    expect(session.state.outbox.length).toBeGreaterThan(0);
    expect(backend.acknowledged).toEqual([]);
    response.resolve(); await turn;
    expect(session.state.outbox).toEqual([]);
    expect(backend.acknowledged).toEqual(["buffered:input"]);
    expect(timers.size).toBe(0);
  } finally {
    response.resolve(); session?.stop(); detach();
    await turn?.catch(() => {});
    await ctx.fiber.dispose();
  }
});

test("failed final persistence releases the session writer and permits replacement", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], new TextModel());
    const persist = backend.persist.bind(backend);
    for (let i = 0; i < 10; i++) {
      const session = ctx.ccrv2.createSession({
        id: "replaced", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", setup,
      });
      await session.open();
      const writers = (ctx.sessionPersistence as unknown as {
        writers: Map<string, { enqueue(event: unknown): void; record: { events: unknown[] } }>;
      }).writers;
      const writer = writers.get(session.id)!;
      writer.enqueue({ seq: writer.record.events.length, type: "test/pending", data: {} });
      backend.persist = async () => { throw new Error("Lease expired"); };
      await expect(session.close()).rejects.toThrow();
      expect(() => ctx.ccrv2.forget(session.id)).not.toThrow();
      backend.persist = persist;
    }
  } finally {
    await ctx.fiber.dispose();
  }
});

test("startup clears task_summary after recovery persistence and before status or publication, including blocked recovery", async () => {
  for (const phase of ["idle", "dispatch"] as const) {
    const backend = new MemoryBackend();
    const state = emptyState();
    state.phase = phase;
    state.revision = 1;
    state.inputId = phase === "dispatch" ? "interrupted-input" : "";
    state.outbox = [{ type: "system.message", uuid: "pending-message" }];
    backend.logs.set("recovery", [{ subtype: "dsh_brain_state", schema_version: 1, data: state }]);
    const order: string[] = [];
    const started = Promise.withResolvers<void>();
    const confirmed = Promise.withResolvers<void>();
    const persist = backend.persist.bind(backend);
    backend.persist = async (...args) => { order.push(`persist:${args[1]}`); await persist(...args); };
    backend.reportMetadata = async (_id, patch) => {
      expect(patch).toEqual({ task_summary: null, pending_action: null, model: "text" });
      order.push("metadata"); started.resolve(); await confirmed.promise;
    };
    backend.setStatus = async (_id, status) => { order.push(`status:${status}`); };
    backend.events = async () => { order.push("publish"); };
    const ctx = new Context();
    try {
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      const session = ctx.ccrv2.createSession({
        id: "recovery", provider: "memory", model: "text", workDir: "/workspace", systemPrompt: "Test", setup,
      });
      const opening = session.open();
      await started.promise;
      expect(order.at(-1)).toBe("metadata");
      expect(order.some((step) => step.startsWith("status:") || step === "publish")).toBe(false);
      if (phase === "dispatch") expect(order).toContain("persist:dsh_brain_state");
      confirmed.resolve();
      await opening;
      expect(session.blocked()).toBe(phase === "dispatch");
      expect(order.indexOf("publish")).toBeGreaterThan(order.indexOf("metadata"));
      if (phase === "idle") expect(order.indexOf("status:idle")).toBeGreaterThan(order.indexOf("metadata"));
      else expect(order.some((step) => step.startsWith("status:"))).toBe(false);
    } finally { confirmed.resolve(); await ctx.fiber.dispose(); }
  }
});

test("worker metadata restores the model across logged history and set_model waits for its patch before ACK", async () => {
  const backend = new MemoryBackend();
  backend.metadata.set("model-session", {
    model: "restored-model", pending_action: { request_id: "obsolete" }, task_summary: "obsolete",
    custom: { keep: true }, permission_mode: "plan",
  });
  const mount = async () => {
    const ctx = new Context();
    await mountKernel(ctx);
    const model = new TextModel();
    await ctx.plugin({ inject: ["llm"], apply(scope: Context) { scope.llm.registerAdapter(["memory"], model); } });
    await ctx.plugin(CcrBridge, { backend });
    const session = ctx.ccrv2.createSession({
      id: "model-session", provider: "memory", model: "launch-model", workDir: "/workspace", systemPrompt: "Test", setup,
    });
    await session.open();
    return { ctx, model, session };
  };
  const first = await mount();
  try {
    expect(backend.metadata.get("model-session")).toEqual({
      model: "restored-model", custom: { keep: true }, permission_mode: "plan",
    });
    await first.session.receive("first", { content: "hello" });
    expect(first.model.requests[0]!.model).toBe("restored-model");
    const started = Promise.withResolvers<void>();
    const release = Promise.withResolvers<void>();
    const patch = backend.reportMetadata.bind(backend);
    backend.reportMetadata = async (id, metadata) => { started.resolve(); await release.promise; await patch(id, metadata); };
    const changing = first.session.receive("change", {
      type: "control_request", request_id: "change-request", request: { subtype: "set_model", model: "changed-model" },
    });
    try {
      await started.promise;
      expect(backend.acknowledged).not.toContain("model-session:change");
      expect(first.session.agent.options.model).toBe("restored-model");
      release.resolve();
      await changing;
    } finally { release.resolve(); backend.reportMetadata = patch; }
    expect(backend.acknowledged).toContain("model-session:change");
    await first.session.receive("second", { content: "continue" });
    expect(first.model.requests[1]!.model).toBe("changed-model");
  } finally { await first.ctx.fiber.dispose(); }
  // Deliberately differ from the last logged request, which used changed-model.
  await backend.reportMetadata("model-session", { model: "new-remote-model" });
  const second = await mount();
  try {
    await second.session.receive("third", { content: "resume" });
    expect(second.model.requests[0]!.model).toBe("new-remote-model");
    await second.session.receive("invalid-model", {
      type: "control_request", request_id: "invalid-model", request: { subtype: "set_model", model: "" },
    });
    expect(second.session.config.model).toBe("new-remote-model");
    expect(backend.output.get("model-session")!.some((event) =>
      event.type === "control_response" && event.response.request_id === "invalid-model" && event.response.subtype === "error")).toBe(true);
    await second.session.receive("default-model", {
      type: "control_request", request_id: "default-model", request: { subtype: "set_model", model: "default" },
    });
    await second.session.receive("fourth", { content: "default" });
    expect(second.model.requests[1]!.model).toBe("launch-model");
  } finally { await second.ctx.fiber.dispose(); }
});

test("startup waits for GET and logs restoration only after successful initialization", async () => {
  const backend = new MemoryBackend();
  const read = Promise.withResolvers<{ metadata: Record<string, unknown> | null; durationMs: number }>();
  backend.getWorkerState = async () => read.promise;
  const logs: string[] = [];
  const info = spyOn(console, "info").mockImplementation((line: unknown) => { logs.push(String(line)); });
  const error = spyOn(console, "error").mockImplementation((line: unknown) => { logs.push(String(line)); });
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    const session = ctx.ccrv2.createSession({
      id: "waiting", provider: "memory", model: "text", workDir: "/workspace", systemPrompt: "Test", setup,
    });
    const opening = session.open();
    expect(session.handle).toBeUndefined();
    expect(logs).toEqual([]);
    backend.reportMetadata = async () => { throw new Error("private-server-message"); };
    read.resolve({ metadata: { model: "private-model" }, durationMs: 50 });
    await expect(opening).rejects.toThrow("private-server-message");
    expect(session.abort.signal.aborted).toBe(true);
    expect(logs).toHaveLength(1);
    expect(logs[0]).toContain("ccrv2_worker_lifecycle_init_failed");
    expect(JSON.parse(logs[0]!)).toMatchObject({ stage: "report_metadata", failure_reason: "unexpected_error" });
    expect(logs[0]).not.toContain("private-");
    expect(logs[0]).not.toContain("state_restored");
  } finally { read.resolve({ metadata: null, durationMs: 0 }); await ctx.fiber.dispose(); info.mockRestore(); error.mockRestore(); }
});

test("plugin heartbeats start before opening and session close cancels and drains the request", async () => {
  const ctx = new Context();
  const started = Promise.withResolvers<AbortSignal>();
  const response = Promise.withResolvers<CcrLeaseResult>();
  const lost: CcrLeaseFailure[] = [];
  const bound: string[] = [];
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, {
      backend: new MemoryBackend(),
      heartbeat: {
        intervalMs: 5,
        bind: (id: string) => {
          bound.push(id);
          return (signal: AbortSignal) => { started.resolve(signal); return response.promise; };
        },
        onLeaseLost: (_session: CcrBridge.CcrSession, failure: CcrLeaseFailure) => { lost.push(failure); },
      },
    });
    const session = ctx.ccrv2.createSession({
      id: "heartbeat", provider: "memory", model: "text", workDir: "/workspace",
      systemPrompt: "Test", setup,
    });
    const signal = await started.promise;
    expect(bound).toEqual([session.id]);
    expect(session.handle).toBeUndefined();
    let closed = false;
    const closing = session.close().then(() => { closed = true; });
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(signal.aborted).toBe(true);
    expect(closed).toBe(false);
    response.resolve({ status: "rejected", httpStatus: 409 });
    await closing;
    expect(lost).toEqual([]);
    ctx.ccrv2.forget(session.id);
  } finally {
    response.resolve({ status: "confirmed" });
    await ctx.fiber.dispose();
  }
});

test("unloading the CCRv2 plugin cancels every session heartbeat and drains in-flight requests", async () => {
  const ctx = new Context();
  const requests = new Map<string, AbortSignal>();
  const started = Promise.withResolvers<void>();
  const response = Promise.withResolvers<CcrLeaseResult>();
  let lost = 0;
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, {
      backend: new MemoryBackend(),
      heartbeat: {
        intervalMs: 5,
        bind: (id: string) => (signal: AbortSignal) => {
          requests.set(id, signal);
          if (requests.size === 2) started.resolve();
          return response.promise;
        },
        onLeaseLost: () => { lost++; },
      },
    });
    for (const id of ["a", "b"]) ctx.ccrv2.createSession({
      id, provider: "memory", model: "text", workDir: "/workspace", systemPrompt: "Test", setup,
    });
    await started.promise;
    let disposed = false;
    const disposing = ctx.fiber.dispose().then(() => { disposed = true; });
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect([...requests.values()].every((signal) => signal.aborted)).toBe(true);
    expect(disposed).toBe(false);
    response.resolve({ status: "unavailable" });
    await disposing;
    expect(lost).toBe(0);
  } finally {
    response.resolve({ status: "confirmed" });
    await ctx.fiber.dispose();
  }
});

test("the plugin reports lease loss for the claimed session without stopping other sessions", async () => {
  const ctx = new Context();
  const lost = Promise.withResolvers<{ session: CcrBridge.CcrSession; failure: CcrLeaseFailure }>();
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, {
      backend: new MemoryBackend(),
      heartbeat: {
        intervalMs: 5,
        bind: (id: string) => async (): Promise<CcrLeaseResult> => id === "a"
          ? { status: "rejected", httpStatus: 409 } : { status: "confirmed" },
        onLeaseLost: (session: CcrBridge.CcrSession, failure: CcrLeaseFailure) => {
          session.stop();
          lost.resolve({ session, failure });
        },
      },
    });
    const config = (id: string) => ({
      id, provider: "memory", model: "text", workDir: "/workspace", systemPrompt: "Test", setup,
    });
    const a = ctx.ccrv2.createSession(config("a"));
    const b = ctx.ccrv2.createSession(config("b"));
    const notification = await lost.promise;
    expect(notification.session).toBe(a);
    expect(notification.failure).toEqual({ status: "rejected", httpStatus: 409, consecutiveFailures: 1 });
    expect(a.abort.signal.aborted).toBe(true);
    expect(b.abort.signal.aborted).toBe(false);
  } finally {
    await ctx.fiber.dispose();
  }
});

test("automatic approval stays running while the backend evaluates permission", async () => {
  const backend = new MemoryBackend();
  const states: { status: SessionState; details?: RequiresActionDetails }[] = [];
  backend.setStatus = async (_id, status, details) => { states.push({ status, details }); };
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    const session = ctx.ccrv2.createSession({
      id: "approval", provider: "memory", model: "text", workDir: "/workspace",
      systemPrompt: "Test", setup,
    });
    await session.open();
    backend.events = async (_id, events: Record<string, unknown>[]) => {
      const request = events.find((event) => event.type === "control_request");
      if (!request) return;
      if (typeof request.request_id !== "string") throw new Error("Missing approval identity");
      expect(states.at(-1)!.status).toBe("running");
      expect(states.at(-1)!.details).toBeUndefined();
      await session.control({
        type: "control_response",
        response: {
          subtype: "success", request_id: request.request_id,
          response: { behavior: "allow", updatedInput: { command: "approved" } },
        },
      });
    };
    const decision = await session.approve({
      callId: "call", toolName: "Bash", input: { command: "original" },
    }, new AbortController().signal);
    expect(decision).toEqual({ allowed: true, input: { command: "approved" } });
    expect(states.map((state) => state.status)).toEqual(["idle", "running", "running"]);
    expect(session.approvals.size).toBe(0);
  } finally {
    await ctx.fiber.dispose();
  }
});

test("child history resumes across scoped and legacy foreground logs", async () => {
  for (const legacy of [false, true]) {
    const backend = new MemoryBackend();
    const mount = async () => {
      const ctx = new Context();
      await mountKernel(ctx);
      const model = new TextModel();
      await ctx.plugin({ inject: ["llm"], apply(scope: Context) { scope.llm.registerAdapter(["memory"], model); } });
      await ctx.plugin(CcrBridge, { backend });
      const session = ctx.ccrv2.createSession({
        id: "parent", provider: "memory", model: "text", workDir: "/workspace",
        systemPrompt: "Parent instructions", setup, subagents: {},
      });
      await session.open();
      return { ctx, model, session };
    };
    const first = await mount();
    let threadId: string;
    try {
      await first.session.receive("root", { content: "parent task" });
      const report = JSON.parse(await first.session.subagents.send({
        name: "child", system_prompt: "Child instructions", message: "original child task",
      }, first.session.abort.signal));
      threadId = report.thread_id;
      expect(typeof threadId).toBe("string");
    } finally { await first.ctx.fiber.dispose(); }
    const logs = backend.logs.get("parent")!;
    expect(logs.some((event) => event.agent_id === threadId! && event.subtype === "dsh_log")).toBe(true);
    if (legacy) for (const event of logs) delete event.agent_id;
    const second = await mount();
    try {
      await second.session.subagents.send({ thread_id: threadId!, message: "child follow-up" }, second.session.abort.signal);
      const history = JSON.stringify(second.model.requests[0]!.messages);
      expect(history).toContain("original child task");
      expect(history).toContain("child follow-up");
      expect(history).not.toContain("parent task");
    } finally { await second.ctx.fiber.dispose(); }
    // A third host also merges the old foreground prefix with new scoped suffixes.
    const third = await mount();
    try {
      await third.session.subagents.send({ thread_id: threadId!, message: "third child task" }, third.session.abort.signal);
      const history = JSON.stringify(third.model.requests[0]!.messages);
      expect(history).toContain("original child task");
      expect(history).toContain("child follow-up");
      expect(history).toContain("third child task");
    } finally { await third.ctx.fiber.dispose(); }
  }
});

test("CCRv2 plugin runs isolated sessions and restores history without OMA", async () => {
  const backend = new MemoryBackend();
  const config = (id: string) => ({
    id, provider: "memory", model: "text", workDir: "/workspace",
    systemPrompt: "Standalone test instructions", setup,
  });
  const mount = async () => {
    const ctx = new Context();
    await mountKernel(ctx);
    const model = new TextModel();
    await ctx.plugin({
      inject: ["llm"],
      apply(scope: Context) { scope.llm.registerAdapter(["memory"], model); },
    });
    await ctx.plugin(CcrBridge, { backend });
    return { ctx, model };
  };
  const first = await mount();
  try {
    const a = first.ctx.ccrv2.createSession(config("a"));
    const b = first.ctx.ccrv2.createSession(config("b"));
    await a.open();
    await b.open();
    // Transports pass opaque values; the bridge rejects invalid protocol input.
    for (const payload of [null, [], "plain text", 42]) {
      await expect(a.receive("invalid", payload)).rejects.toThrow("Expected a JSON object");
    }
    expect(backend.acknowledged).toEqual([]);
    expect(first.model.requests).toHaveLength(0);
    expect(a.state.phase).toBe("idle");
    await a.receive("input_a", { content: "first session" });
    await b.receive("input_b", { content: "second session" });
    expect(backend.acknowledged).toEqual(["a:input_a", "b:input_b"]);
    expect(a.state.completed).toEqual(["input_a"]);
    expect(b.state.completed).toEqual(["input_b"]);
    expect(JSON.stringify(first.model.requests[0]!.messages)).toContain("Standalone test instructions");
    expect(first.model.requests[1]!.messages.some((m) => JSON.stringify(m).includes("first session"))).toBe(false);
    expect(backend.output.get("a")!.some((e) => e.type === "result")).toBe(true);
  } finally {
    await first.ctx.fiber.dispose();
  }
  const restored = await mount();
  try {
    const a = restored.ctx.ccrv2.createSession(config("a"));
    await a.open();
    await a.receive("input_a", { content: "duplicate" });
    expect(restored.model.requests).toHaveLength(0);
    await a.receive("next_input", { content: "continue" });
    expect(restored.model.requests).toHaveLength(1);
    expect(restored.model.requests[0]!.messages.some(
      (m) => m.role === "assistant" && JSON.stringify(m).includes("standalone reply"),
    )).toBe(true);
    expect(a.state.outbox).toEqual([]);
  } finally {
    await restored.ctx.fiber.dispose();
  }
});

test("only backend-confirmed user approvals pause the worker and late lookups cannot pause resolved calls", async () => {
  for (const mode of ["automatic", "confirmation", "late"] as const) {
    const backend = new MemoryBackend();
    const states: SessionState[] = [];
    const requested = Promise.withResolvers<string>();
    const lookingUp = Promise.withResolvers<void>();
    const lookup = Promise.withResolvers<boolean>();
    const paused = Promise.withResolvers<void>();
    let lookupSignal: AbortSignal | undefined;
    backend.isToolConfirmationPending = async (_id, _requestId, signal?: AbortSignal) => {
      lookupSignal = signal;
      lookingUp.resolve();
      return lookup.promise;
    };
    backend.events = async (_id, events) => {
      const request = events.find((event) => event.type === "control_request");
      if (request) requested.resolve(request.request_id);
    };
    backend.setStatus = async (_id, status) => {
      states.push(status);
      if (status === "requires_action") paused.resolve();
    };
    const ctx = new Context();
    try {
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      const session = ctx.ccrv2.createSession({
        id: mode, provider: "memory", model: "text", workDir: "/workspace", systemPrompt: "Test", setup,
      });
      await session.open();
      const decision = session.approve({ callId: "call", toolName: "Bash", input: {} }, new AbortController().signal);
      const requestId = await requested.promise;
      await lookingUp.promise;
      expect(states.at(-1)).toBe("running");
      expect(states).not.toContain("requires_action");
      if (mode !== "late") {
        lookup.resolve(mode === "confirmation");
        if (mode === "confirmation") await paused.promise;
      }
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: requestId, response: { behavior: mode === "automatic" ? "deny" : "allow" },
      } });
      expect(await decision).toEqual({ allowed: mode !== "automatic", input: undefined });
      lookup.resolve(true);
      await session.updateStatus();
      expect(states.includes("requires_action")).toBe(mode === "confirmation");
      expect(states.at(-1)).toBe("running");
      expect(lookupSignal?.aborted).toBe(true);
      expect(session.approvals.size).toBe(0);
    } finally { await ctx.fiber.dispose(); }
  }
});


test("user interrupts settle the tool locally, finish successfully and survive recovery without replay", async () => {
  for (const control of [{ type: "user.interrupt" }, { type: "control_request", request_id: "interrupt", request: { subtype: "interrupt" } }]) {
    const backend = new MemoryBackend();
    const started = Promise.withResolvers<void>();
    let executions = 0;
    const executor: SandboxExecutor = {
      provision: async () => {},
      call: async (_tool, _args, signal) => {
        executions++;
        started.resolve();
        return new Promise((_, reject) => {
          if (signal.aborted) reject(signal.reason);
          else signal.addEventListener("abort", () => reject(signal.reason), { once: true });
        });
      },
    };
    const config = {
      id: "interrupt-session", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "",
      setup: async (scope: Context) => {
        await scope.plugin({ name: "interrupt-sandbox", apply(ctx: Context) { ctx.provide("sandboxExecutor", executor); } });
      },
    };
    const model = new TextModel();
    const stream = model.stream.bind(model);
    model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
      if (this.requests.length) { yield* stream(options); return; }
      this.requests.push(options);
      yield { type: "block-start", index: 0, blockType: "tool-call" };
      yield { type: "block-end", index: 0, block: {
        type: "tool-call", id: ToolCallId("cancelled-call"), name: "Bash", arguments: JSON.stringify({ command: "sleep 120" }),
      } };
      yield { type: "finish", reason: { kind: "tool-calls" } };
    };
    let ctx = new Context();
    let turn: Promise<void> | undefined;
    try {
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      ctx.llm.registerAdapter(["test"], model);
      let session = ctx.ccrv2.createSession(config);
      const events = backend.events.bind(backend);
      backend.events = async (id, payloads) => {
        await events(id, payloads);
        for (const payload of payloads) if (payload.type === "control_request" && payload.request.subtype === "can_use_tool")
          await session.control({ type: "control_response", response: {
            subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
          } });
      };
      await session.open();
      turn = session.receive("first", { content: "Run Bash" });
      await started.promise;
      await session.receive("interrupt", control);
      await turn;
      expect(session.blocked()).toBe(false);
      expect(session.abort.signal.aborted).toBe(false);
      expect(session.state.phase).toBe("completed");
      expect(session.state.toolOutcomes["cancelled-call"]).toMatchObject({ failed: true, remoteOutcome: "unknown" });
      expect(backend.output.get(config.id)!.filter((event) => event.type === "result"))
        .toEqual([{ type: "result", uuid: "first_result", subtype: "success", is_error: false }]);
      expect(JSON.stringify(backend.output.get(config.id))).not.toContain("Remote tool outcome is unknown");
      const results = backend.output.get(config.id)!.flatMap((event) => event.message?.content ?? [])
        .filter((block: Payload) => block.type === "tool_result");
      expect(results).toHaveLength(1);
      expect(results[0]).toMatchObject({ tool_use_id: "cancelled-call", is_error: true });
      await expect(session.recordApproved({ callId: "cancelled-call", name: "Bash", original: {}, effective: {}, readOnly: false }))
        .rejects.toThrow("duplicate");
      await ctx.fiber.dispose();
      ctx = new Context();
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      const resumed = new TextModel();
      ctx.llm.registerAdapter(["test"], resumed);
      session = ctx.ccrv2.createSession(config);
      await session.open();
      expect(session.blocked()).toBe(false);
      expect(session.state.toolOutcomes["cancelled-call"]?.remoteOutcome).toBe("unknown");
      await session.receive("first", { content: "Duplicate delivery" });
      expect(resumed.requests).toHaveLength(0);
      await session.receive("next", { content: "Continue" });
      expect(resumed.requests).toHaveLength(1);
      expect(executions).toBe(1);
      expect(backend.output.get(config.id)!.filter((event) => event.type === "result").every((event) => !event.is_error)).toBe(true);
    } finally {
      await ctx.fiber.dispose();
      await turn?.catch(() => {});
    }
  }
}, 15_000);

test.each([false, true])("context overflow compacts and replays without duplicate results (child=%s)", async child => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  let requests = 0;
  let replayWellFormed = false;
  const sizes: number[] = [];
  let parentRequests = 0;
  model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
    if (options.model === "parent") {
      parentRequests++;
      if (parentRequests === 1) {
        yield { type: "block-start", index: 0, blockType: "tool-call" };
        yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId("delegate-read"),
          name: "send_to_agent", arguments: JSON.stringify({ agent_name: "reader", message: "remember FACT-REAL" }) } };
        yield { type: "finish", reason: { kind: "tool-calls" } };
      } else {
        yield { type: "block-start", index: 0, blockType: "text" };
        yield { type: "block-end", index: 0, block: { type: "text", text: "done" } };
        yield { type: "finish", reason: { kind: "stop" } };
      }
      return;
    }
    sizes.push(JSON.stringify(options.messages).length);
    requests++;
    if (requests <= 4) {
      yield { type: "block-start", index: 0, blockType: "tool-call" };
      yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId(`read-${requests}`),
        name: "Read", arguments: JSON.stringify({ file_path: `/tmp/part-${requests}` }) } };
      yield { type: "finish", reason: { kind: "tool-calls" } };
    } else if (requests === 5) {
      yield { type: "finish", reason: { kind: "error", failure: { code: "CONTEXT_WINDOW_EXCEEDED", status: 400,
        message: "Maximum context length exceeded (90000 in the messages, 8192 in the completion)." } } };
    } else {
      expect(JSON.stringify(options.messages)).toContain("remember FACT-REAL");
      expect(sizes.at(-1)!).toBeLessThan(sizes[4]! - 50000);
      const tools = options.messages.filter(message => message.role === "tool");
      expect(tools).toHaveLength(4);
      replayWellFormed = tools.every(message => message.content.every(block => block.type !== "text" || block.text.isWellFormed()));
      expect(new Set(tools.map(message => message.toolCallId)).size).toBe(4);
      yield { type: "block-start", index: 0, blockType: "text" };
      yield { type: "block-end", index: 0, block: { type: "text", text: "FACT-REAL" } };
      yield { type: "finish", reason: { kind: "stop" } };
    }
  };
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession({ id: "compaction", provider: "test", model: child ? "parent" : "text",
      subagents: child ? { inlineAgents: false, agents: [{ name: "reader", description: "Reader", systemPrompt: "Read", model: "text", tools: ["Read"] }] } : undefined,
      workDir: "/workspace", systemPrompt: "", tools: ["Read"], setup: async scope => {
        await scope.plugin({ name: "read-sandbox", apply(scope: Context) { scope.provide("sandboxExecutor", {
          async provision() {}, async call() { return { content: [{ type: "text", text: "original-file-data:" + "\u{100000}".repeat(30000) + ":tail" }] }; },
        }); } });
      } });
    await session.open();
    spyOn(session, "approve").mockResolvedValue({ allowed: true });
    await session.receive("pressure", { content: "remember FACT-REAL" });
    expect(requests).toBe(6);
    expect(replayWellFormed).toBe(true);
    const output = backend.output.get(session.id)!;
    expect(output.filter(event => event.type === "agent.thread_context_compacted")).toEqual([
      expect.objectContaining({ pre_compaction_tokens: 90000, ...(child ? { session_thread_id: session.state.threads![0]!.id } : {}) }),
    ]);
    const results = output.flatMap(event => event.message?.content ?? []).filter(block => block.type === "tool_result" && block.tool_use_id.startsWith("read-"));
    expect(results).toHaveLength(4);
    expect(results.every(block => JSON.stringify(block).includes("original-file-data"))).toBe(true);
    const native = child ? ctx.sessions.get(SessionId(session.state.threads![0]!.id))! : session.runtime.agent.session;
    if (child) expect(session.runtime.agent.session.snapshotEvents().some(event => event.type === "tool/result" && event.surfaceOp !== "append")).toBe(false);
    const replacements = native.snapshotEvents().filter(event => event.type === "tool/result" && event.surfaceOp !== "append");
    expect(replacements).toHaveLength(2);
    const replay = ctx.sessions.create(undefined, { seed: native.snapshotEvents() });
    expect(replay.deriveMessages()).toEqual(native.deriveMessages());
    expect(native.snapshotEvents().filter(event => event.type === "tool/result" && event.surfaceOp === "append")).toHaveLength(4);
    const failure = { code: "CONTEXT_WINDOW_EXCEEDED", message: "context too long" };
    let published = false;
    const original = ctx.sessions.create(undefined, { seed: native.snapshotEvents().slice(0, replacements[0]!.seq) });
    const recovery = new ContextRecovery();
    expect(await recovery.recover(original, { code: "HTTP_ERROR", message: "network" }, 1, 1,
      async () => {}, async () => { published = true; })).toBe(false);
    await expect(recovery.recover(original, failure, 1, 1,
      async () => { throw new Error("Not durable"); }, async () => { published = true; })).rejects.toThrow("Not durable");
    expect(published).toBe(false);
    expect(await recovery.recover(original, failure, 1, 1,
      async () => {}, async () => { published = true; })).toBe(false);
    expect(await new ContextRecovery().recover(replay, failure, 1, 1,
      async () => {}, async () => { published = true; })).toBe(false);
    await session.close();
  } finally { await ctx.fiber.dispose(); }
});
