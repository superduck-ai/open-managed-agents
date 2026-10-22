import { afterEach, describe, expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import * as CcrBridge from "../src/ccrv2/index.ts";
import { Backend, eventually, toolEvents, textEvents, modelSystemText } from "./backend.ts";
import type { Payload } from "../src/json.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function open(backend: Backend) {
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  const client = await mountOma(ctx, api);
  let session!: ReturnType<typeof createOmaSession>;
  await ctx.plugin({
    inject: ["ccrv2"],
    apply: async (scope: Context) => {
      session = createOmaSession(scope, api, structuredClone(backend.a));
      await session.open();
    },
  });
  cleanups.push(async () => {
    if (!session.abort.signal.aborted) session.stop();
    await session.handle?.dispose();
    await ctx.fiber.dispose();
  });
  return { ctx, api, session, client };
}

function backend() {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  return b;
}

function toolsWithText(text: string, ...calls: string[]): Payload[] {
  return [
    ...textEvents(text).slice(0, 4),
    ...calls.flatMap((callId, index) =>
      toolEvents(callId, callId)
        .slice(1, 4)
        .map((event) => ({ ...event, index: index + 1 })),
    ),
    ...toolEvents().slice(4),
  ];
}

describe("original dsh loop over CCRv2", () => {
  test("invalid mixed input publishes a failed terminal result without model work and allows the next turn", async () => {
    for (const invalid of [
      { type: "image", source: { type: "base64", media_type: "image/png", data: "not an image" } },
      { type: "document", source: { type: "text", media_type: "text/plain", data: 42 } },
    ]) {
      const b = backend();
      b.modelScript = [textEvents("recovered")];
      const { session } = await open(b);
      await session.receive("invalid_input", { content: [{ type: "text", text: "read it" }, invalid] });
      expect(b.requests).toHaveLength(0);
      expect(b.publicLog.get("invalid_input_failed_result")?.is_error).toBe(true);
      expect(b.statuses.at(-1)).toBe("idle");
      expect(session.state.completed).toContain("invalid_input");
      await session.receive("valid_followup", { content: "continue" });
      expect(b.requests).toHaveLength(1);
      expect(b.publicLog.get("valid_followup_result")?.is_error).toBe(false);
    }
  });

  test("mixed image input reaches the model in order and survives a second turn", async () => {
    const b = backend();
    b.modelScript = [textEvents("image read"), textEvents("remembered")];
    const { session } = await open(b);
    const content = [
      { type: "text", text: "read image" },
      { type: "image", source: { type: "base64", media_type: "image/png",
        data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC" } },
      { type: "text", text: "then answer" },
    ];
    await session.receive("image_input", { content });
    const first = b.requests[0]!.messages.find((m: Payload) => m.role === "user").content;
    expect(first.map((block: Payload) => block.type)).toEqual(["text", "image", "text"]);
    expect(first[0]).toEqual(content[0]);
    expect(first[2]).toEqual(content[2]);
    expect(first[1].source.type).toBe("base64");
    expect(first[1].source.data.length).toBeGreaterThan(0);
    await session.receive("image_followup", { content: "remember it" });
    expect(b.requests[1]!.messages.some((m: Payload) => m.content.some((block: Payload) => block.type === "image"))).toBe(true);
    expect(b.publicLog.get("image_input_result")?.is_error).toBe(false);
  });

  test("image file and url references reach the Messages request without inline bytes", async () => {
    const b = backend();
    b.modelScript = [textEvents("seen")];
    const { session } = await open(b);
    const content = [
      { type: "image", source: { type: "file", file_id: "file_attachment" } },
      { type: "image", source: { type: "url", url: "https://example.com/cat.png" } },
    ];
    await session.receive("image_refs", { content });
    const requestContent = b.requests[0]!.messages.find((m: Payload) => m.role === "user").content;
    expect(requestContent.map((block: Payload) => ({ type: block.type, source: block.source }))).toEqual(content);
    expect(JSON.stringify([...b.privateLog.values()])).not.toContain("base64");
    expect(b.publicLog.get("image_refs_result")?.is_error).toBe(false);
  });

  test("plain text documents reach text model routes with their boundary and title", async () => {
    const b = backend();
    b.modelScript = [textEvents("DOC-42")];
    const { session } = await open(b);
    const content = [
      { type: "text", text: "read document" },
      { type: "document", title: "source", source: { type: "text", media_type: "text/plain", data: "number=DOC-42" } },
    ];
    await session.receive("document_input", { message: { content } });
    const requestContent = b.requests[0]!.messages.find((m: Payload) => m.role === "user").content;
    expect(requestContent[0]).toEqual(content[0]);
    expect(requestContent[1].type).toBe("text");
    expect(JSON.parse(requestContent[1].text)).toEqual({ type: "document", title: "source", content: "number=DOC-42" });
    expect(b.publicLog.get("document_input_result")?.is_error).toBe(false);
  });

  test("thinking and text preserve native block identities across previews and durable messages", async () => {
    const b = backend();
    const events = textEvents("你好");
    events[0]!.message.id = "msg_thinking";
    for (const event of events) if (typeof event.index === "number") event.index = 2;
    events.splice(1, 0,
      { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
      { type: "content_block_stop", index: 0 },
      { type: "content_block_start", index: 1, content_block: { type: "thinking", thinking: "" } },
      { type: "content_block_delta", index: 1, delta: { type: "thinking_delta", thinking: "检查回复" } },
      { type: "content_block_stop", index: 1 },
    );
    b.modelScript = [events];
    const { session } = await open(b);
    await session.receive("thinking", { content: "请只回复：你好" });
    const messages = [...b.publicLog.values()].filter((event) => ["agent.thinking", "agent.message"].includes(event.type));
    expect(messages.map((event) => event.type)).toEqual(["agent.message", "agent.thinking", "agent.message"]);
    expect(messages.map((event) => event.content)).toEqual([
      [{ type: "text", text: "" }], undefined, [{ type: "text", text: "你好" }],
    ]);
    const replay = session.state.outbox;
    expect(replay).toEqual([]);
    expect(b.timeline.filter((event) => event.type === "event_start" && event.event.id === messages[0]!.id)).toHaveLength(0);
    for (const message of messages.slice(1)) {
      expect(message.id).toMatch(/^sevt_[a-f0-9]{32}$/);
      expect(b.timeline.filter((event) => event.type === "event_start" && event.event.id === message.id)).toHaveLength(1);
      expect(b.timeline.filter((event) => event.type === "event_stop")).toHaveLength(0);
    }
    const reasoning = b.timeline.find((event) => event.type === "event_delta" && event.event_id === messages[1]!.id);
    expect(reasoning).toBeUndefined();
    expect(b.publicLog.get("thinking_result")?.is_error).toBe(false);
  });

  test("malformed tool JSON retains a failed result and continues with correlated history", async () => {
    const b = backend();
    const raw = '{"x": 2, "y": \u0001}';
    const events = toolEvents();
    events[2]!.delta.partial_json = raw;
    b.modelScript = [events, textEvents("recovered")];
    const { session } = await open(b);
    await session.receive("malformed", { message: { content: "run" } });
    expect(b.executions).toEqual([]);
    expect(b.requests).toHaveLength(2);
    const blocks = b.requests[1]!.messages.flatMap((m: Payload) => m.content);
    expect(blocks).toContainEqual({ type: "tool_use", id: "call_1", name: "Bash",
      input: { __unparsedToolInput: { raw, len: raw.length } } });
    const result = blocks.find((block: Payload) => block.type === "tool_result");
    expect(result).toMatchObject({ tool_use_id: "call_1", is_error: true });
    expect(typeof result.content).toBe("string");
    expect(result.content.length).toBeGreaterThan(0);
    const published = [...b.publicLog.values()].find((event) => event.type === "user");
    expect(published?.message.content[0]).toMatchObject({ tool_use_id: "call_1", is_error: true });
    expect(b.publicLog.get("malformed_result")?.is_error).toBe(false);
  });

  test("successful Bash publishes and replays exact stdout without execution metadata", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("done")];
    b.onTool = async () => {
      const structuredContent = { stdout: "exact stdout\n", stderr: "", exit_code: 0,
        status: "completed", timed_out: false, cancelled: false,
        stdout_truncated: false, stderr_truncated: false };
      return { structuredContent, content: [{ type: "text", text: structuredContent.stdout },
        { type: "text", text: JSON.stringify(structuredContent) }] };
    };
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: p.request_id, response: { behavior: "allow" },
      } });
    };
    await session.receive("stdout", { message: { content: "run" } });
    const result = b.requests[1]!.messages.flatMap((m: Payload) => m.content)
      .find((block: Payload) => block.type === "tool_result");
    expect(result).toEqual({ type: "tool_result", tool_use_id: "call_1", content: "exact stdout\n" });
    const published = [...b.publicLog.values()].find((event) => event.type === "user");
    expect(published?.message.content[0]).toEqual({ type: "tool_result", tool_use_id: "call_1", tool_name: "Bash", content: [{ type: "text", text: "exact stdout\n" }], is_error: false });
    expect(result).not.toHaveProperty("tool_name");
    expect(b.publicLog.get("stdout_result")?.is_error).toBe(false);
  });

  test("上下文提前暂停且提高或移除预算后恢复", async () => {
    for (const remove of [false, true]) {
      const b = backend();
      const route = b.route.bind(b);
      let reached = true;
      const money = (amount: string) => ({ amount, currency: "USD" });
      b.route = async (request) => {
        const response = await route(request);
        if (new URL(request.url).pathname !== `/v2/sessions/${b.a.session_id}`) return response;
        const body = await response.json() as Payload;
        body.session_context.billing = {
          budget: !reached && remove ? null : { type: "limit", max_list_cost: money(reached ? "1" : "100") },
          usage: money("1"), remaining: !reached && remove ? null : money(reached ? "0" : "99"),
          budget_reached: reached, observed_at: new Date().toISOString(), pricing: {}, calculation_policy: "server_authoritative",
        };
        return Response.json(body);
      };
      const { session, api } = await open(b);
      await session.receive("preflight_pause", { message: { content: "hi" } });
      expect(b.requests).toHaveLength(0);
      expect(b.publicLog.has("preflight_pause_failed_result")).toBe(false);
      expect(api.assignment(b.a.session_id).launch.billing?.budget_reached).toBe(true);
      reached = false;
      await session.receive("preflight_resume", { message: { content: "continue" } });
      expect(b.requests).toHaveLength(1);
      expect(b.publicLog.get("preflight_resume_result")?.is_error).toBe(false);
      expect(api.assignment(b.a.session_id).launch.billing?.budget_reached).toBe(false);
    }
  });

  test("预算门禁不重试、不发布失败结果，恢复后接受新输入", async () => {
    const b = backend();
    const route = b.route.bind(b);
    let paused = true;
    let attempts = 0;
    b.route = async (request) => {
      if (new URL(request.url).pathname === "/v1/messages" && paused) {
        attempts++;
        return Response.json({ error: { type: "invalid_request_error", code: "session_budget_reached", message: "budget reached" } }, { status: 400 });
      }
      return route(request);
    };
    const { session } = await open(b);
    await session.receive("budget_pause", { message: { content: "hi" } });
    expect(attempts).toBe(1);
    expect(b.publicLog.has("budget_pause_failed_result")).toBe(false);
    expect(b.publicLog.has("budget_pause_result")).toBe(false);
    expect(session.state.completed).toContain("budget_pause");
    paused = false;
    await session.receive("budget_resume", { message: { content: "continue" } });
    expect(b.publicLog.get("budget_resume_result")?.is_error).toBe(false);
  });

  test("provider HTTP 402 publishes a safe quota error before ACK and a later turn can recover", async () => {
    const b = backend();
    const route = b.route.bind(b);
    let failureStatus = 402;
    b.route = async (request) => {
      if (new URL(request.url).pathname === "/v1/messages" && failureStatus)
        return Response.json({ error: { type: "unknown_error", message: failureStatus === 402
          ? "Insufficient Balance sk-test-private-provider-detail" : "Unauthorized sk-test-private-provider-detail" } }, { status: failureStatus });
      return route(request);
    };
    const diagnostics: string[] = [];
    const log = spyOn(console, "error").mockImplementation((value: unknown) => { diagnostics.push(String(value)); });
    try {
      const { session } = await open(b);
      await session.receive("quota_failure", { message: { content: "hi" } });
      const error = b.publicLog.get("quota_failure_failed_result");
      expect(error?.type).toBe("result");
      expect(error?.is_error).toBe(true);
      const failureText = JSON.stringify(b.publicLog.get("quota_failure_failed_message"));
      expect(failureText).toContain("balance or quota is exhausted");
      expect(failureText).toContain("switch model credentials");
      expect(JSON.stringify([...b.publicLog.values()])).not.toContain("sk-test-private");
      expect(diagnostics.some((line) => line.includes('"failure_reason":"model_quota_exhausted"'))).toBe(true);
      expect(diagnostics.join("\n")).not.toContain("sk-test-private");
      expect(session.state.completed).toContain("quota_failure");
      expect(session.state.outbox).toEqual([]);
      const published = b.timeline.findIndex((entry) => entry.type === "result");
      const saved = b.timeline.findIndex((entry) => entry.type === "private.persist"
        && entry.payload.subtype === "dsh_brain_state"
        && entry.payload.data.outbox.some((event: Payload) => event.type === "result"));
      const ack = b.timeline.findIndex((entry) => entry.type === "input.ack");
      expect(saved).toBeGreaterThanOrEqual(0);
      expect(published).toBeGreaterThan(saved);
      expect(ack).toBeGreaterThan(published);
      failureStatus = 0;
      await session.receive("quota_recovered", { message: { content: "try again" } });
      expect(b.publicLog.get("quota_recovered_result")?.is_error).toBe(false);
      expect(b.publicLog.has("quota_recovered_failed_result")).toBe(false);
      failureStatus = 401;
      await session.receive("different_failure", { message: { content: "another request" } });
      expect(b.publicLog.get("different_failure_failed_result")?.is_error).toBe(true);
    } finally { log.mockRestore(); }
  });

  test("session shutdown cancels an unconfirmed terminal delivery after durable publication", async () => {
    const b = backend();
    b.modelScript = [textEvents("done")];
    const { session, client } = await open(b);
    const processing = Promise.withResolvers<void>();
    const terminal = Promise.withResolvers<void>();
    const request = client.config.request;
    client.config.request = async (options) => {
      if (options.path.endsWith("/events/delivery")) {
        const body = options.body as { updates: { status: string }[] };
        if (body.updates.some((update) => update.status === "processing")) processing.resolve();
        if (body.updates.some((update) => update.status === "processed")) {
          terminal.resolve();
          return new Promise<Response>((_, reject) => {
            options.signal!.addEventListener("abort", () => reject(options.signal!.reason), { once: true });
          });
        }
      }
      return request(options);
    };
    let confirmed = false;
    const turn = session.receive("cancel_delivery", { message: { content: "run" } });
    turn.then(() => { confirmed = true; }, () => {});
    await processing.promise;
    await terminal.promise;
    expect(confirmed).toBe(false);
    expect(session.state.completed).toContain("cancel_delivery");
    expect(session.state.outbox).toEqual([]);
    expect(b.publicLog.has("cancel_delivery_result")).toBe(true);
    expect(b.processed).toEqual([]);
    session.stop();
    await expect(turn).rejects.toThrow();
    expect(b.requests).toHaveLength(1);
  });

  test("unloading the plugin cancels a write retry before draining final persistence", async () => {
    const b = backend();
    const { ctx, session, client } = await open(b);
    const waiting = Promise.withResolvers<void>();
    client.config.sleep = () => { waiting.resolve(); return new Promise<void>(() => {}); };
    b.failWrites = true;
    const turn = session.receive("cancel_retry", { message: { content: "run" } });
    turn.catch(() => {});
    await waiting.promise;
    // The old retry stays asleep until cancellation, while final drain writes
    // can be confirmed normally after shutdown begins.
    b.failWrites = false;
    await ctx.fiber.dispose();
    await expect(turn).rejects.toThrow();
    expect(session.abort.signal.aborted).toBe(true);
    expect(b.processed).toEqual([]);
    expect(b.requests).toEqual([]);
  });

  test("retries hold checkpoint and publication barriers until confirmation, then resume without replay", async () => {
    for (const stage of ["accepted", "dispatch", "result", "publication"] as const) {
      const b = backend();
      b.modelScript = [toolEvents(), textEvents("done")];
      const { session, client } = await open(b);
      b.onApproval = async (payload) => { await session.control({
        type: "control_response", response: {
          subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
        },
      }); };
      let failures = 0;
      const waiting = Promise.withResolvers<void>();
      const resume = Promise.withResolvers<void>();
      const request = client.config.request;
      client.config.request = async (options) => {
        const body = JSON.stringify(options.body);
        const matches = stage === "publication"
          ? options.path.endsWith("/events") && body.includes('"type":"tool_result"')
          : options.path.endsWith("/internal-events") && body.includes('"subtype":"dsh_brain_state"') &&
            body.includes(`"phase":"${stage}"`);
        if (matches && failures < 4) {
          failures++;
          return new Response(null, { status: stage === "accepted" ? 401 : 503 });
        }
        return request(options);
      };
      client.config.sleep = async () => { if (failures === 4) { waiting.resolve(); await resume.promise; } };
      let finished = false;
      const turn = session.receive(`retry_${stage}`, { message: { content: "run" } });
      turn.then(() => { finished = true; }, () => {});
      try {
        await waiting.promise;
        expect(failures).toBe(4);
        expect(finished).toBe(false);
        expect(session.abort.signal.aborted).toBe(false);
        expect(b.processed).toEqual([]);
        expect(b.requests).toHaveLength(stage === "accepted" ? 0 : 1);
        expect(b.executions).toHaveLength(stage === "accepted" || stage === "dispatch" ? 0 : 1);
        resume.resolve();
        await turn;
        expect(b.requests).toHaveLength(2);
        expect(b.executions).toHaveLength(1);
        expect(b.processed).toEqual([`retry_${stage}`]);
        expect(session.state.outbox).toEqual([]);
      } finally { resume.resolve(); session.stop(); await turn.catch(() => {}); }
    }
  });

  test("disposing the bridge cancels an approval wait without dispatching a tool", async () => {
    const b = backend();
    b.modelScript = [toolEvents()];
    await b.ready;
    const ctx = new Context();
    try {
      const api = new OmaApi(b.url);
      await mountOma(ctx, api);
      const session = createOmaSession(ctx, api, b.a);
      await session.open();
      const turn = session.receive("shutdown_approval", { content: "run" });
      // Observe the rejection immediately so cancellation cannot go unhandled.
      const settled = turn.then(() => undefined, () => undefined);
      await eventually(() => session.approvals.size === 1);
      const [bridge] = ctx.registry.get(CcrBridge)!.fibers;
      expect(bridge).toBeDefined();
      await bridge!.dispose();
      await settled;
      expect(ctx.get("ccrv2")).toBeUndefined();
      expect(ctx.get("agentLoop")).toBeDefined();
      expect(session.abort.signal.aborted).toBe(true);
      expect(session.approvals.size).toBe(0);
      expect(b.sandboxCount).toBe(0);
      expect(b.executions).toHaveLength(0);
    } finally {
      await ctx.fiber.dispose();
    }
  });

  test("durable checkpoints precede ACK, sandbox dispatch and terminal publication", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("done")];
    const { session } = await open(b);
    b.onApproval = async (payload) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: { behavior: "allow", updatedInput: { command: "approved" } },
        },
      });
    };
    await session.receive("durable_order", { message: { content: "run" } });
    const first = (predicate: (event: Payload) => boolean) => {
      const index = b.timeline.findIndex(predicate);
      expect(index).toBeGreaterThan(-1);
      return index;
    };
    const state = (predicate: (data: Payload) => boolean) => first(
      (event) => event.type === "private.persist" &&
        event.payload.subtype === "dsh_brain_state" && predicate(event.payload.data),
    );
    const accepted = state((s) => s.inputId === "durable_order" && s.phase === "accepted");
    const ack = first((e) => e.type === "input.ack" && e.inputId === "durable_order");
    const processing = first((e) => e.type === "input.delivery" && e.inputId === "durable_order" && e.status === "processing");
    const model = first((e) => e.type === "model.request");
    expect(accepted).toBeLessThan(processing);
    expect(processing).toBeLessThan(model);
    const approval = first((e) => e.type === "control_request");
    expect(b.timeline.slice(model, approval).some(
      (e) => e.type === "private.persist" && e.payload.subtype === "dsh_log",
    )).toBe(true);
    const approved = state((s) => s.approvedCalls.call_1?.effective.command === "approved");
    const sandbox = first((e) => e.type === "sandbox.prepare");
    const dispatch = state((s) => s.phase === "dispatch");
    const execution = first((e) => e.type === "tool.dispatch");
    expect(approval).toBeLessThan(sandbox);
    expect(sandbox).toBeLessThan(dispatch);
    expect(approved).toBe(dispatch);
    expect(dispatch).toBeLessThan(execution);
    const stateSaves = (from: number, to: number) => b.timeline.slice(from, to).filter(
      (e) => e.type === "private.persist" && e.payload.subtype === "dsh_brain_state",
    ).length;
    expect(stateSaves(model, approval)).toBe(1);
    expect(stateSaves(approval, execution)).toBe(1);
    expect(b.timeline.filter((e) => e.type === "private.persist" && e.payload.subtype === "dsh_log"
      && e.payload.data.events.length === 0)).toHaveLength(1);
    const result = state((s) => s.phase === "result" && !!s.toolOutcomes.call_1);
    const nextModel = b.timeline.findIndex((e, i) => i > execution && e.type === "model.request");
    expect(nextModel).toBeGreaterThan(result);
    const completed = state((s) => s.phase === "completed" && s.outbox.length > 0);
    const idle = b.timeline.findIndex((e, i) => i > completed && e.type === "worker.status" && e.status === "idle");
    const published = first((e) => e.type === "result");
    const cleared = state((s) => s.phase === "completed" && s.outbox.length === 0);
    expect(idle).toBeGreaterThan(completed);
    expect(cleared).toBeLessThan(idle);
    expect(published).toBeLessThan(cleared);
    expect(cleared).toBeLessThan(ack);
  });

  test("a failed tool-result publication stops the next model request and survives restart", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("must not be requested")];
    b.failPublicType = "user";
    const first = await open(b);
    b.onApproval = async (payload) => {
      await first.session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    const turn = first.session.receive("unpublished_result", { message: { content: "run" } });
    turn.catch(() => {});
    await eventually(() => b.failedCcrWrites > 0);
    first.session.stop();
    await expect(turn).rejects.toThrow();
    expect(b.requests).toHaveLength(1);
    expect(b.executions).toHaveLength(1);
    expect(
      first.session.state.outbox.some((event) => event.type === "user"),
    ).toBe(true);
    await first.session.close();
    b.failPublicType = undefined;
    const second = await open(b);
    await second.session.receive("unpublished_result", {
      message: { content: "run" },
    });
    const results = [...b.publicLog.values()].filter(
      (event) => event.type === "user",
    );
    expect(results).toHaveLength(1);
    expect(results[0]!.message.content[0].tool_use_id).toBe("call_1");
    expect(b.requests).toHaveLength(1);
    expect(b.executions).toHaveLength(1);
    expect(second.session.state.outbox).toEqual([]);
  });

  test("messages and tool results precede the next tool or model request", async () => {
    const b = backend();
    b.modelScript = [
      toolsWithText("before tools", "call_first", "call_second"),
      toolsWithText("after two tools", "call_third"),
      textEvents("done"),
    ];
    const { session } = await open(b);
    b.onApproval = async (payload) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    await session.receive("ordered_turn", { message: { content: "run" } });
    const order = b.timeline.flatMap((event) => {
      switch (event.type) {
        case "model.request":
          return ["model"];
        case "agent.message":
          return [event.content.map((block: Payload) => block.text).join("")];
        case "control_request":
          return [`call:${event.request.tool_use_id}`];
        case "user":
          return event.message.content.map(
            (block: Payload) => `result:${block.tool_use_id}`,
          );
        case "result":
          return ["idle"];
        default:
          return [];
      }
    });
    expect(order).toEqual([
      "model",
      "before tools",
      "call:call_first",
      "result:call_first",
      "call:call_second",
      "result:call_second",
      "model",
      "after two tools",
      "call:call_third",
      "result:call_third",
      "model",
      "done",
      "idle",
    ]);
    expect(
      b.timeline.filter((event) => event.type === "system.message"),
    ).toEqual([]);
    const messages = b.timeline.filter(
      (event) => event.type === "agent.message",
    );
    const starts = b.timeline.filter((event) => event.type === "event_start");
    for (const message of messages) {
      const id = message.id;
      expect(starts.filter((event) => event.event.id === id)).toHaveLength(1);
      const started = b.timeline.findIndex(
        (event) => event.type === "event_start" && event.event.id === id,
      );
      expect(started).toBeGreaterThan(-1);
      expect(started).toBeLessThan(b.timeline.indexOf(message));
      expect(b.timeline.filter((event) => event.type === "event_stop")).toHaveLength(0);
    }
  });

  test("tool-only model steps do not publish empty message previews", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("done")];
    const { session } = await open(b);
    b.onApproval = async (payload) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    await session.receive("tool_only", { message: { content: "run" } });
    const starts = b.timeline.filter((event) => event.type === "event_start");
    const stops = b.timeline.filter((event) => event.type === "event_stop");
    const messages = b.timeline.filter(
      (event) => event.type === "agent.message",
    );
    expect(starts).toHaveLength(1);
    expect(stops).toHaveLength(0);
    expect(messages).toHaveLength(1);
    expect(starts[0]!.event.id).toBe(messages[0]!.id);
  });

  test("terminal outbox survives failed publication and is republished without resampling", async () => {
    const b = backend();
    const first = await open(b);
    b.failPublicEvents = true;
    const turn = first.session.receive("unpublished", { message: { content: "hello" } });
    turn.catch(() => {});
    await eventually(() => first.session.state.completed.includes("unpublished") && b.failedCcrWrites > 0);
    first.session.stop();
    await expect(turn).rejects.toThrow();
    expect(first.session.state.completed).toContain("unpublished");
    expect(first.session.state.outbox.length).toBeGreaterThan(0);
    await first.session.close();
    b.failPublicEvents = false;
    const second = await open(b);
    await second.session.receive("unpublished", {
      message: { content: "hello" },
    });
    expect(b.requests).toHaveLength(1);
    expect(
      [...b.publicLog.values()].filter((p) => p.type === "result"),
    ).toHaveLength(1);
    expect(second.session.state.outbox).toEqual([]);
  });
  test("unconfirmed input checkpoint is not acknowledged or sent to the model", async () => {
    const b = backend();
    const { session } = await open(b);
    b.failWrites = true;
    const turn = session.receive("failed_commit", { message: { content: "run" } });
    turn.catch(() => {});
    await eventually(() => b.failedCcrWrites > 0);
    session.stop();
    await expect(turn).rejects.toThrow();
    expect(b.processed).toEqual([]);
    expect(b.requests).toEqual([]);
    expect(b.executions).toEqual([]);
    b.failWrites = false;
  });

  test("an epoch rejection at the dispatch checkpoint prevents tool execution", async () => {
    const b = backend();
    b.modelScript = [toolEvents()];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      b.a.worker_epoch++;
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    await expect(
      session.receive("lost_lease", { message: { content: "run" } }),
    ).rejects.toThrow("409");
    expect(b.sandboxCount).toBe(1);
    expect(b.executions).toEqual([]);
    expect(b.requests).toHaveLength(1);
    b.a.worker_epoch--;
  });
  test("denied tool allocates no sandbox and continues the model turn", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("denied")];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: { behavior: "deny" },
        },
      });
    };
    await session.receive("input_denied", {
      type: "user",
      message: { content: "run" },
    });
    expect(b.mcpMethods).toEqual([]);
    expect(b.sandboxCount).toBe(0);
    expect(b.executions.length).toBe(0);
    expect(b.requests.length).toBe(2);
    const resultIndex = b.timeline.findIndex((event) => event.type === "user");
    expect(resultIndex).toBeGreaterThan(-1);
    expect(resultIndex).toBeLessThan(
      b.timeline.findLastIndex((event) => event.type === "model.request"),
    );
    expect(
      b.requests[1]!.messages.some((m: any) =>
        Array.isArray(m.content) && m.content.some((c: any) => c.type === "tool_result" && c.is_error),
      ),
    ).toBe(true);
  });

  test("approved replacement is validated before sandbox creation", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("invalid replacement")];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: { behavior: "allow", updatedInput: { command: 123 } },
        },
      });
    };
    await session.receive("input_invalid", { message: { content: "run" } });
    expect(b.sandboxCount).toBe(0);
    expect(b.executions).toEqual([]);
  });

  test("text completes once and acknowledged duplicate is not resampled", async () => {
    const b = backend();
    const { session } = await open(b);
    expect(b.mcpMethods).toEqual([]);
    await session.receive("input_text", { message: { content: "hello" } });
    await session.receive("input_text", { message: { content: "hello" } });
    expect(b.mcpMethods).toEqual([]);
    expect(b.requests.length).toBe(1);
    expect(
      [...b.publicLog.values()]
        .filter((p) => p.type === "agent.message")
        .map((p) => p.content),
    ).toEqual([[{ type: "text", text: "hello" }]]);
    expect(
      [...b.publicLog.values()].filter((p) => p.type === "result"),
    ).toHaveLength(1);
    expect(session.state.phase).toBe("completed");
    expect(b.sandboxCount).toBe(0);
  });

  test("modified approval parameters execute and reach the next model request", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("done")];
    const { ctx, session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: {
            behavior: "allow",
            updatedInput: { command: "approved" },
          },
        },
      });
    };
    await session.receive("input_tool", { message: { content: "run" } });
    expect(b.executions).toEqual([
      { name: "bash", arguments: { command: "approved" } },
    ]);
    expect(session.state.approvedCalls.call_1).toEqual({
      name: "Bash",
      original: { command: "original" },
      effective: { command: "approved" },
    });
    expect(b.mcpMethods).toEqual(["initialize", "notifications/initialized", "tools/call"]);
    expect(JSON.stringify(b.requests[1]!.messages)).toContain("approved");
    expect([...b.publicLog.values()].filter((event) => event.type === "system.message"))
      .toMatchObject([{ content: [{ text: 'Approved Bash arguments: {"command":"approved"}' }] }]);
    expect(JSON.stringify(b.requests[1]!.messages)).toContain("OMA approval changed Bash arguments");
    expect(JSON.stringify(b.requests[1]!.messages)).toContain(
      "actual stdout\\n",
    );
    const stored = await ctx.sessionPersistence.open(
      SessionId(session.id),
      "read",
    );
    const events = (await stored.read()).events;
    expect(events.find((e) => e.type === "tool/call")?.data).toMatchObject({
      arguments: '{"command":"original"}',
    });
    await stored.close();
    expect(session.state.phase).toBe("completed");
  });

  test("approval key order changes do not publish argument notices or add model context", async () => {
    const b = backend();
    const original = { file_path: "/tmp/approval-order.txt", content: "file content\n" };
    const effective = { content: original.content, file_path: original.file_path };
    const events = toolEvents();
    events[1]!.content_block.name = "Write";
    events[2]!.delta.partial_json = JSON.stringify(original);
    b.modelScript = [events, textEvents("done")];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({
        type: "control_response",
        response: {
          subtype: "success", request_id: p.request_id,
          response: { behavior: "allow", updatedInput: effective },
        },
      });
    };
    await session.receive("input_write", { message: { content: "write" } });
    expect(b.executions).toEqual([{ name: "write", arguments: effective }]);
    expect(session.state.approvedCalls.call_1).toEqual({ name: "Write", original, effective });
    expect([...b.publicLog.values()].filter((event) => event.type === "system.message")).toEqual([]);
    expect(JSON.stringify(b.requests[1]!.messages)).not.toContain("OMA approval changed");
    expect(session.state.phase).toBe("completed");
  });

  test("signed thinking survives the tool loop and fresh-host history restoration", async () => {
    const b = backend();
    const scripted = toolEvents();
    b.modelScript = [
      [
        scripted[0]!,
        {
          type: "content_block_start",
          index: 0,
          content_block: { type: "thinking", thinking: "" },
        },
        {
          type: "content_block_delta",
          index: 0,
          delta: { type: "thinking_delta", thinking: "test reasoning" },
        },
        {
          type: "content_block_delta",
          index: 0,
          delta: { type: "signature_delta", signature: "opaque-signature" },
        },
        { type: "content_block_stop", index: 0 },
        ...scripted
          .slice(1)
          .map((e) =>
            typeof e.index === "number" ? { ...e, index: e.index + 1 } : e,
          ),
      ],
      textEvents("done"),
    ];
    const first = await open(b);
    b.onApproval = async (p) => {
      await first.session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    await first.session.receive("thinking_tool", {
      message: { content: "run" },
    });
    await first.session.close();
    b.modelScript = [textEvents("restored")];
    const second = await open(b);
    await second.session.receive("thinking_resume", {
      message: { content: "continue" },
    });
    for (const request of b.requests.slice(1)) {
      expect(
        request.messages.flatMap((m: { content: unknown[] }) => m.content),
      ).toContainEqual({
        type: "thinking",
        thinking: "test reasoning",
        signature: "opaque-signature",
      });
    }
    expect(b.executions).toHaveLength(1);
    expect([...b.publicLog.values()].filter((p) => p.type === "agent.thinking").map((p) => p.content)).toEqual([
      undefined,
    ]);
    expect(JSON.stringify([...b.publicLog.values()])).not.toContain("opaque-signature");
    expect(JSON.stringify([...b.publicLog.values()])).not.toContain("test reasoning");
  });

  test("a follow-up retains the completed assistant reply before the next user message", async () => {
    const b = backend();
    b.modelScript = [toolEvents(), textEvents("first answer"), textEvents("second answer")];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: p.request_id, response: { behavior: "allow" },
      } });
    };
    await session.receive("first", { message: { content: "first user" } });
    await session.receive("second", { message: { content: "second user" } });
    const messages = b.requests[2]!.messages;
    const answerIndex = messages.findIndex((message: Payload) => message.role === "assistant" && JSON.stringify(message.content).includes("first answer"));
    const followupIndex = messages.findIndex((message: Payload) => message.role === "user" && JSON.stringify(message.content).includes("second user"));
    expect(answerIndex).toBeGreaterThanOrEqual(0);
    expect(followupIndex).toBeGreaterThan(answerIndex);
  });

  test("a new plugin host resumes the durable model history", async () => {
    const b = backend();
    const first = await open(b);
    await first.session.receive("first", {
      message: { content: "first user" },
    });
    await first.session.close();
    b.modelScript = [textEvents("second answer")];
    const second = await open(b);
    await second.session.receive("second", {
      message: { content: "second user" },
    });
    const history = JSON.stringify(b.requests[1]!.messages);
    expect(history).toContain("first user");
    expect(history).toContain("hello");
    expect(history).toContain("second user");
    expect(second.session.state.completed).toEqual(["first", "second"]);
  });

  test("a new plugin host resumes logs whose stored key order differs between batches", async () => {
    const jsonbOrder = (value: unknown): unknown => {
      if (Array.isArray(value)) return value.map(jsonbOrder);
      if (!value || typeof value !== "object") return value;
      const keys = Object.keys(value).sort((a, b) => a.length - b.length || (a < b ? -1 : a > b ? 1 : 0));
      return Object.fromEntries(keys.map((key) => [key, jsonbOrder((value as Payload)[key])]));
    };
    const b = backend();
    const first = await open(b);
    await first.session.receive("first", {
      message: { content: "first user " + "x".repeat(40_000) },
    });
    await first.session.close();
    const logs = [...b.privateLog.entries()].filter(([, payload]) => payload.subtype === "dsh_log");
    expect(logs.some(([, payload]) => JSON.stringify(payload).length > 32_768)).toBe(true);
    for (const [uuid, payload] of logs)
      if (JSON.stringify(payload).length <= 32_768) b.privateLog.set(uuid, jsonbOrder(payload) as Payload);
    const [, last] = logs.at(-1)!;
    b.privateLog.set("duplicate-dsh-log", { ...(jsonbOrder(last) as Payload), uuid: "duplicate-dsh-log" });
    b.modelScript = [textEvents("second answer")];
    const second = await open(b);
    await second.session.receive("second", {
      message: { content: "second user" },
    });
    const history = JSON.stringify(b.requests[1]!.messages);
    expect(history).toContain("first user");
    expect(history).toContain("second user");
    expect(second.session.state.completed).toEqual(["first", "second"]);
  });
});


test("native approval denial reason reaches every rejected tool and the next model without execution", async () => {
  for (const name of ["Bash", "list_agents", "unknown_tool"]) {
    const b = backend();
    const reason = "Use the staging project instead: rejection-" + name;
    b.a.launch.subagents = { inlineAgents: true, agents: [] };
    b.modelScript = [toolEvents().map((event: Payload) =>
      event.type === "content_block_start" ? { ...event, content_block: { ...event.content_block, name } } :
      event.type === "content_block_delta" ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify(name === "Bash" ? { command: "pwd" } : {}) } } : event), textEvents("denied")];
    const { session } = await open(b);
    b.onApproval = async (p) => {
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: p.request_id,
        response: { behavior: "deny", message: reason, denyMessage: reason },
      } });
    };
    await session.receive("reason-denied", { content: "try this tool" });
    expect(b.executions).toHaveLength(0);
    expect(b.sandboxCount).toBe(0);
    const results = [...b.publicLog.values()].filter(e => e.type === "user");
    expect(results).toHaveLength(1);
    expect(results[0]!.message.content[0]).toMatchObject({ is_error: true });
    expect(JSON.stringify(results[0]!.message.content[0])).toContain(reason);
    expect(b.requests).toHaveLength(2);
    expect(JSON.stringify(b.requests[1]!.messages)).toContain(reason);
  }
});


test("primary appended system context reaches current and later model requests and survives runtime recovery", async () => {
  const b = backend();
  b.a.launch.system = "ORIGINAL_AGENT_BASE_RULE";
  b.modelScript = [textEvents("first"), textEvents("second"), textEvents("recovered")];
  const { session } = await open(b);
  const first = { id: "system-one", text: "APPENDED_ID_RULE" };
  const second = { id: "system-two", text: "APPENDED_OK_RULE" };
  await session.receive("system-first", { content: "one", oma_system_context: [first] });
  expect(modelSystemText(b.requests[0]!)).toContain("ORIGINAL_AGENT_BASE_RULE");
  expect(modelSystemText(b.requests[0]!)).toContain(first.text);
  await session.receive("system-second", { content: "two", oma_system_context: [first, second] });
  const text = modelSystemText(b.requests[1]!);
  expect(text).toContain("ORIGINAL_AGENT_BASE_RULE");
  expect(text.indexOf(first.text)).toBeGreaterThan(-1);
  expect(text.indexOf(second.text)).toBeGreaterThan(text.indexOf(first.text));
  expect(JSON.stringify(b.requests[1]!.messages.filter((m: Payload) => m.role === "user"))).not.toContain(first.text);
  await session.close();
  const { session: recovered } = await open(b);
  await recovered.receive("system-recovered", { content: "three", oma_system_context: [first, second] });
  expect(modelSystemText(b.requests[2]!)).toContain(first.text);
  expect(modelSystemText(b.requests[2]!)).toContain(second.text);
  expect(b.a.launch.system).toBe("ORIGINAL_AGENT_BASE_RULE");
});
