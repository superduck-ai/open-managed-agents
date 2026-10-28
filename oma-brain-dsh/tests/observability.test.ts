import { expect, spyOn, test } from "bun:test";
import OTel from "@deepseek-ai/dsh-otel";
import { Context } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { version as sessionVersion } from "@deepseek-ai/dsh-session/package.json";
import { OmaApi } from "../src/api.ts";
import { mountKernel } from "../src/kernel.ts";
import { mountTelemetry } from "../src/observability/index.ts";
import { assignment } from "./backend.ts";

test("disabled observability creates no exporter", async () => {
  const ctx = new Context();
  const api = new OmaApi("http://127.0.0.1:1");
  const task = assignment();
  api.assignments.set(task.session_id, task);
  try {
    await mountKernel(ctx);
    await mountTelemetry(ctx, api);
    const reporter = spyOn(ctx.otel, "createSessionLogReporter");
    ctx.sessions.create(SessionId(task.session_id)).append("turn/start", { turn: 1 });
    await api.telemetry!.close(task.session_id);
    expect(reporter).not.toHaveBeenCalled();
    reporter.mockRestore();
  } finally { await ctx.fiber.dispose(); }
});

test("official live capture exports original events and fork headers through the owning OMA credentials", async () => {
  type Record = { attributes: { key: string; value: { stringValue?: string } }[] };
  const scopes: { name: string; version: string }[] = [];
  const requests: { path: string; authorization: string | null; events: Record[] }[] = [];
  const server = Bun.serve({ hostname: "127.0.0.1", port: 0, async fetch(request) {
    const body = await request.json() as { resourceLogs: { scopeLogs: { scope: { name: string; version: string }; logRecords: Record[] }[] }[] };
    scopes.push(...body.resourceLogs.flatMap(resource => resource.scopeLogs.map(scope => scope.scope)));
    requests.push({ path: new URL(request.url).pathname, authorization: request.headers.get("authorization"), events: body.resourceLogs.flatMap(resource => resource.scopeLogs.flatMap(scope => scope.logRecords)) });
    return Response.json({});
  } });
  const ctx = new Context();
  const api = new OmaApi(server.url.toString().replace(/\/$/, ""));
  const task = { ...assignment(), observability_enabled: true };
  api.assignments.set(task.session_id, task);
  try {
    await mountKernel(ctx);
    await mountTelemetry(ctx, api);
    const root = ctx.sessions.create(SessionId(task.session_id));
    const event = root.append("turn/start", { turn: 1 });
    const child = ctx.sessions.fork(root);
    child.append("turn/end", { turn: 1, reason: { kind: "completed" } });
    task.ingress_token = "rotated-ingress";
    await api.telemetry!.close(task.session_id);
    expect(requests.length).toBeGreaterThan(0);
    expect(scopes.length).toBeGreaterThan(0);
    for (const scope of scopes) expect(scope).toEqual({ name: "@deepseek-ai/dsh-session", version: sessionVersion });
    const records = requests.flatMap(request => request.events);
    const value = (record: Record, key: string) => record.attributes.find(attribute => attribute.key === key)?.value.stringValue;
    expect(records.some(record => value(record, "content") === JSON.stringify(event))).toBe(true);
    expect(records.some(record => JSON.parse(value(record, "dsh.header")!).parentSession === root.id)).toBe(true);
    for (const request of requests) {
      expect(request.path).toBe(`/v1/code/sessions/${task.session_id}/worker/otlp/v1/logs`);
      expect(request.authorization).toBe("Bearer rotated-ingress");
    }
  } finally { await ctx.fiber.dispose(); server.stop(true); }
});

test("unavailable telemetry does not block chat or tool execution", async () => {
  const { Backend, eventually, toolEvents, textEvents } = await import("./backend.ts");
  const { mountOma } = await import("../src/plugin.ts");
  const { createOmaSession } = await import("../src/oma-session.ts");
  const backend = new Backend();
  await backend.ready;
  backend.a.observability_enabled = true;
  backend.a.launch.tool_permissions = { bash: "allow" };
  backend.modelScript = [toolEvents("telemetry-outage"), textEvents("completed despite telemetry outage")];
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    await mountOma(ctx, api);
    await ctx.plugin({
      inject: ["ccrv2"],
      apply: async (scope: Context) => {
        session = createOmaSession(scope, api, backend.a);
        await session.open();
      },
    });
    await session!.receive("telemetry-outage", { content: "run the tool" });
    await eventually(() => backend.telemetryRequests > 0);
    expect(backend.executions).toEqual([{ name: "bash", arguments: { command: "telemetry-outage" } }]);
    expect([...backend.publicLog.values()].some(event => event.type === "result" && event.is_error === false)).toBe(true);
  } finally {
    await session?.close();
    await api.telemetry?.close(backend.a.session_id);
    await ctx.fiber.dispose();
    backend.stop();
  }
});

test("telemetry cleanup finishes when transport shutdown never settles", async () => {
  const ctx = new Context();
  const api = new OmaApi("http://127.0.0.1:1");
  const task = { ...assignment(), observability_enabled: true };
  api.assignments.set(task.session_id, task);
  const { eventually } = await import("./backend.ts");
  try {
    await mountKernel(ctx);
    await mountTelemetry(ctx, api);
    const original = ctx.otel.createSessionLogReporter.bind(ctx.otel);
    let stop: ReturnType<typeof spyOn> | undefined;
    let shutdown: ReturnType<typeof spyOn> | undefined;
    const create = spyOn(OTel.prototype, "createSessionLogReporter").mockImplementation(options => {
      const reporter = original(options);
      stop = spyOn(reporter, "stopPending");
      shutdown = spyOn(reporter, "shutdown").mockImplementation(() => new Promise<void>(() => {}));
      return reporter;
    });
    ctx.sessions.create(SessionId(task.session_id)).append("turn/start", { turn: 1 });
    await eventually(() => stop !== undefined);
    api.assignments.delete(task.session_id);
    await api.telemetry!.close(task.session_id);
    expect(stop).toHaveBeenCalledTimes(1);
    shutdown?.mockRestore();
    stop?.mockRestore();
    create.mockRestore();
  } finally { await ctx.fiber.dispose(); }
});

test("closed assignments never recreate exporters and replacement credentials remain independent", async () => {
  const { NativeTelemetry } = await import("../src/observability/index.ts");
  const { eventually } = await import("./backend.ts");
  const ctx = new Context();
  const api = new OmaApi("http://127.0.0.1:1");
  const task = { ...assignment(), observability_enabled: true };
  api.assignments.set(task.session_id, task);
  type Options = Parameters<OTel["createSessionLogReporter"]>[0];
  const options: Options[] = [];
  const finish: (() => void)[] = [];
  let reports = 0;
  const create = spyOn(OTel.prototype, "createSessionLogReporter").mockImplementation(option => {
    options.push(option);
    const drained = new Promise<void>(resolve => finish.push(resolve));
    return {
      reportSessionLog: () => { reports++; },
      shutdown: () => drained,
      stopPending: () => {},
    } as unknown as ReturnType<OTel["createSessionLogReporter"]>;
  });
  try {
    await mountKernel(ctx);
    await mountTelemetry(ctx, api);
    const root = ctx.sessions.create(SessionId(task.session_id));
    root.append("turn/start", { turn: 1 });
    await eventually(() => options.length === 1);
    task.ingress_token = "closing-token";
    const firstClose = api.telemetry!.close(task.session_id);
    expect(api.telemetry!.close(task.session_id)).toBe(firstClose);
    const before = reports;
    root.append("turn/end", { turn: 1, reason: { kind: "completed" } });
    await Bun.sleep(20);
    expect(reports).toBe(before);
    expect(create).toHaveBeenCalledTimes(1);
    task.ingress_token = "too-late-token";
    expect(await (options[0]!.exporter!.headers as () => Promise<Record<string, string>>)()).toEqual({ Authorization: "Bearer closing-token" });
    const replacement = { ...task, ingress_token: "replacement-token", worker_epoch: task.worker_epoch + 1 };
    api.assignments.set(task.session_id, replacement);
    root.append("turn/start", { turn: 2 });
    await eventually(() => options.length === 2);
    finish[0]!();
    await firstClose;
    root.append("turn/end", { turn: 2, reason: { kind: "completed" } });
    await Bun.sleep(20);
    expect(create).toHaveBeenCalledTimes(2);
    expect(await (options[1]!.exporter!.headers as () => Promise<Record<string, string>>)()).toEqual({ Authorization: "Bearer replacement-token" });
    const shutdown = (api.telemetry as InstanceType<typeof NativeTelemetry>).shutdown();
    api.assignments.set(task.session_id, { ...replacement });
    root.append("turn/start", { turn: 3 });
    await Bun.sleep(20);
    expect(create).toHaveBeenCalledTimes(2);
    finish[1]!();
    await shutdown;
  } finally {
    for (const resolve of finish) resolve();
    create.mockRestore();
    await ctx.fiber.dispose();
  }
});

test("closing an assignment before its first event prevents exporter creation", async () => {
  const ctx = new Context();
  const api = new OmaApi("http://127.0.0.1:1");
  const task = { ...assignment(), observability_enabled: true };
  api.assignments.set(task.session_id, task);
  const create = spyOn(OTel.prototype, "createSessionLogReporter");
  try {
    await mountKernel(ctx);
    await mountTelemetry(ctx, api);
    await api.telemetry!.close(task.session_id);
    ctx.sessions.create(SessionId(task.session_id)).append("turn/start", { turn: 1 });
    await Bun.sleep(20);
    expect(create).not.toHaveBeenCalled();
  } finally { create.mockRestore(); await ctx.fiber.dispose(); }
});

test("model request clock prefers start frames and falls back to step and settlement times", async () => {
  const { ModelRequestClock } = await import("../src/observability/model-requests.ts");
  const clock = new ModelRequestClock();
  const stream = [{ type: "chunk", time: 140, chunk: { type: "block-start", index: 0, blockType: "text" } }, { type: "text-chunks", time0: 150, index: 0, dt: [0], texts: ["hi"] }];
  expect(clock.observe("s", "step/start", 100, { turn: 1, step: 1 })).toEqual({});
  expect(clock.observe("s", "request/context", 101, { provider: "oma", model: "context-model" })).toEqual({});
  expect(clock.observe("s", "assistant/attempt", 120, { turn: 1, step: 1, stream: [] })).toEqual({
    "oma.model_request.started_at": 100, "gen_ai.request.model": "context-model",
  });
  clock.started("s", 130);
  expect(clock.observe("s", "assistant/message", 160, { turn: 1, step: 1, stream, message: { source: { model: "message-model" } } })).toEqual({
    "oma.model_request.started_at": 130, "oma.model_request.first_token_at": 150, "gen_ai.request.model": "message-model",
  });
  expect(clock.observe("s", "assistant/attempt", 170, { stream: [] })["oma.model_request.started_at"]).toBe(160);
  clock.observe("s", "step/end", 180, {});
  expect(clock.observe("s", "assistant/attempt", 190, { stream: [] })).toEqual({ "gen_ai.request.model": "context-model" });
  clock.started("s", 200);
  clock.forget("s");
  expect(clock.observe("s", "assistant/attempt", 210, null)).toEqual({});
  clock.started("late", 300);
  expect(clock.observe("late", "assistant/attempt", 250, { stream: "not-a-stream" })).toEqual({});
});

test("model request attributes use the retried attempt's start frame instead of retry backoff", async () => {
  const { Backend, eventually, textEvents } = await import("./backend.ts");
  const { mountOma } = await import("../src/plugin.ts");
  const { createOmaSession } = await import("../src/oma-session.ts");
  const backend = new Backend();
  await backend.ready;
  backend.a.observability_enabled = true;
  backend.telemetryStatus = 200;
  let calls = 0;
  backend.onModel = async () => (++calls === 1 ? Response.json({ type: "error", error: { type: "api_error", message: "overloaded" } }, { status: 500 }) : textEvents("after retry"));
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  let session: ReturnType<typeof createOmaSession> | undefined;
  type Attribute = { key: string; value: { stringValue?: string; intValue?: string | number; doubleValue?: number } };
  const attribute = (record: { attributes: Attribute[] }, key: string) => {
    const value = record.attributes.find(item => item.key === key)?.value;
    return value?.stringValue ?? (value?.intValue !== undefined ? Number(value.intValue) : value?.doubleValue);
  };
  const event = (record: { attributes: Attribute[] }) => JSON.parse(String(attribute(record, "content"))) as { type: string; time: number };
  try {
    await mountOma(ctx, api);
    await ctx.plugin({
      inject: ["ccrv2"],
      apply: async (scope: Context) => {
        session = createOmaSession(scope, api, backend.a);
        await session.open();
      },
    });
    await session!.receive("retry-telemetry", { content: "hello" });
    await eventually(() => backend.telemetryRecords.some(record => event(record as { attributes: Attribute[] }).type === "assistant/message"), 10_000);
    const records = backend.telemetryRecords as { attributes: Attribute[] }[];
    const attempt = records.find(record => event(record).type === "assistant/attempt")!;
    const message = records.find(record => event(record).type === "assistant/message")!;
    const stepStart = records.find(record => event(record).type === "step/start")!;
    expect(calls).toBe(2);
    const attemptStarted = attribute(attempt, "oma.model_request.started_at") as number;
    const messageStarted = attribute(message, "oma.model_request.started_at") as number;
    expect(attemptStarted).toBeGreaterThanOrEqual(event(stepStart).time);
    expect(attemptStarted).toBeLessThanOrEqual(event(attempt).time);
    expect(messageStarted).toBeGreaterThan(event(attempt).time);
    expect(messageStarted).toBeLessThanOrEqual(event(message).time);
    expect(attribute(message, "oma.model_request.first_token_at") as number).toBeGreaterThanOrEqual(messageStarted);
    expect(attribute(message, "gen_ai.request.model")).toBe(backend.a.launch.model);
    expect(attribute(attempt, "oma.model_request.first_token_at")).toBeUndefined();
    expect(records.every(record => event(record).type.startsWith("assistant/") || attribute(record, "oma.model_request.started_at") === undefined)).toBe(true);
  } finally {
    await session?.close();
    await api.telemetry?.close(backend.a.session_id);
    await ctx.fiber.dispose();
    backend.stop();
  }
});
