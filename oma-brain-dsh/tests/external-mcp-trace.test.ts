import { expect, test } from "bun:test";
import { ExternalMcpClient } from "../src/external-mcp/client.ts";
import { traceHooks } from "../src/observability/trace-context.ts";

test("MCP tools/call carries the tool span trace context only inside a traced dispatch", async () => {
  const metas: unknown[] = [];
  const server = Bun.serve({ port: 0, hostname: "127.0.0.1", async fetch(request) {
    if (request.method !== "POST") return new Response(null, { status: 405 });
    const body = await request.json() as { id?: number; method: string; params?: { _meta?: unknown } };
    const reply = (result: unknown) => Response.json({ jsonrpc: "2.0", id: body.id, result });
    if (body.method === "server/discover") return new Response(null, { status: 404 });
    if (body.method === "initialize") return reply({ protocolVersion: "2025-11-25", capabilities: { tools: {} }, serverInfo: { name: "trace-peer", version: "1" } });
    if (body.method.startsWith("notifications/")) return new Response(null, { status: 202 });
    if (body.method === "tools/list") return reply({ tools: [{ name: "lookup", inputSchema: { type: "object", properties: {} } }] });
    if (body.method === "tools/call") {
      metas.push(body.params?._meta);
      return reply({ content: [{ type: "text", text: "ok" }] });
    }
    return new Response(null, { status: 404 });
  } });
  const client = new ExternalMcpClient({ name: "Trace Peer", prefix: "trace_peer", type: "http", url: server.url, headers: {}, disabledTools: new Set() });
  const previous = traceHooks.target;
  try {
    const signal = new AbortController().signal;
    const tools = await client.discover(signal);
    traceHooks.target = {
      inputReceived: () => {}, openTurns: () => [], turnsInterrupted: () => {}, approvalPending: () => {}, approvalResolved: () => {}, subagent: () => {},
      toolDispatched: (sessionId, callId) => sessionId === "cse_traced" && callId === "call_1"
        ? { traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", tracestate: "vendor=a" } : undefined,
    };
    await traceHooks.runTool("cse_traced", "call_1", () => client.call(tools[0]!, {}, signal));
    await traceHooks.runTool("cse_other", "call_2", () => client.call(tools[0]!, {}, signal));
    await client.call(tools[0]!, {}, signal);
    expect(metas[0]).toMatchObject({ traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", tracestate: "vendor=a" });
    for (const meta of metas.slice(1)) expect((meta as { traceparent?: string } | undefined)?.traceparent).toBeUndefined();
    expect(traceHooks.mcpServerName("trace_peer")).toBe("Trace Peer");
    traceHooks.target = { ...traceHooks.target, toolDispatched: () => { throw new Error("broken telemetry"); } };
    expect((await traceHooks.runTool("cse_traced", "call_1", () => client.call(tools[0]!, {}, signal))).content).toEqual([{ type: "text", text: "ok" }]);
  } finally {
    traceHooks.target = previous;
    await client.close();
    server.stop(true);
  }
});
