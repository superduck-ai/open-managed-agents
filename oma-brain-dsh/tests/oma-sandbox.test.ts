

import { expect,test } from "bun:test";

import { Context } from "@deepseek-ai/cordis";

import { OmaApi } from "../src/api.ts";

import * as OmaSandboxProvider from "../src/oma-sandbox/index.ts";

import { sandboxCallTimeoutMs,SandboxMcp } from "../src/oma-sandbox/client.ts";


import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";

import { assignment } from "./backend.ts";

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

function flakySandbox(options: { failFirstPost?: boolean; holdDiscover?: Promise<void> } = {}) {
  const requests: string[] = [];
  let posts = 0;
  const api = new OmaApi("http://oma.test", (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.method !== "POST") return new Response(null, { status: 405 });
    const body = await request.json() as Payload;
    posts++;
    if (options.failFirstPost && posts === 1) {
      requests.push(`503:${body.method}`);
      return Response.json({ type: "error", error: { type: "api_error", message: "Sandbox is unavailable" } }, { status: 503 });
    }
    requests.push(body.method);
    if (body.method === "server/discover") {
      await options.holdDiscover;
      return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
    }
    if (body.method === "initialize") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "sandbox", version: "1" },
    } });
    if (body.method !== "tools/call") return new Response(null, { status: 202 });
    return Response.json({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "ok" }] } });
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  return { requests, executor: new SandboxMcp(api, a.session_id) };
}

test("a failed sandbox MCP initialization is retried by the next call", async () => {
  const { requests, executor } = flakySandbox({ failFirstPost: true });
  const bash = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;
  try {
    await expect(executor.call(bash, { command: "true" }, new AbortController().signal)).rejects.toThrow("HTTP 503");
    expect(await executor.call(bash, { command: "true" }, new AbortController().signal)).toEqual({ content: [{ type: "text", text: "ok" }] });
    expect(await executor.call(bash, { command: "true" }, new AbortController().signal)).toEqual({ content: [{ type: "text", text: "ok" }] });
    expect(requests).toEqual(["503:server/discover", "server/discover", "initialize", "notifications/initialized", "tools/call", "tools/call"]);
  } finally { await executor.close(); }
});

test("interrupting the first caller does not fail the shared sandbox MCP initialization", async () => {
  let release!: () => void;
  const { requests, executor } = flakySandbox({ holdDiscover: new Promise<void>((resolve) => { release = resolve; }) });
  const bash = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;
  const interrupted = new AbortController();
  try {
    const first = executor.call(bash, { command: "true" }, interrupted.signal);
    const second = executor.call(bash, { command: "true" }, new AbortController().signal);
    interrupted.abort(new Error("user interrupt"));
    await expect(first).rejects.toThrow("user interrupt");
    release();
    expect(await second).toEqual({ content: [{ type: "text", text: "ok" }] });
    expect(requests).toEqual(["server/discover", "initialize", "notifications/initialized", "tools/call"]);
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
    type: "http", url: "/v1/dsr/sessions/session/sandbox/mcp", headers: { Authorization: "Bearer session" },
  } } }, "https://oma.example");
  expect(endpoint.url).toBe("https://oma.example/v1/dsr/sessions/session/sandbox/mcp");
  expect(endpoint.headers).toEqual({ Authorization: "Bearer session" });
});
