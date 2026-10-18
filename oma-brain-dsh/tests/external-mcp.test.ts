import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { mcpServerPrefix } from "../src/external-mcp/server-name.ts";
import { externalServers } from "../src/external-mcp/config.ts";
import { registerWorkerTask } from "../src/worker-registration.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function peer(transport: "http" | "sse" = "http", serverName = "peer") {
  const requests: Payload[] = [];
  let failDiscovery = false;
  let authenticationStatus: number | undefined;
  let failCall = false;
  let errorResult = false;
  let protocolErrorCode: number | undefined;
  let hangCall = false;
  const dispatched = Promise.withResolvers<void>();
  let stream: ReadableStreamDefaultController<Uint8Array> | undefined;
  const disconnected = Promise.withResolvers<void>();
  const server = Bun.serve({
    port: 0,
    hostname: "127.0.0.1",
    async fetch(request) {
      if (
        request.method === "GET" &&
        new URL(request.url).pathname === "/sse"
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
        if (failDiscovery) return new Response(null, { status: 503 });
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
          content: [
            {
              type: "text",
              text: `Learn document: ${body.params.arguments.url}`,
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
    dispatched: dispatched.promise,
    disconnected: disconnected.promise,
    hangCall: () => {
      hangCall = true;
    },
    config: {
      mcpServers: {
        [serverName]: {
          type: transport,
          url: new URL(transport === "sse" ? "/sse" : "/", server.url).href,
          headers: { Authorization: "Bearer peer-secret" },
          tools: [{ name: "disabled", enabled: false }],
        },
      },
    },
    failAuthentication: (status: number) => { authenticationStatus = status; },
    failDiscovery: () => {
      failDiscovery = true;
    },
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

async function fixture(
  transport: "http" | "sse" = "http",
  serverName = "peer",
  extraServers: Payload = {},
) {
  const p = peer(transport, serverName);
  const b = new Backend();
  await b.ready;
  const route = b.route.bind(b);
  b.route = async (request) =>
    new URL(request.url).pathname === `/v2/sessions/${b.a.session_id}`
      ? Response.json({
          session_context: {
            cwd: "/workspace",
            model: "test-model",
            runtime_tools: [],
            mcp_config: { mcpServers: { ...p.config.mcpServers, ...extraServers } },
          },
        })
      : route(request);
  const ctx = new Context();
  const api = new OmaApi(b.url, (async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (url.pathname === `/v2/ccr-sessions/${b.a.session_id}/mcp`) {
      expect(request.headers.get("Authorization")).toBe(`Bearer ${b.a.ingress_token}`);
      const upstream = new Request(url.searchParams.get("mcp_url")!, request);
      upstream.headers.set("Authorization", "Bearer peer-secret");
      return fetch(upstream, { redirect: "error" });
    }
    return fetch(request);
  }) as typeof fetch);
  await mountOma(ctx, api);
  const a = await registerWorkerTask(api, b.a);
  const session = createOmaSession(ctx, api, a);
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
  return {
    p,
    b,
    a,
    session,
    ctx,
    api,
    async close() {
      try {
        await session.close();
      } finally {
        await ctx.fiber.dispose();
        b.stop();
        p.close();
      }
    },
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

test("external discovery failure stops initialization before any model request or sandbox allocation", async () => {
  const f = await fixture();
  try {
    f.p.failDiscovery();
    await expect(f.session.open()).rejects.toThrow(
      "External MCP discovery failed",
    );
    expect(f.b.requests).toHaveLength(0);
    expect(f.b.sandboxCount).toBe(0);
  } finally {
    await f.close();
  }
});

test("denied external tools and invalid approved arguments never dispatch", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    let allow = false;
    f.b.onApproval = async (payload) => {
      await f.session.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: allow
            ? { behavior: "allow", updatedInput: { url: 42 } }
            : { behavior: "deny" },
        },
      });
    };
    const agent = f.session.agent;
    const execute = (id: string, args: Payload) =>
      agent.ctx.tools.execute({
        agent,
        name: "mcp__peer__microsoft_docs_fetch",
        arguments: args,
        callId: ToolCallId(id),
        signal: new AbortController().signal,
      });
    expect((await execute("bad_schema", {})).isError).toBe(true);
    expect(
      (await execute("denied", { url: "https://example.com" })).isError,
    ).toBe(true);
    allow = true;
    expect(
      (await execute("bad_approval", { url: "https://example.com" })).isError,
    ).toBe(true);
    expect(
      f.p.requests.some((request) => request.method === "tools/call"),
    ).toBe(false);
  } finally {
    await f.close();
  }
});

test("external transport failure records unknown and blocks the turn without replay", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    f.p.failCall();
    f.b.modelScript = [mcpEvents(), textEvents("must not continue")];
    await expect(
      f.session.receive("lost_result", { content: "Fetch documentation" }),
    ).rejects.toThrow();
    expect(f.session.blocked()).toBe(true);
    expect(
      f.p.requests.filter((request) => request.method === "tools/call"),
    ).toHaveLength(1);
    expect(f.b.requests).toHaveLength(1);
    expect(f.b.sandboxCount).toBe(0);
  } finally {
    await f.close();
  }
});

test("interrupting an external call finishes locally and permits a new turn without replay", async () => {
  const f = await fixture();
  let turn: Promise<unknown> | undefined;
  try {
    await f.session.open();
    f.p.hangCall();
    f.b.modelScript = [mcpEvents()];
    turn = f.session
      .receive("interrupted", { content: "Fetch documentation" })
      .catch((error) => error);
    await f.p.dispatched;
    await f.session.control({ type: "interrupt" });
    await turn;
    expect(f.session.blocked()).toBe(false);
    expect(Object.values(f.session.state.toolOutcomes)).toContainEqual({
      text: "Tool call interrupted.",
      failed: true,
      remoteOutcome: "unknown",
    });
    expect(
      f.p.requests.filter((request) => request.method === "tools/call"),
    ).toHaveLength(1);
    expect(f.b.requests).toHaveLength(1);
    f.b.modelScript = [textEvents("continued")];
    await f.session.receive("next", { content: "Continue" });
    expect(f.b.requests).toHaveLength(2);
    expect(
      f.p.requests.filter((request) => request.method === "tools/call"),
    ).toHaveLength(1);
  } finally {
    await f.close();
    await turn;
  }
});

test("external MCP error results are recorded as failed results rather than unknown outcomes", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    f.p.errorResult();
    f.b.modelScript = [
      mcpEvents(),
      textEvents("The server rejected the request"),
    ];
    await f.session.receive("error_result", { content: "Fetch documentation" });
    expect(f.session.blocked()).toBe(false);
    expect(f.session.state.toolOutcomes?.mcp_call?.failed).toBe(true);
    const events = [...f.b.publicLog.values()];
    expect(
      events.some((event) =>
        event.message?.content?.some(
          (block: Payload) =>
            block.tool_use_id === "mcp_call" && block.is_error,
        ),
      ),
    ).toBe(true);
  } finally {
    await f.close();
  }
});

test("registration retains external config and discovers all pages before advertising tools to the model", async () => {
  const f = await fixture();
  try {
    expect(f.a.external_mcp_config).toEqual(f.p.config);
    expect(f.a.mcp_config).not.toEqual(f.p.config);
    await f.session.open();
    f.b.modelScript = [mcpEvents(), textEvents("Document fetched")];
    await f.session.receive("mcp_input", { content: "Fetch documentation" });
    const advertised = f.b.requests[0]!.tools.map((tool: Payload) => tool.name);
    expect(advertised).toContain("mcp__peer__microsoft_docs_fetch");
    expect(advertised).toContain("mcp__peer__second_page");
    expect(advertised).not.toContain("mcp__peer__disabled");
    expect(advertised).not.toContain("Bash");
    const calls = f.p.requests.filter(
      (request) => request.method === "tools/call",
    );
    expect(calls).toHaveLength(1);
    expect(calls[0]!.params).toMatchObject({
      name: "microsoft_docs_fetch",
      arguments: { url: "https://learn.microsoft.com/example" },
    });
    expect(
      f.p.requests.filter((request) => request.method === "tools/list"),
    ).toHaveLength(2);
    const events = [...f.b.publicLog.values()];
    expect(
      events.some(
        (event) =>
          event.request?.tool_name === "mcp__peer__microsoft_docs_fetch",
      ),
    ).toBe(true);
    const result = events
      .flatMap((event) => event.message?.content ?? [])
      .find((block) => block.type === "tool_result");
    expect(result).toMatchObject({
      tool_use_id: "mcp_call",
      tool_name: "mcp__peer__microsoft_docs_fetch",
      is_error: false,
      content: [
        {
          type: "text",
          text: "Learn document: https://learn.microsoft.com/example",
        },
      ],
    });
    expect(f.b.sandboxCount).toBe(0);
  } finally {
    await f.close();
  }
});

test("children reuse external discovery with their own approval and result ownership", async () => {
  const f = await fixture();
  try {
    f.a.launch.subagents = { inlineAgents: true };
    f.session.config.subagents = f.a.launch.subagents;
    await f.session.open();
    f.session.state.inputId = "parent_input";
    f.b.modelScript = [mcpEvents("child_mcp"), textEvents("child fetched")];
    const report = JSON.parse(
      await f.session.subagents.send(
        { name: "reader", system_prompt: "Read docs", message: "Fetch docs" },
        new AbortController().signal,
      ),
    );
    expect(report.report).toBe("child fetched");
    expect(
      f.p.requests.filter((request) => request.method === "initialize"),
    ).toHaveLength(1);
    expect(
      f.p.requests.filter((request) => request.method === "tools/list"),
    ).toHaveLength(2);
    const events = [...f.b.publicLog.values()];
    expect(
      events.some(
        (event) =>
          event.session_thread_id === report.thread_id &&
          event.request?.tool_name === "mcp__peer__microsoft_docs_fetch",
      ),
    ).toBe(true);
    expect(
      events.some(
        (event) =>
          event.session_thread_id === report.thread_id &&
          event.message?.content?.some(
            (block: Payload) => block.tool_use_id === "child_mcp",
          ),
      ),
    ).toBe(true);
  } finally {
    await f.close();
  }
});

test("legacy SSE discovery and calls retain headers and root disposal closes the stream", async () => {
  const f = await fixture("sse");
  try {
    await f.session.open();
    f.b.modelScript = [mcpEvents(), textEvents("fetched through SSE")];
    await f.session.receive("sse_input", { content: "Fetch documentation" });
    expect(
      f.p.requests.filter((request) => request.method === "tools/call"),
    ).toHaveLength(1);
    expect(f.b.requests).toHaveLength(2);
    await f.session.close();
    await f.p.disconnected;
  } finally {
    await f.close();
  }
});

test("resuming a session rediscovers external tools without replaying completed calls", async () => {
  const f = await fixture();
  let resumed: ReturnType<typeof createOmaSession> | undefined;
  try {
    await f.session.open();
    f.b.modelScript = [mcpEvents(), textEvents("first done")];
    await f.session.receive("first", { content: "Fetch documentation" });
    await f.session.close();
    f.ctx.ccrv2.forget(f.a.session_id);
    resumed = createOmaSession(f.ctx, f.api, f.a);
    f.b.onApproval = async (payload) => {
      await resumed!.control({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: payload.request_id,
          response: { behavior: "allow" },
        },
      });
    };
    await resumed.open();
    f.b.modelScript = [mcpEvents("second_mcp"), textEvents("second done")];
    await resumed.receive("second", { content: "Fetch again" });
    expect(
      f.p.requests.filter((request) => request.method === "initialize"),
    ).toHaveLength(2);
    expect(
      f.p.requests.filter((request) => request.method === "tools/call"),
    ).toHaveLength(2);
  } finally {
    await resumed?.close();
    await f.close();
  }
});

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

test("Unicode server aliases preserve remote tool dispatch", async () => {
  const prefix = mcpServerPrefix("Peer-测试");
  expect(prefix).toBe("mcp_68d87f931a770c47d39b414c");
  const f = await fixture("http", "Peer-测试");
  try {
    await f.session.open();
    const events = mcpEvents();
    events[1]!.content_block.name = `mcp__${prefix}__microsoft_docs_fetch`;
    f.b.modelScript = [events, textEvents("Document fetched")];
    await f.session.receive("unicode_input", {
      content: "Fetch documentation",
    });
    expect(f.b.requests[0]!.tools.map((tool: Payload) => tool.name)).toContain(
      `mcp__${prefix}__microsoft_docs_fetch`,
    );
    const calls = f.p.requests.filter(
      (request) => request.method === "tools/call",
    );
    expect(calls).toHaveLength(1);
    expect(calls[0]!.params.name).toBe("microsoft_docs_fetch");
  } finally {
    await f.close();
  }
});


test("external InvalidParams response records known failure and permits next real call", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    f.p.protocolError(-32602);
    f.b.modelScript = [mcpEvents(), textEvents("The tool no longer exists")];
    await f.session.receive("missing_tool", { content: "Fetch documentation" });
    expect(f.session.blocked()).toBe(false);
    expect(f.session.state.toolOutcomes?.mcp_call?.failed).toBe(true);
    expect(f.session.state.toolOutcomes?.mcp_call?.text).toContain('unknown tool "microsoft_docs_fetch"');
    expect(f.session.state.toolOutcomes?.mcp_call?.remoteOutcome).toBeUndefined();
    f.p.protocolError();
    f.b.modelScript = [mcpEvents("next_call"), textEvents("Fetched successfully")];
    await f.session.receive("next_input", { content: "Fetch documentation again" });
    expect(f.session.blocked()).toBe(false);
    expect(f.session.state.toolOutcomes?.next_call?.failed).toBe(false);
    expect(f.p.requests.filter((r) => r.method === "tools/call")).toHaveLength(2);
  } finally { await f.close(); }
});

test("external server InternalError keeps unknown outcome without replay", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    f.p.protocolError(-32603);
    f.b.modelScript = [mcpEvents()];
    await expect(f.session.receive("server_error", { content: "Fetch documentation" })).rejects.toThrow();
    expect(f.session.blocked()).toBe(true);
    expect(f.p.requests.filter((r) => r.method === "tools/call")).toHaveLength(1);
  } finally { await f.close(); }
});

for (const status of [401, 403]) {
  test(`external MCP ${status} discovery emits a public authentication error and leaves chat usable`, async () => {
    const f = await fixture();
    try {
      f.p.failAuthentication(status);
      await f.session.open();
      const errors = [...f.b.publicLog.values()].filter(event => event.type === "session.error");
      expect(errors).toHaveLength(1);
      expect(errors[0]!.error).toEqual({
        type: "mcp_authentication_failed_error", mcp_server_name: "peer",
        message: "MCP server authentication failed during tool discovery.",
        retry_status: { type: "exhausted" },
      });
      expect(JSON.stringify(errors)).not.toContain("peer-secret");
      expect(JSON.stringify(errors)).not.toContain(String(f.p.config.mcpServers.peer!.url));
      expect(f.b.requests).toHaveLength(0);
      f.b.modelScript = [textEvents("still interactive")];
      await f.session.receive("after_auth_failure", { content: "Say still interactive" });
      expect(f.b.requests).toHaveLength(1);
      expect(f.b.requests[0]!.tools.some((tool: Payload) => tool.name.startsWith("mcp__"))).toBe(false);
      expect(f.p.requests.filter(request => request.method === "tools/call")).toHaveLength(0);
      expect(f.session.blocked()).toBe(false);
      expect(f.session.busy).toBe(false);
    } finally { await f.close(); }
  });
}

test("authentication failure disables only its server and preserves healthy real MCP dispatch", async () => {
  const healthy = peer("http", "healthy");
  const f = await fixture("http", "peer", healthy.config.mcpServers);
  try {
    f.p.failAuthentication(401);
    await f.session.open();
    const call = mcpEvents();
    call[1]!.content_block.name = "mcp__healthy__microsoft_docs_fetch";
    f.b.modelScript = [call, textEvents("healthy server succeeded")];
    await f.session.receive("healthy_after_auth_failure", { content: "Fetch with healthy" });
    const tools = f.b.requests[0]!.tools.map((tool: Payload) => tool.name);
    expect(tools).toContain("mcp__healthy__microsoft_docs_fetch");
    expect(tools.some((name: string) => name.startsWith("mcp__peer__"))).toBe(false);
    expect(healthy.requests.filter(request => request.method === "tools/call")).toHaveLength(1);
    expect(f.p.requests.filter(request => request.method === "tools/call")).toHaveLength(0);
    expect([...f.b.publicLog.values()].filter(event => event.type === "session.error")).toHaveLength(1);
  } finally { await f.close(); healthy.close(); }
});
