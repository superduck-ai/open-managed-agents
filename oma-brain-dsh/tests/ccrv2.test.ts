import { nativeOutcomes, completedInputs } from "./session-log.ts";
import { eventually } from "./backend.ts";
import { expect, spyOn, test } from "bun:test";
import { SESSION_FORMAT_VERSION, SessionId, SessionSeq } from "@deepseek-ai/dsh-session";
import { Context } from "@deepseek-ai/cordis";
import { freezeMessage, MessageId, createUserMessage, LlmAdapter, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { mountKernel } from "../src/kernel.ts";
import * as CcrBridge from "../src/ccrv2/index.ts";
import type { CcrBackend, CcrPersistArgs } from "../src/ccrv2/index.ts";
import type { SandboxExecutor } from "../src/runtime/contracts.ts";
import type { Payload } from "../src/ccrv2/protocol.ts";
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
  async privateEvents(id: string, options?: { subagents?: boolean; agentId?: string }) {
    return structuredClone((this.logs.get(id) ?? []).filter((event) =>
      options?.agentId !== undefined ? event.agent_id === options.agentId : options?.subagents ? !!event.agent_id : !event.agent_id));
  }
  async persist(id: string, ...[type, data, agentId]: CcrPersistArgs) {
    const log = this.logs.get(id) ?? [];
    log.push(structuredClone({ type, data, ...(agentId ? { agent_id: agentId } : {}) }));
    this.logs.set(id, log);
  }
  async events(id: string, payloads: Payload[]) {
    const previous = this.output.get(id) ?? [];
    const seen = new Set(previous.map(event => event.uuid));
    this.output.set(id, [...previous, ...structuredClone(payloads.filter(event => !seen.has(event.uuid)))]);
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

test("log recovery rejects unsupported headers, missing headers, gaps and conflicting events", async () => {
  const header = { version: SESSION_FORMAT_VERSION, id: SessionId("invalid"), createdAt: 0, isSeeded: false } as const;
  const event = { type: "turn/start" as const, seq: SessionSeq(0), time: 0, data: { turn: 1 } };
  const storedHeader = { type: "dsh_log", data: header };
  const storedEvent = { type: "dsh_log", data: event };
  const cases: [Payload[], string][] = [
    [[{ ...storedHeader, data: { ...header, version: 999 } }], "v999"],
    [[storedEvent], "Missing dsh log header"],
    [[storedHeader, { ...storedEvent, data: { ...event, seq: SessionSeq(1) } }], ""],
    [[storedHeader, storedEvent, { ...storedEvent, data: { ...event, time: 1 } }], "Conflicting dsh log event"],
  ];
  for (const [payloads, error] of cases) {
    const backend = new MemoryBackend();
    backend.logs.set("invalid", payloads);
    const ctx = new Context();
    try {
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      await expect(ctx.ccrReplication.ensure(SessionId("invalid"))).rejects.toThrow(error);
    } finally {
      await ctx.fiber.dispose();
    }
  }
});

test("startup explicitly rejects legacy persistence instead of creating blank history", async () => {
  const payloads: Payload[] = [
    ...["dsh_log", "dsh_header", "dsh_brain_state", "ccrv2_delivery", "grok_checkpoint"].map((subtype) => ({ type: "system", subtype, data: {} })),
    { type: "dsh_log", data: { header: {}, events: [] } },
    { type: "grok_checkpoint", data: {} },
  ];
  for (const payload of payloads) {
    const backend = new MemoryBackend();
    backend.logs.set("legacy", [payload]);
    const ctx = new Context();
    try {
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      const session = ctx.ccrv2.createSession({ id: "legacy", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", setup });
      await expect(session.open()).rejects.toThrow("Unsupported persistence format for session legacy");
      expect(backend.logs.get("legacy")).toHaveLength(1);
    } finally { await ctx.fiber.dispose(); }
  }
});

test("persistence stores a separate header and individual events and restores duplicate seqs once", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const header = { version: SESSION_FORMAT_VERSION, id: SessionId("event-log"), createdAt: 0, isSeeded: false, delegationDepth: 0 } as const;
  const events = [
    { type: "turn/start" as const, seq: SessionSeq(0), time: 0, data: { turn: 1 } },
    { type: "turn/end" as const, seq: SessionSeq(1), time: 1, data: { turn: 1, reason: { kind: "completed" as const } } },
  ];
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    const handle = await ctx.sessionPersistence.create(header);
    await handle.append(events);
    await handle.close();
    await ctx.ccrReplication.flush(SessionId("event-log"));
    const logs = backend.logs.get("event-log")!;
    expect(logs.map((payload) => payload.type)).toEqual(["dsh_log", "dsh_log", "dsh_log"]);
    expect(logs[0]!.data).toEqual(header);
    expect(logs.slice(1).map((payload) => payload.data)).toEqual(events);
    logs.push(structuredClone(logs[1]!));
  } finally {
    await ctx.fiber.dispose();
  }
  const restored = new Context();
  try {
    await mountKernel(restored);
    await restored.plugin(CcrBridge, { backend });
    await restored.ccrReplication.ensure(header.id);
    const handle = await restored.sessionPersistence.open(header.id, "read");
    expect(handle.header).toEqual(header);
    expect((await handle.read()).events).toEqual(events);
    await handle.close();
  } finally {
    await restored.fiber.dispose();
  }
});

test("completed blocks flush earlier previews and input acknowledgement waits for HTTP confirmation", async () => {
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
    if (payloads.some((payload) => ["event_start", "event_delta"].includes(payload.type))) previewCalls++;
    await client.events(id, payloads);
    await events(id, payloads);
  };
  const persist = backend.persist.bind(backend);
  backend.persist = async (id, ...args) => {
    await persist(id, ...args);
    const [, data] = args;
    if ("seq" in data && data.type === "turn/end") completed.resolve();
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
    await eventually(() => previewCalls === 2);
    await completed.promise;
    expect(previewCalls).toBe(2);
    expect(timers.size).toBe(0);
    expect(session.pendingEvents.length).toBeGreaterThan(0);
    expect(backend.acknowledged).toEqual([]);
    await sending.promise;
    const previews = batches.flat().filter((event) => event.ephemeral);
    expect(previews.map((event) => event.payload.type)).toEqual(["event_start", "event_delta", "event_delta"]);
    expect(previews.filter((event) => event.payload.type === "event_delta").map((event) => event.payload.delta))
      .toEqual(["A", "B"].map((text) => ({ type: "content_delta", index: 0, content: { type: "text", text } })));
    expect(session.pendingEvents.length).toBeGreaterThan(0);
    expect(backend.acknowledged).toEqual([]);
    response.resolve(); await turn;
    expect(session.pendingEvents).toEqual([]);
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
      session.agent.session.append("user/message", createUserMessage({ source: { kind: "user" }, content: [{ type: "text", text: "Native event" }] }), { surfaceOp: "append" });
      backend.persist = async () => { throw new Error("Lease expired"); };
      await expect(session.close()).rejects.toThrow();
      expect(() => ctx.ccrv2.forget(session.id)).not.toThrow();
      backend.persist = persist;
    }
  } finally {
    await ctx.fiber.dispose();
  }
});

test("startup clears task_summary after recovery persistence and before status or publication, including interrupted inputs", async () => {
  for (const interrupted of [false, true]) {
    const backend = new MemoryBackend();
    const events: Payload[] = [
      { type: "turn/start", seq: 0, time: 0, data: { turn: 1 } },
      { type: "user/message", seq: 1, time: 1, surfaceOp: "append", data: freezeMessage({ id: MessageId("interrupted-input"), role: "user", source: { kind: "user" }, content: [{ type: "text", text: "Native input" }] }) },
      ...interrupted ? [] : [{ type: "turn/end", seq: 2, time: 2, data: { turn: 1, reason: { kind: "completed" } } }],
    ];
    backend.logs.set("recovery", [
      { type: "dsh_log", data: { version: SESSION_FORMAT_VERSION, id: "recovery", createdAt: 0, isSeeded: false } },
      ...events.map(data => ({ type: "dsh_log", data })),
    ]);
    const order: string[] = [];
    const started = Promise.withResolvers<void>();
    const confirmed = Promise.withResolvers<void>();
    const persist = backend.persist.bind(backend);
    backend.persist = async (id, ...args) => { order.push(`persist:${args[0]}`); await persist(id, ...args); };
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
      if (interrupted) expect(order).toContain("persist:dsh_log");
      confirmed.resolve();
      await opening;
      expect(session.abort.signal.aborted).toBe(false);
      expect(order.indexOf("publish")).toBeGreaterThan(order.indexOf("metadata"));
      expect(order.indexOf("status:idle")).toBeGreaterThan(order.indexOf("metadata"));
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
    await eventually(() => backend.acknowledged.includes("model-session:change"));
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
    expect(states.map((state) => state.status)).toEqual(["idle", "running", "idle"]);
    expect(session.approvals.size).toBe(0);
  } finally {
    await ctx.fiber.dispose();
  }
});

test("child history resumes from individually persisted scoped events", async () => {
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
  expect(logs.some((event) => event.agent_id === threadId! && event.type === "dsh_log")).toBe(true);
  const second = await mount();
  try {
    await second.session.subagents.send({ thread_id: threadId!, message: "child follow-up" }, second.session.abort.signal);
    const history = JSON.stringify(second.model.requests[0]!.messages);
    expect(history).toContain("original child task");
    expect(history).toContain("child follow-up");
    expect(history).not.toContain("parent task");
  } finally { await second.ctx.fiber.dispose(); }
  const third = await mount();
  try {
    await third.session.subagents.send({ thread_id: threadId!, message: "third child task" }, third.session.abort.signal);
    const history = JSON.stringify(third.model.requests[0]!.messages);
    expect(history).toContain("original child task");
    expect(history).toContain("child follow-up");
    expect(history).toContain("third child task");
  } finally { await third.ctx.fiber.dispose(); }
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
    expect(a.inputId).toBe("");
    await a.receive("input_a", { content: "first session" });
    await b.receive("input_b", { content: "second session" });
    expect(backend.acknowledged).toEqual(["a:input_a", "b:input_b"]);
    expect(completedInputs(a)).toEqual(["input_a"]);
    expect(completedInputs(b)).toEqual(["input_b"]);
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
    expect(a.pendingEvents).toEqual([]);
  } finally {
    await restored.ctx.fiber.dispose();
  }
});

async function mountMediaKernel(backend: MemoryBackend) {
  const ctx = new Context();
  await mountKernel(ctx);
  const model = new TextModel();
  await ctx.plugin({
    inject: ["llm"],
    apply(scope: Context) { scope.llm.registerAdapter(["memory"], model); },
  });
  await ctx.plugin(CcrBridge, { backend });
  const session = ctx.ccrv2.createSession({
    id: "a", provider: "memory", model: "text", workDir: "/workspace",
    systemPrompt: "Standalone test instructions", setup,
  });
  await session.open();
  return { ctx, model, session };
}

test("flushes without new log events do not write headers or events", async () => {
  const backend = new MemoryBackend();
  const { ctx, session } = await mountMediaKernel(backend);
  try {
    await session.receive("first", { content: "hello" });
    const previous = (backend.logs.get("a") ?? []).filter((event) => event.type === "dsh_log");
    expect(previous.length).toBeGreaterThan(0);
    await session.checkpoint();
    await session.checkpoint();
    const logRecords = () => (backend.logs.get("a") ?? []).filter((event) => event.type === "dsh_log");
    const logs = logRecords();
    expect(logs.filter((event) => "version" in event.data)).toHaveLength(1);
    expect(logs[0]!.data).toHaveProperty("version", SESSION_FORMAT_VERSION);
    expect(logs.slice(1).every((payload) => "seq" in payload.data)).toBe(true);
    const count = logs.length;
    await session.checkpoint();
    expect(logRecords()).toHaveLength(count);
  } finally {
    await ctx.fiber.dispose();
  }
});

test("a redelivered control input is acknowledged without repeating its effect", async () => {
  const backend = new MemoryBackend();
  const { ctx, session } = await mountMediaKernel(backend);
  try {
    const request = { type: "control_request", request_id: "req-1", request: { subtype: "set_model", model: "other" } };
    await session.receive("control-1", request);
    await session.receive("control-1", request);
    await eventually(() => backend.acknowledged.length === 2);
    const responses = (backend.output.get("a") ?? []).filter((event) => event.type === "control_response");
    expect(responses).toHaveLength(1);
    expect(backend.acknowledged).toEqual(["a:control-1", "a:control-1"]);
  } finally {
    await ctx.fiber.dispose();
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
      expect(states.at(-1)).toBe("idle");
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
      expect(session.abort.signal.aborted).toBe(false);
      expect(session.abort.signal.aborted).toBe(false);
      expect(completedInputs(session)).toContain(session.inputId);
      expect(nativeOutcomes(session)["cancelled-call"]).toMatchObject({ failed: true });
      expect(backend.output.get(config.id)!.filter((event) => event.type === "result"))
        .toEqual([{ type: "result", uuid: "first_result", subtype: "success", is_error: false }]);
      expect(JSON.stringify(backend.output.get(config.id))).not.toContain("Remote tool outcome is unknown");
      const results = backend.output.get(config.id)!.flatMap((event) => event.message?.content ?? [])
        .filter((block: Payload) => block.type === "tool_result");
      expect(results).toHaveLength(1);
      expect(results[0]).toMatchObject({ tool_use_id: "cancelled-call", is_error: true });
      await ctx.fiber.dispose();
      ctx = new Context();
      await mountKernel(ctx);
      await ctx.plugin(CcrBridge, { backend });
      const resumed = new TextModel();
      ctx.llm.registerAdapter(["test"], resumed);
      session = ctx.ccrv2.createSession(config);
      await session.open();
      expect(session.abort.signal.aborted).toBe(false);
      expect(nativeOutcomes(session)["cancelled-call"]?.failed).toBe(true);
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

test("repeated clean runtime flushes perform no backend writes after a large journal and dirty flushes send only new events", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], new TextModel());
    const session = ctx.ccrv2.createSession({ id: "clean-flush", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", tools: [], setup });
    await session.open();
    const writes = spyOn(backend, "persist");
    const native = session.runtime.agent.session;
    for (let index = 0; index < 256; index++) native.append("user/message", createUserMessage({
      source: { kind: "user" }, content: [{ type: "text", text: `${index}:` + "x".repeat(16384) }],
    }), { surfaceOp: "append" });
    await session.flushEvents();
    expect(writes).toHaveBeenCalledTimes(256);
    const firstWrite = writes.mock.calls[0]!;
    expect(firstWrite[1]).toBe("dsh_log");
    if (firstWrite[1] !== "dsh_log") throw new Error("Expected journal write");
    expect(writes.mock.calls.map(call => call[2])).toEqual(native.snapshotEvents().slice(-256));
    writes.mockClear();
    for (let index = 0; index < 100; index++) await session.flushEvents();
    expect(writes).not.toHaveBeenCalled();
    const event = native.append("user/message", createUserMessage({
      source: { kind: "user" }, content: [{ type: "text", text: "next delta" }],
    }), { surfaceOp: "append" });
    await session.flushEvents();
    expect(writes).toHaveBeenCalledTimes(1);
    const delta = writes.mock.calls[0]!;
    expect(delta[1]).toBe("dsh_log");
    if (delta[1] !== "dsh_log") throw new Error("Expected journal delta");
    expect(delta[2]).toEqual(event);
  } finally { await ctx.fiber.dispose(); }
});

test("native compaction waits for journal persistence before notification and next request", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  let requests = 0;
  let failPersistence = true;
  const persist = backend.persist.bind(backend);
  backend.persist = async (id, ...args) => {
    if (failPersistence && args[0] === "dsh_log" && "type" in args[1] && args[1].type === "compaction/end")
      throw new Error("compaction persistence unavailable");
    await persist(id, ...args);
  };
  model.resolveModel = async (provider, id) => ({ provider, id, name: id, context: { contextWindow: 128000 }, defaultMaxTokens: 8192 });
  model.stream = async function* (options): AsyncIterable<StreamChunk> {
    if (options.purpose !== "compaction") requests++;
    yield { type: "block-end", index: 0, block: { type: "text", text: options.purpose === "compaction" ? "Previous task details." : "a".repeat(150000) } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession({ id: "durable-compaction", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", tools: [], setup });
    await session.open();
    await session.receive("first", { content: "u".repeat(150000) });
    await expect(session.receive("second", { content: "continue" })).rejects.toThrow("compaction persistence unavailable");
    expect(requests).toBe(1);
    expect(backend.output.get(session.id)!.filter(event => event.type === "agent.thread_context_compacted")).toHaveLength(0);
    failPersistence = false;
    await session.flushEvents();
    expect(backend.output.get(session.id)!.filter(event => event.type === "agent.thread_context_compacted")).toHaveLength(1);
    await session.close();
  } finally { failPersistence = false; await ctx.fiber.dispose(); }
});

async function compactedSessionFixture() {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  model.stream = async function* (options): AsyncIterable<StreamChunk> {
    yield { type: "block-end", index: 0, block: { type: "text", text: options.purpose === "compaction" ? "Previous task details." : "a".repeat(12000) } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  await mountKernel(ctx);
  await ctx.plugin(CcrBridge, { backend });
  ctx.llm.registerAdapter(["test"], model);
  const config = { id: "compaction-notification", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", tools: [], setup };
  const session = ctx.ccrv2.createSession(config);
  await session.open();
  await session.receive("first", { content: "u".repeat(12000) });
  expect(await ctx.compaction.compactNow(session.runtime.agent, new AbortController().signal)).not.toBeNull();
  return { backend, ctx, model, config, session };
}

test.each(["journal", "publication", "callback"] as const)("compaction notification retries after failed %s without duplicate enqueue", async failure => {
  const { backend, ctx, session } = await compactedSessionFixture();
  const persist = backend.persist.bind(backend);
  const events = backend.events.bind(backend);
  const notify = session.contextCompacted.bind(session);
  let failing = true;
  let deliveredId: string | undefined;
  backend.persist = async (id, ...args) => {
    if (failing && failure === "journal" && "type" in args[1] && args[1].type === "compaction/end") throw new Error("notification unavailable");
    await persist(id, ...args);
  };
  backend.events = async (id, payloads) => {
    const notification = payloads.find(event => event.type === "agent.thread_context_compacted");
    if (notification) deliveredId = notification.uuid;
    if (failing && failure === "publication") throw new Error("notification unavailable");
    await events(id, payloads);
  };
  session.contextCompacted = async (...args) => {
    if (failing && failure === "callback") throw new Error("notification unavailable");
    await notify(...args);
  };
  try {
    await expect(session.flushEvents()).rejects.toThrow("notification unavailable");
    const queuedId = session.pendingEvents.find(event => event.type === "agent.thread_context_compacted")?.uuid;
    const attemptedId = deliveredId;
    failing = false;
    await session.flushEvents();
    const notifications = backend.output.get(session.id)!.filter(event => event.type === "agent.thread_context_compacted");
    expect(notifications).toHaveLength(1);
    if (queuedId ?? attemptedId) expect(notifications[0]!.uuid).toBe(queuedId ?? attemptedId);
    await session.flushEvents();
    expect(backend.output.get(session.id)!.filter(event => event.type === "agent.thread_context_compacted")).toHaveLength(1);
  } finally { failing = false; await ctx.fiber.dispose(); }
});

test("restart recovers a compaction notification when its journal is durable but publication failed", async () => {
  const { backend, ctx, session, model, config } = await compactedSessionFixture();
  const resumedContext = new Context();
  const publish = backend.events.bind(backend);
  backend.events = async (id, events) => {
    if (events.some(event => event.type === "agent.thread_context_compacted")) throw new Error("notification publication unavailable");
    await publish(id, events);
  };
  try {
    await expect(session.flushEvents()).rejects.toThrow("notification publication unavailable");
    const queuedId = session.pendingEvents.find(event => event.type === "agent.thread_context_compacted")!.uuid;
    const recovered = new MemoryBackend();
    for (const [id, logs] of backend.logs) recovered.logs.set(id, structuredClone(logs));
    for (const [id, output] of backend.output) recovered.output.set(id, structuredClone(output));
    await mountKernel(resumedContext);
    await resumedContext.plugin(CcrBridge, { backend: recovered });
    resumedContext.llm.registerAdapter(["test"], model);
    const resumed = resumedContext.ccrv2.createSession(config);
    await resumed.open();
    expect(recovered.output.get(config.id)!.filter(event => event.type === "agent.thread_context_compacted")).toEqual([
      expect.objectContaining({ uuid: queuedId }),
    ]);
    expect(resumed.runtime.agent.session.deriveMessages()).toEqual(session.runtime.agent.session.deriveMessages());
  } finally { backend.events = publish; await resumedContext.fiber.dispose(); await ctx.fiber.dispose(); }
});

test("native overflow recovery stops after one unsuccessful retry", async () => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  let requests = 0;
  let summaries = 0;
  model.stream = async function* (options): AsyncIterable<StreamChunk> {
    if (options.purpose === "compaction") {
      summaries++;
      yield { type: "block-end", index: 0, block: { type: "text", text: "A prior request needs continuation." } };
      yield { type: "finish", reason: { kind: "stop" } };
      return;
    }
    requests++;
    if (requests === 1) {
      yield { type: "block-end", index: 0, block: { type: "text", text: "previous response " + "z".repeat(10000) } };
      yield { type: "finish", reason: { kind: "stop" } };
    } else yield { type: "finish", reason: { kind: "error", failure: { code: "CONTEXT_WINDOW_EXCEEDED", message: "too large" } } };
  };
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession({ id: "bounded-overflow", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "", tools: [], setup });
    await session.open();
    await session.receive("first", { content: "previous request" });
    await session.receive("second", { content: "continue" });
    expect(requests).toBe(3);
    expect(summaries).toBe(1);
    expect(backend.output.get(session.id)!.filter(event => event.type === "result").at(-1)?.is_error).toBe(true);
    await session.close();
  } finally { await ctx.fiber.dispose(); }
});

test.each([true, false])("native pressure compaction handles summary failure and text history (failure=%s)", async summaryFailure => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  let requests = 0;
  let summaries = 0;
  const model = new TextModel();
  model.resolveModel = async (provider, id) => ({ provider, id, name: id, context: { contextWindow: 128000 }, defaultMaxTokens: 8192 });
  model.stream = async function* (options): AsyncIterable<StreamChunk> {
    if (options.purpose === "compaction") {
      summaries++;
      if (summaryFailure) throw new Error("summary unavailable");
      expect(options.maxTokens).toBe(65536);
      yield { type: "block-end", index: 0, block: { type: "text", text: "Remember FACT-REAL and continue the task." } };
      yield { type: "finish", reason: { kind: "stop" } };
      return;
    }
    requests++;
    expect(options.maxTokens).toBe(8192);
    if (requests === 2 && !summaryFailure) {
      expect(JSON.stringify(options.messages)).toContain("<compacted-summary>");
      expect(backend.output.get("native-pressure")?.filter(event => event.type === "agent.thread_context_compacted")).toHaveLength(1);
      const persisted = backend.logs.get("native-pressure") ?? [];
      expect(persisted.some(log => log.data?.type === "compaction/end")).toBe(true);
    }
    yield { type: "block-end", index: 0, block: { type: "text", text: requests === 1 ? "a".repeat(150000) : "done" } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const config = { id: "native-pressure", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "stable system", tools: [], setup };
    const session = ctx.ccrv2.createSession(config);
    await session.open();
    await session.receive("first", { content: "remember FACT-REAL " + "u".repeat(150000) });
    await session.receive("second", { content: "continue" });
    expect(requests).toBe(2);
    expect(summaries).toBe(1);
    const events = session.runtime.agent.session.snapshotEvents();
    const endings = events.filter(event => event.type === "compaction/end");
    expect(endings).toHaveLength(1);
    expect(endings[0]!.data.error !== undefined).toBe(summaryFailure);
    const notifications = backend.output.get(config.id)!.filter(event => event.type === "agent.thread_context_compacted");
    expect(notifications).toHaveLength(summaryFailure ? 0 : 1);
    if (!summaryFailure) {
      const derived = session.runtime.agent.session.deriveMessages();
      expect(JSON.stringify(derived).length).toBeLessThan(200000);
      await session.close();
      const resumedContext = new Context();
      try {
        await mountKernel(resumedContext);
        await resumedContext.plugin(CcrBridge, { backend });
        resumedContext.llm.registerAdapter(["test"], model);
        const resumed = resumedContext.ccrv2.createSession(config);
        await resumed.open();
        expect(resumed.runtime.agent.session.deriveMessages()).toEqual(derived);
        await resumed.receive("third", { content: "continue after restart" });
        expect(summaries).toBe(1);
        expect(backend.output.get(config.id)!.filter(event => event.type === "agent.thread_context_compacted")).toHaveLength(1);
        await resumed.close();
      } finally { await resumedContext.fiber.dispose(); }
    } else await session.close();
  } finally { await ctx.fiber.dispose(); }
});

test.each([false, true])("native compaction recovers overflow and preserves root/child history (child=%s)", async child => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  let requests = 0;
  let parentRequests = 0;
  let summaries = 0;
  model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
    if (options.purpose === "compaction") {
      summaries++;
      expect(options.maxTokens).toBe(65536);
      expect(JSON.stringify(options.messages)).toContain("remember FACT-REAL");
      yield { type: "block-end", index: 0, block: { type: "text", text: "User requested remember FACT-REAL. Four Bash calls completed." } };
      yield { type: "finish", reason: { kind: "stop" } };
      return;
    }
    if (options.model === "parent" && parentRequests++ === 0) {
      yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId("delegate"),
        name: "send_to_agent", arguments: JSON.stringify({ agent_name: "reader", message: "remember FACT-REAL" }) } };
      yield { type: "finish", reason: { kind: "tool-calls" } };
      return;
    }
    if (options.model !== "parent") {
      requests++;
      if (requests <= 4) {
        yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId(`bash-${requests}`),
          name: "Bash", arguments: JSON.stringify({ command: `cat part-${requests}` }) } };
        yield { type: "finish", reason: { kind: "tool-calls" } };
        return;
      }
      if (requests === 5) {
        yield { type: "finish", reason: { kind: "error", failure: { code: "CONTEXT_WINDOW_EXCEEDED", status: 400,
          message: "Maximum context length exceeded" } } };
        return;
      }
      expect(JSON.stringify(options.messages)).toContain("<compacted-summary>");
      const logs = backend.logs.get("native-compaction") ?? [];
      expect(logs.some(log => log.data?.type === "compaction/end")).toBe(true);
    }
    yield { type: "block-end", index: 0, block: { type: "text", text: "FACT-REAL" } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession({ id: "native-compaction", provider: "test", model: child ? "parent" : "text",
      subagents: child ? { inlineAgents: false, agents: [{ name: "reader", description: "Reader", systemPrompt: "Read", model: "text", tools: ["Bash"] }] } : undefined,
      workDir: "/workspace", systemPrompt: "", tools: ["Bash"], setup: async scope => {
        await scope.plugin({ name: "compaction-sandbox", apply(scope: Context) { scope.provide("sandboxExecutor", {
          async provision() {}, async call() { return { content: [{ type: "text", text: "original:" + "x".repeat(60000) + ":tail" }] }; },
        }); } });
      } });
    await session.open();
    spyOn(session, "approve").mockResolvedValue({ allowed: true });
    await session.receive("pressure", { content: "remember FACT-REAL" });
    expect(requests).toBe(6);
    expect(summaries).toBe(1);
    const output = backend.output.get(session.id)!;
    const notifications = output.filter(event => event.type === "agent.thread_context_compacted");
    expect(notifications).toHaveLength(1);
    expect(notifications[0]!.pre_compaction_tokens).toBeGreaterThan(0);
    if (child) expect(notifications[0]!.session_thread_id).toBe(session.subagents.threads![0]!.id);
    const results = output.flatMap(event => event.message?.content ?? []).filter(block => block.type === "tool_result" && block.tool_use_id.startsWith("bash-"));
    expect(results).toHaveLength(4);
    expect(results.every(block => JSON.stringify(block).includes("original:"))).toBe(true);
    const native = child ? ctx.sessions.get(SessionId(session.subagents.threads![0]!.id))! : session.runtime.agent.session;
    expect(native.snapshotEvents().filter(event => event.type === "compaction/prune")).toHaveLength(4);
    expect(native.snapshotEvents().filter(event => event.type === "compaction/summary")).toHaveLength(1);
    const replay = ctx.sessions.create(undefined, { seed: native.snapshotEvents() });
    expect(replay.deriveMessages()).toEqual(native.deriveMessages());
    expect(native.snapshotEvents().filter(event => event.type === "tool/result" && event.surfaceOp === "append")).toHaveLength(4);
    await session.close();
  } finally { await ctx.fiber.dispose(); }
});

test("native tool errors remain resumable without operator reconciliation", async () => {
  const backend = new MemoryBackend();
  let executions = 0;
  const executor: SandboxExecutor = {
    provision: async () => {},
    call: async () => { executions++; throw new Error("response lost"); },
  };
  const config = {
    id: "reconcile-session", provider: "test", model: "text", workDir: "/workspace", systemPrompt: "",
    setup: async (scope: Context) => {
      await scope.plugin({ name: "reconcile-sandbox", apply(ctx: Context) { ctx.provide("sandboxExecutor", executor); } });
    },
  };
  const toolTurn = (model: TextModel) => {
    const stream = model.stream.bind(model);
    model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
      if (this.requests.length) { yield* stream(options); return; }
      this.requests.push(options);
      yield { type: "block-start", index: 0, blockType: "tool-call" };
      yield { type: "block-end", index: 0, block: {
        type: "tool-call", id: ToolCallId("lost-call"), name: "Bash", arguments: JSON.stringify({ command: "deploy" }),
      } };
      yield { type: "finish", reason: { kind: "tool-calls" } };
    };
    return model;
  };
  const mount = async (model: TextModel) => {
    const ctx = new Context();
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession(config);
    const events = backend.events.bind(backend);
    backend.events = async (id, payloads) => {
      await events(id, payloads);
      for (const payload of payloads) if (payload.type === "control_request" && payload.request.subtype === "can_use_tool")
        await session.control({ type: "control_response", response: {
          subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
        } });
    };
    return { ctx, session, restore: () => { backend.events = events; } };
  };

  const first = await mount(toolTurn(new TextModel()));
  try {
    await first.session.open();
    await first.session.receive("first", { content: "Deploy" });
    expect(executions).toBe(1);
    expect(nativeOutcomes(first.session)["lost-call"]?.text).toContain("Do not retry blindly");
    expect(first.session.abort.signal.aborted).toBe(false);
    expect(first.session).not.toHaveProperty("delivery");
  } finally { first.restore(); await first.ctx.fiber.dispose(); }
  const model = new TextModel();
  const resumed = await mount(model);
  try {
    await resumed.session.open();
    await resumed.session.receive("first", { content: "Duplicate" });
    expect(model.requests).toHaveLength(0);
    await resumed.session.receive("next", { content: "Check the deployment" });
    expect(model.requests).toHaveLength(1);
    expect(JSON.stringify(model.requests[0]!.messages)).toContain("Do not retry blindly");
    expect(executions).toBe(1);
    expect(backend.metadata.get(config.id)).not.toHaveProperty("unknown_tool_outcomes");
  } finally { resumed.restore(); await resumed.ctx.fiber.dispose(); }
});

test.each([false, true])("next native request waits for tool-result publication (child=%s)", async child => {
  const backend = new MemoryBackend();
  const ctx = new Context();
  const model = new TextModel();
  const publishing = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  let toolRequests = 0;
  let parentRequests = 0;
  let resumed = false;
  const events = backend.events.bind(backend);
  backend.events = async (id, payloads) => {
    if (payloads.some(event => event.message?.content?.some((block: Payload) => block.type === "tool_result" && block.tool_use_id === "ordered-call"))) {
      publishing.resolve();
      await release.promise;
    }
    await events(id, payloads);
  };
  model.stream = async function* (options: GenerateOptions): AsyncIterable<StreamChunk> {
    if (options.model === "parent" && parentRequests++ === 0) {
      yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId("delegate-order"), name: "send_to_agent",
        arguments: JSON.stringify({ agent_name: "reader", message: "Run Bash" }) } };
      yield { type: "finish", reason: { kind: "tool-calls" } };
      return;
    }
    if (options.model !== "parent") {
      if (toolRequests++ === 0) {
        yield { type: "block-end", index: 0, block: { type: "tool-call", id: ToolCallId("ordered-call"), name: "Bash", arguments: '{"command":"printf ordered"}' } };
        yield { type: "finish", reason: { kind: "tool-calls" } };
        return;
      }
      resumed = true;
      expect((backend.output.get("request-order") ?? []).flatMap(event => event.message?.content ?? [])
        .some(block => block.type === "tool_result" && block.tool_use_id === "ordered-call")).toBe(true);
    }
    yield { type: "block-end", index: 0, block: { type: "text", text: "done" } };
    yield { type: "finish", reason: { kind: "stop" } };
  };
  let turn: Promise<void> | undefined;
  try {
    await mountKernel(ctx);
    await ctx.plugin(CcrBridge, { backend });
    ctx.llm.registerAdapter(["test"], model);
    const session = ctx.ccrv2.createSession({ id: "request-order", provider: "test", model: child ? "parent" : "text",
      workDir: "/workspace", systemPrompt: "", tools: ["Bash"],
      subagents: child ? { inlineAgents: false, agents: [{ name: "reader", description: "Reader", systemPrompt: "Read", model: "text", tools: ["Bash"] }] } : undefined,
      setup: async scope => { await scope.plugin({ name: "ordered-sandbox", apply(scope: Context) {
        scope.provide("sandboxExecutor", { async provision() {}, async call() { return { content: [{ type: "text", text: "ordered" }] }; } });
      } }); } });
    await session.open();
    spyOn(session, "approve").mockResolvedValue({ allowed: true });
    turn = session.receive("ordered-input", { content: "Run Bash" });
    await publishing.promise;
    expect(resumed).toBe(false);
    release.resolve();
    await turn;
    expect(resumed).toBe(true);
    await session.close();
  } finally { release.resolve(); await turn?.catch(() => {}); await ctx.fiber.dispose(); }
});
