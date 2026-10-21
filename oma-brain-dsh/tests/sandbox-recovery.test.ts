import { expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { SandboxMcp } from "../src/oma-sandbox/client.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import { ToolNotForwardedError } from "../src/runtime/contracts.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

async function clientFixture() {
  const backend = new Backend();
  await backend.ready;
  const api = new OmaApi(backend.url);
  api.assignments.set(backend.a.session_id, backend.a);
  const client = new SandboxMcp(api, backend.a.session_id);
  return { backend, client, async close() { await client.close(); backend.stop(); } };
}

async function sessionFixture() {
  const backend = new Backend();
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  backend.onApproval = async (payload) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  await session.open();
  return { backend, session, async close() {
    try { await session.close(); } finally { await ctx.fiber.dispose(); backend.stop(); }
  } };
}

function modelResults(request: Payload): Payload[] {
  return request.messages.flatMap((message: Payload) => Array.isArray(message.content) ? message.content : [])
    .filter((block: Payload) => block.type === "tool_result");
}

test.each([405, 503])("failed HTTP %s initialization reconnects on the next attempt", async (status) => {
  const f = await clientFixture();
  let attempts = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method === "initialize" && ++attempts === 1)
      return new Response("gateway unavailable", { status });
  };
  try {
    await expect(f.client.provision(new AbortController().signal)).rejects.toThrow();
    await f.client.provision(new AbortController().signal);
    expect(attempts).toBe(2);
  } finally { await f.close(); }
});

test("a failed modern discovery reconnects without a legacy initialize", async () => {
  const f = await clientFixture();
  let attempts = 0;
  f.backend.onSandbox = async (body, request) => {
    if (body.method === "server/discover") {
      if (++attempts === 1) return new Response("unavailable", { status: 503 });
      return Response.json({ jsonrpc: "2.0", id: body.id, result: {
        resultType: "complete", supportedVersions: ["2026-07-28"], capabilities: { tools: {} }, ttlMs: 0, cacheScope: "private",
      } });
    }
    if (body.method === "tools/call") {
      expect(request.headers.get("Mcp-Protocol-Version")).toBe("2026-07-28");
      return Response.json({ jsonrpc: "2.0", id: body.id, result: { resultType: "complete", content: [{ type: "text", text: "modern result" }] } });
    }
    if (body.method === "tools/list") {
      return Response.json({ jsonrpc: "2.0", id: body.id, result: { resultType: "complete", ttlMs: 0, cacheScope: "private", tools: [] } });
    }
    return undefined;
  };
  try {
    await expect(f.client.provision(new AbortController().signal)).rejects.toThrow("HTTP 503");
    const read = sandboxToolDefinitions().find((tool) => tool.name === "Read")!;
    const result = await f.client.call(read, { file_path: "/workspace/file" }, new AbortController().signal);
    expect(result.content[0].text).toBe("modern result");
    expect(attempts).toBe(2);
    expect(f.backend.mcpMethods).not.toContain("initialize");
  } finally { await f.close(); }
});

test("cancelling one initialization waiter leaves the shared handshake available", async () => {
  const f = await clientFixture();
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  let attempts = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method === "initialize") { attempts++; started.resolve(); await release.promise; }
    return undefined;
  };
  const abort = new AbortController();
  const first = f.client.provision(abort.signal).catch((error: unknown) => error);
  try {
    await started.promise;
    const second = f.client.provision(new AbortController().signal);
    abort.abort(new Error("cancelled waiter"));
    expect(await first).toBeInstanceOf(Error);
    release.resolve();
    await second;
    await f.client.provision(new AbortController().signal);
    expect(attempts).toBe(1);
  } finally { release.resolve(); await first; await f.close(); }
});

test("provider close cancels an unfinished handshake and prevents reconnect", async () => {
  const f = await clientFixture();
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  f.backend.onSandbox = async (body) => {
    if (body.method === "initialize") { started.resolve(); await release.promise; }
    return undefined;
  };
  const pending = f.client.provision(new AbortController().signal).catch((error: unknown) => error);
  try {
    await started.promise;
    await f.client.close();
    expect(await pending).toBeInstanceOf(Error);
    await expect(f.client.provision(new AbortController().signal)).rejects.toThrow();
  } finally { release.resolve(); await pending; await f.close(); }
});

test("a tool HTTP failure preserves the connection and an in-flight sibling result", async () => {
  const f = await clientFixture();
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  const read = sandboxToolDefinitions().find((tool) => tool.name === "Read")!;
  f.backend.onSandbox = async (body) => {
    if (body.method !== "tools/call") return undefined;
    if (body.params.arguments.file_path === "slow") {
      started.resolve(); await release.promise;
      return Response.json({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "sibling result" }] } });
    }
    return new Response("unavailable", { status: 503 });
  };
  const sibling = f.client.call(read, { file_path: "slow" }, new AbortController().signal);
  try {
    await started.promise;
    await expect(f.client.call(read, { file_path: "failed" }, new AbortController().signal)).rejects.toThrow("HTTP 503");
    release.resolve();
    expect((await sibling).content[0].text).toBe("sibling result");
    await f.client.provision(new AbortController().signal);
    expect(f.backend.sandboxCount).toBe(1);
  } finally { release.resolve(); await sibling.catch(() => {}); await f.close(); }
});

test.each(["plain", "oversized", "sensitive", "json"])("not-forwarded %s errors reach the model without retry or connection reset", async (kind) => {
  const f = await sessionFixture();
  let calls = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method !== "tools/call") return undefined;
    calls++;
    const text = kind === "oversized" ? JSON.stringify({ error: { message: "x".repeat(4096) } })
      : kind === "sensitive" ? JSON.stringify({ error: { message: "secret=hidden" } })
      : kind === "json" ? JSON.stringify({ error: { message: "Write denied" } }) : "<html>upstream details</html>";
    return new Response(text, { status: 405, headers: { "X-Oma-Sandbox-Not-Forwarded": "true" } });
  };
  try {
    f.backend.modelScript = [toolEvents("write", "rejected_call"), textEvents("understood")];
    await f.session.receive("rejected_input", { content: "Write" });
    const result = modelResults(f.backend.requests[1]!)[0]!;
    expect(result.is_error).toBe(true);
    const text = JSON.stringify(result.content);
    expect(text).toContain("HTTP 405");
    expect(text).toContain("not executed");
    expect(text.length).toBeLessThan(700);
    expect(text).not.toContain("secret=hidden");
    expect(text).not.toContain("<html>");
    if (kind === "json") expect(text).toContain("Write denied");
    expect(calls).toBe(1);
    expect(f.backend.sandboxCount).toBe(1);
    expect(f.session.blocked()).toBe(false);
  } finally { await f.close(); }
});

test("initialization failure reaches the model without dispatch and the next turn reconnects", async () => {
  const f = await sessionFixture();
  let attempts = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method === "initialize" && ++attempts === 1)
      return new Response("unavailable", { status: 503 });
  };
  try {
    f.backend.modelScript = [toolEvents("first", "failed_init"), textEvents("understood")];
    await f.session.receive("first_input", { content: "Run" });
    const result = modelResults(f.backend.requests[1]!)[0]!;
    expect(result.is_error).toBe(true);
    expect(JSON.stringify(result.content)).toContain("HTTP 503");
    expect(JSON.stringify(result.content)).toContain("not executed");
    expect(f.backend.executions).toHaveLength(0);
    expect(f.session.state.toolOutcomes.failed_init?.failed).toBe(true);
    expect(f.session.state.executions![f.session.id]!.callPhases!.failed_init).toBe("result");
    expect(f.session.blocked()).toBe(false);
    f.backend.modelScript = [toolEvents("second", "recovered_call"), textEvents("done")];
    await f.session.receive("second_input", { content: "Retry" });
    expect(attempts).toBe(2);
    expect(f.backend.executions).toHaveLength(1);
    expect(f.backend.publicLog.get("second_input_result")?.is_error).toBe(false);
  } finally { await f.close(); }
});

test("read-only HTTP failure retains the status in model feedback after one retry", async () => {
  const f = await sessionFixture();
  let calls = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method === "tools/call") { calls++; return new Response("proxy timeout", { status: 504 }); }
  };
  try {
    const events = toolEvents("unused", "read_failure");
    events[1]!.content_block.name = "Read";
    events[2]!.delta.partial_json = JSON.stringify({ file_path: "/workspace/file" });
    f.backend.modelScript = [events, textEvents("understood")];
    await f.session.receive("read_input", { content: "Read" });
    const result = modelResults(f.backend.requests[1]!)[0]!;
    expect(result.is_error).toBe(true);
    expect(JSON.stringify(result.content)).toContain("HTTP 504");
    expect(calls).toBe(2);
    expect(f.session.blocked()).toBe(false);
  } finally { await f.close(); }
});

test("child initialization failure settles its own call and the parent can reconnect", async () => {
  const f = await sessionFixture();
  let attempts = 0;
  f.backend.onSandbox = async (body) => {
    if (body.method === "initialize" && ++attempts === 1) return new Response("unavailable", { status: 503 });
  };
  try {
    f.backend.modelScript = [toolEvents("child", "child_init"), textEvents("child recovered")];
    const report = JSON.parse(await f.session.subagents.send({
      name: "child", system_prompt: "Test", message: "Run",
    }, new AbortController().signal));
    const entry = f.session.state.executions![report.thread_id]!;
    expect(entry.callPhases!.child_init).toBe("result");
    expect(entry.toolOutcomes.child_init?.text).toContain("HTTP 503");
    expect(f.backend.executions).toHaveLength(0);
    expect(f.session.blocked()).toBe(false);
    f.backend.modelScript = [toolEvents("parent", "parent_reconnect"), textEvents("done")];
    await f.session.receive("parent_input", { content: "Run" });
    expect(attempts).toBe(2);
    expect(f.backend.executions).toHaveLength(1);
  } finally { await f.close(); }
});

test.each([405, 504])("forwarded write HTTP %s stays blocked and publishes a safe diagnostic without replay", async (status) => {
  const f = await sessionFixture();
  let calls = 0;
  const diagnostics: string[] = [];
  const log = spyOn(console, "error").mockImplementation((value: unknown) => { diagnostics.push(String(value)); });
  f.backend.onSandbox = async (body) => {
    if (body.method === "tools/call") {
      calls++;
      return new Response("private upstream body secret=hidden", { status });
    }
  };
  try {
    f.backend.modelScript = [toolEvents("write", "unknown_call")];
    await f.session.receive("unknown_input", { content: "Write" }).catch(() => {});
    expect(f.session.blocked()).toBe(true);
    const messages = [...f.backend.publicLog.values()].filter((event) => event.type === "system.message");
    expect(JSON.stringify(messages)).toContain(`HTTP ${status}`);
    expect(JSON.stringify(messages)).toContain("unknown");
    expect(JSON.stringify(messages)).not.toContain("secret=hidden");
    expect(diagnostics.map((line) => JSON.parse(line))).toContainEqual({
      event: "ccrv2_sandbox_outcome_unknown", session_id: f.session.id, session_thread_id: f.session.id,
      call_id: "unknown_call", failure_phase: "tool_call", http_status: status,
    });
    expect(JSON.stringify(diagnostics)).not.toContain("secret=hidden");
    await expect(f.session.receive("retry_input", { content: "Retry" })).rejects.toThrow();
    expect(calls).toBe(1);
    expect(f.backend.requests).toHaveLength(1);
  } finally { log.mockRestore(); await f.close(); }
});

test("a connection closed before dispatch rejects the call as not executed", async () => {
  const f = await clientFixture();
  const bash = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;
  try {
    await f.client.provision(new AbortController().signal);
    const pending = f.client.call(bash, { command: "touch once" }, new AbortController().signal);
    void (f.client as unknown as { connection: { client: { close(): Promise<void> } } }).connection.client.close();
    const error = await pending.catch((reason: unknown) => reason);
    expect(error).toBeInstanceOf(ToolNotForwardedError);
    expect((error as Error).message).toContain("not executed");
    expect(f.backend.mcpMethods).not.toContain("tools/call");
  } finally { await f.close(); }
});
