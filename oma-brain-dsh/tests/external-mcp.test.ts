


import { expect,test } from "bun:test";




import { mcpServerPrefix } from "../src/external-mcp/server-name.ts";

import { externalServers } from "../src/external-mcp/config.ts";

import { ExternalMcpClient } from "../src/external-mcp/client.ts";

import { toolEvents } from "./backend.ts";

import type { Payload } from "../src/json.ts";

function peer(transport: "http" | "sse" | "fallback" = "http", serverName = "peer") {
  const requests: Payload[] = [];
  const httpRequests: { method: string; path: string }[] = [];
  let failDiscovery = false;
  let authenticationStatus: number | undefined;
  let failCall = false;
  let errorResult = false;
  let outputText: string | undefined;
  let outputContent: Payload[] | undefined;
  let protocolErrorCode: number | undefined;
  let hangCall = false;
  let discoveryHold: Promise<void> | undefined;
  const listRequested = Promise.withResolvers<void>();
  const listCompleted = Promise.withResolvers<void>();
  const listAborted = Promise.withResolvers<void>();
  const dispatched = Promise.withResolvers<void>();
  let stream: ReadableStreamDefaultController<Uint8Array> | undefined;
  const disconnected = Promise.withResolvers<void>();
  const server = Bun.serve({
    port: 0,
    hostname: "127.0.0.1",
    async fetch(request) {
      const path = new URL(request.url).pathname;
      httpRequests.push({ method: request.method, path });
      if (transport === "fallback" && path === "/legacy" && request.method === "POST")
        return new Response(null, { status: authenticationStatus ?? 400 });
      if (
        request.method === "GET" &&
        (path === "/sse" || (transport === "fallback" && path === "/legacy"))
      ) {
        expect(request.headers.get("Authorization")).toBe("Bearer peer-secret");
        return new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              stream = controller;
              controller.enqueue(
                new TextEncoder().encode(
                  "event: endpoint\ndata: /messages\n\n",
                ),
              );
            },
            cancel() {
              disconnected.resolve();
            },
          }),
          { headers: { "Content-Type": "text/event-stream" } },
        );
      }
      if (request.method !== "POST") return new Response(null, { status: 405 });
      expect(request.headers.get("Authorization")).toBe("Bearer peer-secret");
      const body = (await request.json()) as Payload;
      requests.push(body);
      if (authenticationStatus !== undefined) return new Response(null, { status: authenticationStatus });
      if (body.method.startsWith("notifications/"))
        return new Response(null, { status: 202 });
      if (body.method === "server/discover")
        return new Response(null, { status: 404 });
      const reply = (result: Payload) => {
        const message = { jsonrpc: "2.0", id: body.id, result };
        if (transport === "http") return Response.json(message);
        stream!.enqueue(
          new TextEncoder().encode(
            `event: message\ndata: ${JSON.stringify(message)}\n\n`,
          ),
        );
        return new Response(null, { status: 202 });
      };
      if (body.method === "initialize")
        return reply({
          protocolVersion: "2024-11-05",
          capabilities: { tools: {} },
          serverInfo: { name: "peer", version: "1" },
        });
      if (body.method === "tools/list") {
        const aborted = Promise.withResolvers<void>();
        const onAbort = () => { listAborted.resolve(); aborted.resolve(); };
        request.signal.addEventListener("abort", onAbort, { once: true });
        try {
          if (request.signal.aborted) onAbort();
          listRequested.resolve();
          if (discoveryHold) await Promise.race([discoveryHold, aborted.promise]);
          if (request.signal.aborted) return new Response(null, { status: 499 });
        } finally {
          request.signal.removeEventListener("abort", onAbort);
        }
        if (failDiscovery) return new Response(null, { status: 503 });
        if (body.params?.cursor === "page2") listCompleted.resolve();
        const tool = {
          name: "microsoft_docs_fetch",
          description: "Fetch documentation",
          inputSchema: {
            type: "object",
            properties: { url: { type: "string" } },
            required: ["url"],
            additionalProperties: false,
          },
        };
        return reply(
          body.params?.cursor === "page2"
            ? {
                tools: [
                  { ...tool, name: "disabled" },
                  { ...tool, name: "second_page" },
                ],
              }
            : { tools: [tool], nextCursor: "page2" },
        );
      }
      if (body.method === "tools/call") {
        dispatched.resolve();
        if (hangCall) return new Promise<Response>(() => {});
        if (failCall) return new Response(null, { status: 503 });
        if (protocolErrorCode !== undefined)
          return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: protocolErrorCode, message: 'unknown tool "microsoft_docs_fetch"' } });
        return reply({
          content: outputContent ?? [
            {
              type: "text",
              text: outputText ?? `Learn document: ${body.params.arguments.url}`,
            },
          ],
          isError: errorResult,
        });
      }
      return new Response(null, { status: 404 });
    },
  });
  return {
    requests,
    httpRequests,
    output: (text: string) => { outputText = text; },
    content: (blocks: Payload[]) => { outputContent = blocks; },
    dispatched: dispatched.promise,
    disconnected: disconnected.promise,
    listRequested: listRequested.promise,
    listCompleted: listCompleted.promise,
    listAborted: listAborted.promise,
    holdDiscovery: (until: Promise<void>) => { discoveryHold = until; },
    hangCall: () => {
      hangCall = true;
    },
    config: {
      mcpServers: {
        [serverName]: {
          type: transport === "fallback" ? "http" : transport,
          url: new URL(transport === "sse" ? "/sse" : transport === "fallback" ? "/legacy" : "/", server.url).href,
          headers: { Authorization: "Bearer peer-secret" },
          tools: [{ name: "disabled", enabled: false }],
        },
      },
    },
    failAuthentication: (status: number) => { authenticationStatus = status; },
    recoverAuthentication: () => { authenticationStatus = undefined; },
    failDiscovery: () => {
      failDiscovery = true;
    },
    recoverDiscovery: () => { failDiscovery = false; },
    failCall: () => {
      failCall = true;
    },
    protocolError: (code?: number) => { protocolErrorCode = code; },
    errorResult: () => {
      errorResult = true;
    },
    close: () => server.stop(true),
  };
}

function mcpEvents(callId = "mcp_call"): Payload[] {
  const events = toolEvents("unused", callId);
  events[1]!.content_block.name = "mcp__peer__microsoft_docs_fetch";
  events[2]!.delta.partial_json = JSON.stringify({
    url: "https://learn.microsoft.com/example",
  });
  return events;
}

test("external configuration rejects unsupported transports, malformed headers and ambiguous names", () => {
  const server = { type: "http", url: "http://localhost/mcp" };
  for (const config of [
    { mcpServers: { peer: { ...server, type: "stdio" } } },
    { mcpServers: { peer: { ...server, url: "file:///secret" } } },
    { mcpServers: { peer: { ...server, headers: { Authorization: 42 } } } },
    {
      mcpServers: {
        peer: { ...server, tools: [{ name: "x", enabled: "false" }] },
      },
    },
    { mcpServers: { "peer.name": server, peer_name: server } },
  ])
    expect(() => externalServers(config)).toThrow();
});

async function isToolCall(request: Request): Promise<boolean> {
  if (request.method !== "POST") return false;
  return ((await request.json()) as Payload).method === "tools/call";
}

test("server prefixes count Unicode code points and reject collisions", () => {
  for (const name of ["", "测".repeat(256), "😀".repeat(256)]) {
    expect(() => mcpServerPrefix(name)).toThrow();
  }
  for (const name of ["测".repeat(255), "😀".repeat(255)]) {
    expect(mcpServerPrefix(name)).toMatch(/^mcp_[a-f0-9]{24}$/);
  }
  expect(mcpServerPrefix("peer.name")).toBe("peer_name");
  expect(mcpServerPrefix("peer\n")).toMatch(/^mcp_[a-f0-9]{24}$/);
  expect(mcpServerPrefix("é")).not.toBe(mcpServerPrefix("e\u0301"));
  const server = { type: "http", url: "https://example.com/mcp" };
  expect(() =>
    externalServers({
      mcpServers: {
        "Peer-测试": server,
        [mcpServerPrefix("Peer-测试")]: server,
      },
    }),
  ).toThrow();
});

function within<T>(promise: Promise<T>, ms: number): Promise<T> {
  return Promise.race([
    promise,
    new Promise<T>((_, reject) => setTimeout(() => reject(new Error(`not settled within ${ms}ms`)), ms)),
  ]);
}

for (const status of [401, 403]) {
  test(`URL MCP authentication ${status} never falls back to legacy SSE`, async () => {
    const p = peer("fallback");
    const client = new ExternalMcpClient(externalServers(p.config)[0]);
    try {
      p.failAuthentication(status);
      await expect(client.discover(new AbortController().signal)).rejects.toThrow();
      expect(client.authenticationFailed).toBe(true);
      expect(p.httpRequests.some(request => request.method === "GET")).toBe(false);
    } finally { await client.close(); p.close(); }
  });
}
