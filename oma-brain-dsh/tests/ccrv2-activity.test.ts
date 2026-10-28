import { completedInputs } from "./session-log.ts";
import { logData } from "./persistence-fixtures.ts";
import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { CcrActivity, isEnvTruthy, type CcrActivityClock } from "../src/ccrv2/activity.ts";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat, type CcrLeaseFailure } from "../src/ccrv2/heartbeat.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, textEvents, toolEvents } from "./backend.ts";

class ManualClock implements CcrActivityClock {
  readonly pending = new Set<{ callback: () => void; delayMs: number }>();
  schedule(callback: () => void, delayMs: number) {
    const task = { callback, delayMs };
    this.pending.add(task);
    return () => { this.pending.delete(task); };
  }
  fire() {
    const task = [...this.pending][0];
    if (!task) throw new Error("No activity timer");
    expect(task.delayMs).toBe(30_000);
    this.pending.delete(task);
    task.callback();
  }
}

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

test("activity keepalives follow the reference opt-in switch and only run while active", async () => {
  for (const value of [undefined, "", "0", "false", "off", "no", "unexpected", false]) expect(isEnvTruthy(value)).toBe(false);
  for (const value of ["1", "true", "yes", "on", " TRUE ", true]) expect(isEnvTruthy(value)).toBe(true);
  const before = process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES;
  delete process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES;
  const clock = new ManualClock();
  let sends = 0;
  const activity = new CcrActivity(async () => { sends++; }, new AbortController().signal, { clock });
  try {
    expect(clock.pending.size).toBe(0);
    activity.start("root", "api_call", "attempt");
    clock.fire(); await settle();
    expect(sends).toBe(0);
    process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES = "yes";
    clock.fire(); await settle();
    expect(sends).toBe(1);
    activity.end("root", "api_call", "attempt");
    expect(clock.pending.size).toBe(0);
    activity.start("root", "tool_exec", "call");
    clock.fire(); await settle();
    expect(sends).toBe(2);
    activity.end("root", "tool_exec", "call");
    expect(clock.pending.size).toBe(0);
  } finally {
    await activity.close();
    if (before === undefined) delete process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES;
    else process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES = before;
  }
});

test("overlapping threads share a timer and ending one thread does not stop another", async () => {
  const clock = new ManualClock();
  const request = Promise.withResolvers<void>();
  let signal: AbortSignal | undefined;
  let sends = 0;
  const activity = new CcrActivity(async (next) => {
    sends++; signal = next;
    await request.promise;
  }, new AbortController().signal, { enabled: true, clock });
  try {
    activity.start("root", "api_call", "same-id");
    activity.start("child", "tool_exec", "same-id");
    activity.start("child", "tool_exec", "same-id");
    expect(clock.pending.size).toBe(1);
    clock.fire();
    clock.fire();
    expect(sends).toBe(1);
    activity.endThread("root");
    expect(signal?.aborted).toBe(false);
    expect(clock.pending.size).toBe(1);
    activity.endThread("child");
    expect(clock.pending.size).toBe(0);
    expect(signal?.aborted).toBe(true);
    let closed = false;
    const closing = activity.close().then(() => { closed = true; });
    await settle();
    expect(closed).toBe(false);
    request.resolve();
    await closing;
    activity.start("root", "api_call", "late");
    expect(clock.pending.size).toBe(0);
  } finally { request.resolve(); await activity.close(); }
});

test("failed keepalives leave active scheduling intact and session cancellation clears it", async () => {
  const clock = new ManualClock();
  const abort = new AbortController();
  let sends = 0;
  const activity = new CcrActivity(async () => { sends++; throw new Error("offline"); }, abort.signal, { enabled: true, clock });
  activity.start("a", "api_call", "request");
  clock.fire(); await settle();
  clock.fire(); await settle();
  expect(sends).toBe(2);
  abort.abort();
  expect(clock.pending.size).toBe(0);
  await activity.close();
  const stopped = new CcrActivity(async () => { sends++; }, abort.signal, { enabled: true, clock });
  stopped.start("a", "tool_exec", "late");
  expect(clock.pending.size).toBe(0);
  await stopped.close();
});

test("activity and cancellation stay isolated between sessions", async () => {
  const aClock = new ManualClock();
  const bClock = new ManualClock();
  const aAbort = new AbortController();
  const sends: string[] = [];
  const a = new CcrActivity(async () => { sends.push("a"); }, aAbort.signal, { enabled: true, clock: aClock });
  const b = new CcrActivity(async () => { sends.push("b"); }, new AbortController().signal, { enabled: true, clock: bClock });
  try {
    a.start("root", "api_call", "same");
    b.start("root", "api_call", "same");
    aClock.fire(); bClock.fire(); await settle();
    aAbort.abort();
    expect(aClock.pending.size).toBe(0);
    bClock.fire(); await settle();
    expect(sends).toEqual(["a", "b", "b"]);
  } finally { await Promise.all([a.close(), b.close()]); }
});

test("keepalive retries cancel on idle without stopping other writes, with or without a heartbeat binding", async () => {
  for (const bound of [false, true]) {
    const clock = new ManualClock();
    const requests: CcrHttpRequest[] = [];
    const client = new CcrClient({
      credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
      request: async (request) => {
        requests.push(request);
        return new Response(null, { status: request.path.endsWith("/events") ? 503 : 204 });
      },
      sleep: () => new Promise<void>(() => {}),
    });
    const heartbeat = bound ? new CcrHeartbeat({
      check: client.bindLease("a"), onLeaseLost: () => { throw new Error("Unexpected lease loss"); },
    }, new AbortController().signal, { schedule: () => () => {}, random: () => 0.5 }) : undefined;
    const activity = new CcrActivity((signal) => client.events("a", [{ type: "keep_alive" }], signal),
      new AbortController().signal, { enabled: true, clock });
    try {
      activity.start("a", "api_call", "request");
      clock.fire(); await settle();
      expect(requests).toHaveLength(1);
      expect(requests[0]?.timeout).toBe(10_000);
      expect(JSON.stringify(requests[0]?.body)).toContain('"type":"keep_alive"');
      activity.endThread("a");
      await activity.close();
      expect(requests[0]?.signal?.aborted).toBe(true);
      await client.persist("a", "dsh_log", logData({}));
      expect(requests).toHaveLength(2);
    } finally { await Promise.all([activity.close(), heartbeat?.close()]); }
  }
});

test("keepalive authentication failures still stop the owning session at the shared limit", async () => {
  const clock = new ManualClock();
  const abort = new AbortController();
  const lost: CcrLeaseFailure[] = [];
  let requests = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
    request: async () => { requests++; return new Response(null, { status: 401 }); },
    sleep: async () => {},
  });
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); abort.abort(); },
  }, abort.signal, { schedule: () => () => {}, random: () => 0.5 });
  const activity = new CcrActivity((signal) => client.events("a", [{ type: "keep_alive" }], signal),
    abort.signal, { enabled: true, clock });
  try {
    activity.start("a", "tool_exec", "call");
    clock.fire(); await settle();
    expect(requests).toBe(10);
    expect(lost).toHaveLength(1);
    expect(lost[0]?.consecutiveFailures).toBe(10);
    expect(clock.pending.size).toBe(0);
  } finally { await Promise.all([activity.close(), heartbeat.close()]); }
});

test("the plugin sends during real model and tool work, stops for approval and keeps events out of checkpoints", async () => {
  const backend = new Backend();
  const ctx = new Context();
  const clock = new ManualClock();
  const model = Promise.withResolvers<void>();
  const tool = Promise.withResolvers<void>();
  let calls = 0;
  backend.onModel = async () => { if (!calls++) { await model.promise; return toolEvents(); } return textEvents("done"); };
  backend.onTool = async () => { await tool.promise; return { content: [{ type: "text", text: "done" }] }; };
  try {
    await backend.ready;
    const api = new OmaApi(backend.url);
    await mountOma(ctx, api);
    ctx.ccrv2.config.keepAlive = { enabled: true, clock };
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    expect(clock.pending.size).toBe(0);
    const turn = session.receive("active", { content: "run tool" });
    await eventually(() => backend.requests.length === 1);
    const revision = session.agent.session.seq;
    clock.fire();
    await eventually(() => [...backend.publicLog.values()].filter((event) => event.type === "keep_alive").length === 1);
    expect(session.agent.session.seq).toBe(revision);
    expect(JSON.stringify([...backend.privateLog.values()])).not.toContain('"keep_alive"');
    model.resolve();
    await eventually(() => session.approvals.size === 1 && backend.statuses.at(-1) === "requires_action");
    expect(clock.pending.size).toBe(0);
    const requestId = [...session.approvals.keys()][0]!;
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: requestId, response: { behavior: "allow" },
    } });
    await eventually(() => backend.executions.length === 1);
    clock.fire();
    await eventually(() => [...backend.publicLog.values()].filter((event) => event.type === "keep_alive").length === 2);
    tool.resolve();
    await turn;
    expect(clock.pending.size).toBe(0);
    expect(backend.processed).toEqual(["active"]);
    expect(backend.executions).toHaveLength(1);
    expect(completedInputs(session)).toContain("active");
    expect(JSON.stringify([...backend.privateLog.values()])).not.toContain('"keep_alive"');
  } finally { model.resolve(); tool.resolve(); await ctx.fiber.dispose(); backend.stop(); }
});

test("a pending keepalive cannot delay a model result or terminal publication", async () => {
  const backend = new Backend();
  const ctx = new Context();
  const clock = new ManualClock();
  const model = Promise.withResolvers<void>();
  let keepAliveSignal: AbortSignal | undefined;
  backend.onModel = async () => { await model.promise; return textEvents("done"); };
  try {
    await backend.ready;
    const api = new OmaApi(backend.url);
    const client = await mountOma(ctx, api);
    ctx.ccrv2.config.keepAlive = { enabled: true, clock };
    const request = client.config.request;
    client.config.request = (options) => {
      if (!JSON.stringify(options.body)?.includes('"type":"keep_alive"')) return request(options);
      keepAliveSignal = options.signal;
      return new Promise<Response>((_resolve, reject) => {
        options.signal!.addEventListener("abort", () => reject(options.signal!.reason), { once: true });
      });
    };
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    const turn = session.receive("slow_keepalive", { content: "finish normally" });
    await eventually(() => backend.requests.length === 1);
    clock.fire();
    expect(keepAliveSignal?.aborted).toBe(false);
    model.resolve();
    await turn;
    expect(keepAliveSignal?.aborted).toBe(true);
    expect(clock.pending.size).toBe(0);
    expect(session.abort.signal.aborted).toBe(false);
    expect([...backend.publicLog.values()].some((event) => event.type === "result")).toBe(true);
    expect(session.pendingEvents).toEqual([]);
  } finally { model.resolve(); await ctx.fiber.dispose(); backend.stop(); }
});
