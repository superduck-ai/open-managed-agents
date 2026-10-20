import { expect, test } from "bun:test";
import { ExternalMcpClient } from "../src/external-mcp/client.ts";

function statefulPeer(rejection: number, issueSession = true, rejectFresh = false) {
  const initializes: (string | null)[] = [];
  const calls: (string | null)[] = [];
  let active = "sdk-session-a";
  let expired = false;
  let executed = 0;
  const server = Bun.serve({ port: 0, hostname: "127.0.0.1", async fetch(request) {
    if (request.method !== "POST") return new Response(null, { status: 405 });
    const body = await request.json() as { id?: number; method: string };
    const sid = request.headers.get("mcp-session-id");
    const reply = (result: unknown, session?: string) => Response.json(
      { jsonrpc: "2.0", id: body.id, result },
      { headers: session ? { "Mcp-Session-Id": session } : {} },
    );
    if (body.method === "server/discover") return new Response(null, { status: 404 });
    if (body.method === "initialize") {
      initializes.push(sid);
      if (expired) active = "sdk-session-b";
      return reply({ protocolVersion: "2025-11-25", capabilities: { tools: {} }, serverInfo: { name: "owned-session-control", version: "1" } }, issueSession ? active : undefined);
    }
    if (body.method.startsWith("notifications/")) return new Response(null, { status: 202 });
    if (body.method === "tools/list") return reply({ tools: [{ name: "read_record", description: "Read owned record", inputSchema: { type: "object", properties: {}, additionalProperties: false } }] });
    if (body.method === "tools/call") {
      calls.push(sid);
      if (expired && (!issueSession || sid !== "sdk-session-b" || rejectFresh)) return new Response("Session rejected", { status: rejection });
      executed++;
      return reply({ content: [{ type: "text", text: "actual-owned-record" }] });
    }
    return new Response(null, { status: 404 });
  } });
  const client = new ExternalMcpClient({ name: "peer", prefix: "peer", type: "http", url: server.url, headers: {}, disabledTools: new Set() });
  return { client, initializes, calls, expire: () => { expired = true; }, executed: () => executed, close: async () => { await client.close(); server.stop(true); } };
}

test("expired stateful MCP404 reinitializes without old ID before one safe dispatch", async () => {
  const p = statefulPeer(404);
  try {
    const signal = new AbortController().signal;
    const tools = await p.client.discover(signal);
    await p.client.call(tools[0]!, {}, signal);
    p.expire();
    const result = await p.client.call(tools[0]!, {}, signal);
    expect(result.content).toEqual([{ type: "text", text: "actual-owned-record" }]);
    expect(p.initializes).toEqual([null, null]);
    expect(p.calls).toEqual(["sdk-session-a", "sdk-session-a", "sdk-session-b"]);
    expect(p.executed()).toBe(2);
  } finally { await p.close(); }
});

for (const [status, issueSession] of [[503, true], [404, false]] as const) {
  test(`external MCP ${status} with session=${issueSession} preserves no-replay for uncertain failures`, async () => {
    const p = statefulPeer(status, issueSession);
    try {
      const signal = new AbortController().signal;
      const tools = await p.client.discover(signal);
      await p.client.call(tools[0]!, {}, signal);
      p.expire();
      await expect(p.client.call(tools[0]!, {}, signal)).rejects.toThrow();
      expect(p.initializes).toEqual([null]);
      expect(p.calls).toHaveLength(2);
      expect(p.executed()).toBe(1);
    } finally { await p.close(); }
  });
}


test("a second stateful404 never causes an unbounded replay loop", async () => {
  const p = statefulPeer(404, true, true);
  try {
    const signal = new AbortController().signal;
    const tools = await p.client.discover(signal);
    await p.client.call(tools[0]!, {}, signal);
    p.expire();
    await expect(p.client.call(tools[0]!, {}, signal)).rejects.toThrow();
    expect(p.initializes).toEqual([null, null]);
    expect(p.calls).toEqual(["sdk-session-a", "sdk-session-a", "sdk-session-b"]);
    expect(p.executed()).toBe(1);
  } finally { await p.close(); }
});
