import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import * as OmaSandboxProvider from "../src/oma-sandbox/index.ts";
import { SandboxMcp, sandboxCallTimeoutMs } from "../src/oma-sandbox/client.ts";
import * as SandboxTools from "../src/sandbox-tools/index.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import { assignment, Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

test("sandbox call timeout outlasts the requested Bash timeout", () => {
  const tools = sandboxToolDefinitions();
  const bash = tools.find((tool) => tool.name === "Bash")!;
  const read = tools.find((tool) => tool.name === "Read")!;
  expect(sandboxCallTimeoutMs(bash, { command: "true", timeout: 600 })).toBe(665_000);
  expect(sandboxCallTimeoutMs(bash, { command: "true", timeout: 3600 })).toBe(665_000);
  expect(sandboxCallTimeoutMs(bash, { command: "true", timeout: 300 })).toBe(365_000);
  expect(sandboxCallTimeoutMs(bash, { command: "true" })).toBe(185_000);
  expect(sandboxCallTimeoutMs(bash, { command: "true", timeout: "900" })).toBe(185_000);
  expect(sandboxCallTimeoutMs(read, { file_path: "/tmp/a", timeout: 600 })).toBe(185_000);
});

test("a silent Bash call outlives Bun's default socket idle timeout", async () => {
  const child = Bun.spawn([process.execPath, "tests/sandbox-idle-worker.ts"], {
    env: { ...process.env, BUN_CONFIG_HTTP_IDLE_TIMEOUT: "2", TEST_TOOL_DELAY_MS: "9000" },
    stdout: "pipe", stderr: "pipe",
  });
  const [output, code] = await Promise.all([new Response(child.stdout).text(), child.exited]);
  expect(code).toBe(0);
  expect(JSON.parse(output.trim().split("\n").at(-1)!)).toEqual({ ok: true, result: { content: [{ type: "text", text: "finished" }] } });
}, 30_000);

test("sandbox tool calls disable Bun's socket idle timeout and bound the request by the call timeout", async () => {
  const idleTimeouts: unknown[] = [];
  const deadlines: AbortSignal[] = [];
  const api = new OmaApi("http://oma.test", (async (input: RequestInfo | URL, init?: RequestInit & { timeout?: unknown }) => {
    const request = new Request(input, init);
    const body = request.method === "POST" ? await request.json() as Payload : {};
    if (body.method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
    if (body.method === "initialize") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "sandbox", version: "1" },
    } });
    if (body.method !== "tools/call") return new Response(null, { status: 202 });
    idleTimeouts.push(init?.timeout);
    if (init?.signal) deadlines.push(init.signal);
    return Response.json({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "ok" }] } });
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const executor = new SandboxMcp(api, a.session_id);
  const tools = sandboxToolDefinitions();
  try {
    await executor.call(tools.find((tool) => tool.name === "Bash")!, { command: "true", timeout: 600 }, new AbortController().signal);
    await executor.call(tools.find((tool) => tool.name === "Read")!, { file_path: "/tmp/a" }, new AbortController().signal);
    expect(idleTimeouts).toEqual([false, false]);
    expect(deadlines).toHaveLength(2);
    expect(deadlines.every((signal) => !signal.aborted)).toBe(true);
  } finally { await executor.close(); }
});

test("OMA providers isolate roots, initialize lazily and retain separate MCP sessions", async () => {
  const ctx = new Context();
  const requests: { id: string; method: string; mcpSession: string | null }[] = [];
  const api = new OmaApi("http://sandbox.test", (async (input, init) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    const id = path.split("/")[1]!;
    if (request.method === "GET") return new Response(null, { status: 405 });
    const body = await request.json() as Record<string, any>;
    const method = body.method;
    if (method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
    requests.push({ id, method, mcpSession: request.headers.get("Mcp-Session-Id") });
    expect(request.headers.get("Authorization")).toBe(`Bearer mcp_${id}`);
    expect(request.headers.has("X-OMA-MCP-Client-ID")).toBe(false);
    expect(request.headers.has("X-Brain-Epoch")).toBe(false);
    if (method === "notifications/initialized") return new Response(null, { status: 202 });
    return Response.json({
      jsonrpc: "2.0", id: body.id,
      result: method === "initialize"
        ? { protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "test", version: "1" } }
        : { content: [{ type: "text", text: id }] },
    }, { headers: { "Mcp-Session-Id": `mcp_${id}` } });
  }) as typeof fetch);
  const a = assignment();
  const b = assignment();
  for (const value of [a, b]) {
    value.mcp_config = { mcpServers: { sandbox: { type: "http", url: `http://sandbox.test/${value.session_id}/mcp`, headers: { Authorization: `Bearer mcp_${value.session_id}` } } } };
    api.assignments.set(value.session_id, value);
  }
  const scopeA = ctx.isolate("sandboxExecutor");
  const scopeB = ctx.isolate("sandboxExecutor");
  try {
    const providerA = await scopeA.plugin(OmaSandboxProvider, { api, sessionId: a.session_id });
    await scopeB.plugin(OmaSandboxProvider, { api, sessionId: b.session_id });
    const executorA = scopeA.get("sandboxExecutor")!;
    const executorB = scopeB.get("sandboxExecutor")!;
    expect(executorA).toBeDefined();
    expect(executorB).not.toBe(executorA);
    expect(ctx.get("sandboxExecutor")).toBeUndefined();
    expect(requests).toHaveLength(0);
    const tool = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;
    const signal = new AbortController().signal;
    for (const executor of [executorA, executorB, executorA]) {
      await executor.provision!(signal);
      await executor.call(tool, { command: "pwd" }, signal);
    }
    await providerA.dispose();
    expect(scopeA.get("sandboxExecutor")).toBeUndefined();
    expect(scopeB.get("sandboxExecutor")).toBe(executorB);
    await executorB.provision!(signal);
    await executorB.call(tool, { command: "still available" }, signal);
    for (const value of [a, b]) {
      const calls = requests.filter((entry) => entry.id === value.session_id);
      expect(calls.map((entry) => entry.method)).toEqual([
        "initialize", "notifications/initialized", "tools/call", "tools/call",
      ]);
      expect(calls.filter((entry) => entry.method === "tools/call").every(
        (entry) => entry.mcpSession === `mcp_${value.session_id}`,
      )).toBe(true);
    }
  } finally {
    await ctx.fiber.dispose();
  }
});

async function fixture(tools?: string[]) {
  const b = new Backend();
  if (tools) b.a.launch.tools = tools;
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  b.onApproval = async (payload) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  return {
    b, ctx, session,
    providers: () => [...(ctx.registry.get(OmaSandboxProvider)?.fibers ?? [])],
    consumers: () => [...(ctx.registry.get(SandboxTools)?.fibers ?? [])],
    async close() {
      try { await session.close(); }
      finally { await ctx.fiber.dispose(); b.stop(); }
    },
  };
}

test("children share the root provider and archiving a child preserves its executor and MCP connection", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    expect(f.providers()).toHaveLength(1);
    expect(f.b.mcpMethods).toEqual([]);
    const provider = f.providers()[0]!;
    const executor = provider.ctx.get("sandboxExecutor");
    f.b.modelScript = [toolEvents("child", "child_call"), textEvents("child report")];
    const report = JSON.parse(await f.session.subagents.send({
      name: "child", system_prompt: "Inspect files", message: "Inspect the workspace",
    }, new AbortController().signal));
    expect(report.report).toBe("child report");
    expect(f.providers()).toEqual([provider]);
    expect(f.consumers()).toHaveLength(2);
    for (const consumer of f.consumers()) expect(consumer.ctx.get("sandboxExecutor")).toBe(executor);
    const agent = f.session.agent;
    const archived = await agent.ctx.tools.execute({
      callId: ToolCallId("archive_child"), name: "archive_agent", arguments: { thread_id: report.thread_id },
      agent, signal: new AbortController().signal,
    });
    expect(archived.isError).toBe(false);
    expect(f.consumers()).toHaveLength(1);
    expect(provider.ctx.get("sandboxExecutor")).toBe(executor);
    f.b.modelScript = [toolEvents("parent", "parent_call"), textEvents("done")];
    await f.session.receive("parent_input", { content: "Continue" });
    expect(f.b.executions.map((call) => call.arguments.command)).toEqual(["child", "parent"]);
    expect(f.b.mcpMethods).toEqual(["initialize", "notifications/initialized", "tools/call", "tools/call"]);
    await f.session.close();
    expect(f.providers()).toHaveLength(0);
    expect(f.consumers()).toHaveLength(0);
    expect(provider.ctx.get("sandboxExecutor")).toBeUndefined();
  } finally { await f.close(); }
});

test("failed root setup rolls back the provider without allocating a sandbox", async () => {
  const f = await fixture();
  const setup = f.session.config.setup!;
  f.session.config.setup = async (scope) => {
    await setup(scope);
    expect(f.providers()).toHaveLength(1);
    throw new Error("setup failed after provider mounted");
  };
  try {
    await expect(f.session.open()).rejects.toThrow("setup failed after provider mounted");
    expect(f.providers()).toHaveLength(0);
    expect(f.consumers()).toHaveLength(0);
    expect(f.b.sandboxCount).toBe(0);
    expect(f.b.mcpMethods).toEqual([]);
  } finally { await f.close(); }
});

test("root close keeps the provider alive until a child's tool result checkpoint drains", async () => {
  const f = await fixture();
  const writing = Promise.withResolvers<void>();
  const confirmed = Promise.withResolvers<void>();
  let settled: Promise<unknown> | undefined;
  try {
    await f.session.open();
    const provider = f.providers()[0]!;
    const persistence = f.ctx.ccrv2.config.backend;
    const persist = persistence.persist.bind(persistence);
    persistence.persist = async (id, ...args) => {
      if ("type" in args[1] && args[1].type === "tool/result") {
        writing.resolve();
        await confirmed.promise;
      }
      await persist(id, ...args);
    };
    f.b.modelScript = [toolEvents("child", "draining_call"), textEvents("done")];
    settled = f.session.subagents.send({ name: "child", system_prompt: "Test", message: "Run" },
      new AbortController().signal).catch(() => {});
    await writing.promise;
    let closed = false;
    const closing = f.session.close().then(() => { closed = true; });
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(closed).toBe(false);
    expect(provider.ctx.get("sandboxExecutor")).toBeDefined();
    confirmed.resolve();
    await Promise.all([settled, closing]);
    expect(f.providers()).toHaveLength(0);
    expect(f.b.executions).toHaveLength(1);
  } finally {
    confirmed.resolve();
    await settled;
    await f.close();
  }
});

test("provider rejects absent, ambiguous or local-command MCP configuration", async () => {
  const { sandboxEndpoint } = await import("../src/oma-sandbox/config.ts");
  for (const config of [{}, { mcpServers: { other: { type: "http", url: "https://other.test/mcp" } } },
    { mcpServers: { sandbox: { command: "sh" } } },
    { mcpServers: { sandbox: { type: "http", url: "file:///tmp/mcp" } } }]) {
    expect(() => sandboxEndpoint(config)).toThrow();
  }
});

test("server-relative MCP configuration resolves against the OMA API origin", async () => {
  const { sandboxEndpoint } = await import("../src/oma-sandbox/config.ts");
  const endpoint = sandboxEndpoint({ mcpServers: { sandbox: {
    type: "http", url: "/v2/ccr-sessions/session/sandbox/mcp", headers: { Authorization: "Bearer session" },
  } } }, "https://oma.example");
  expect(endpoint.url).toBe("https://oma.example/v2/ccr-sessions/session/sandbox/mcp");
  expect(endpoint.headers).toEqual({ Authorization: "Bearer session" });
});


test.each(["user.interrupt", "control_request"])("confirmed %s ends as end_turn and permits another turn in the same session", async (type) => {
  const f = await fixture();
  const dispatched = Promise.withResolvers<void>();
  const cancelled = Promise.withResolvers<Record<string, unknown>>();
  try {
    await f.session.open();
    f.b.modelScript = [toolEvents("sleep 120", "cancel_call")];
    f.b.onTool = async () => { dispatched.resolve(); return cancelled.promise; };
    f.b.onCancel = async () => { cancelled.resolve({ isError: true, content: [{ type: "text", text: "cancelled" }], structuredContent: { status: "cancelled", cancelled: true } }); };
    const turn = f.session.receive("interrupt_input", { content: "Run until interrupted" });
    await dispatched.promise;
    await f.session.receive("interrupt_control", type === "control_request"
      ? { type, request_id: "interrupt_request", request: { subtype: "interrupt" } }
      : { type });
    await turn;
    expect(f.session.abort.signal.aborted).toBe(false);
    expect([...f.b.publicLog.values()].some((event) => event.type === "result" && event.is_error === true)).toBe(false);
    expect([...f.b.publicLog.values()].filter((event) => event.type === "result").map((event) => event.is_error)).toEqual([false]);
    expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message")).toBe(false);
    f.b.modelScript = [textEvents("RESUMED")];
    await f.session.receive("resume_input", { content: "Reply RESUMED without tools" });
    expect([...f.b.publicLog.values()].filter((event) => event.type === "result").at(-1)!.is_error).toBe(false);
    expect(f.b.executions).toHaveLength(1);
  } finally { cancelled.resolve({ isError: true, content: [{ type: "text", text: "cancelled" }] }); await f.close(); }
});


test("root model requests expose only the session's enabled sandbox tools", async () => {
  for (const tools of [[], ["Read"]]) {
    const f = await fixture(tools);
    try {
      await f.session.open();
      f.b.modelScript = [textEvents("done")];
      await f.session.receive("allowlist_input", { content: "Inspect available tools" });
      expect(f.b.requests).toHaveLength(1);
      expect((f.b.requests[0]!.tools ?? []).map((tool: { name: string }) => tool.name))
        .toEqual([...tools, "archive_agent", "list_agents", "send_to_agent"].sort());
      expect(f.b.sandboxCount).toBe(0);
      expect(f.b.mcpMethods).toEqual([]);
    } finally { await f.close(); }
  }
});

test("sandbox Bash projection preserves model and public tool output", async () => {
  const f = await fixture(["Bash"]);
  const stdout = "exact stdout\n";
  const metadata = { stdout, stderr: "", exit_code: 0, status: "completed", timed_out: false,
    cancelled: false, stdout_truncated: false, stderr_truncated: false };
  f.b.onTool = async () => ({ structuredContent: metadata, content: [
    { type: "text", text: stdout }, { type: "text", text: JSON.stringify(metadata) },
  ] });
  try {
    await f.session.open();
    f.b.modelScript = [toolEvents("printf exact", "projected-bash"), textEvents("done")];
    await f.session.receive("projection", { content: "Run Bash" });
    const request = JSON.stringify(f.b.requests[1]!.messages);
    expect(request).toContain("exact stdout");
    expect(request).not.toContain("exit_code");
    const results = [...f.b.publicLog.values()].flatMap(event => event.message?.content ?? [])
      .filter(block => block.type === "tool_result" && block.tool_use_id === "projected-bash");
    expect(results).toHaveLength(1);
    expect(results[0]!.content).toEqual([{ type: "text", text: stdout }]);
  } finally { await f.close(); }
});
