import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { WebClient } from "../src/web-tools/client.ts";
import { webToolDefinitions } from "../src/web-tools/definitions.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, toolEvents, textEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function callEvents(name: string, args: Payload, id: string): Payload[] {
  const events = toolEvents("unused", id);
  events[1]!.content_block.name = name;
  events[2]!.delta.partial_json = JSON.stringify(args);
  return events;
}

test("server failures and cancellation are ordinary tool failures without unknown side effects", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools.push("web_search", "web_fetch");
  const api = new OmaApi(backend.url, Object.assign(async () => Response.json({ error: { message: "Web service balance is insufficient" } }, { status: 503 }), { preconnect: fetch.preconnect }));
  api.assignments.set(backend.a.session_id, backend.a);
  const client = new WebClient(api, backend.a.session_id);
  try {
    const result = await client.call(webToolDefinitions()[0]!, { query: "example" }, new AbortController().signal);
    expect(result.isError).toBe(true);
    expect(result.content[0].text).toContain("balance is insufficient");
    const abort = new AbortController();
    abort.abort();
    const cancelled = new WebClient(new OmaApi(backend.url), backend.a.session_id);
    expect((await cancelled.call(webToolDefinitions()[0]!, { query: "test" }, abort.signal)).isError).toBe(true);
  } finally { await backend.stop(); }
});

test("real dsh loop approves search and fetch through OMA, journals results, and finishes without a sandbox", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools.push("web_search", "web_fetch");
  const calls: Request[] = [];
  const result = { title: "Example", url: "https://example.com/article", snippet: "source snippet" };
  const page = { title: "Example", url: result.url, status_code: 200, content: "source page body", truncated: false };
  backend.modelScript = [callEvents("web_search", { query: "example" }, "search_1"),
    callEvents("web_fetch", { url: result.url }, "fetch_1"), textEvents("Answer [source](https://example.com/article)")];
  const fetcher = Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.url.includes("/web/")) {
      expect(request.url).toStartWith(backend.url);
      expect(request.headers.get("authorization")).toBe(`Bearer ${backend.a.ingress_token}`);
      expect(request.headers.get("authorization")).not.toContain("deepseek");
      calls.push(request);
      return Response.json(request.url.endsWith("/search") ? { sources: [result] } : page);
    }
    return fetch(input, init);
  }, { preconnect: fetch.preconnect });
  const ctx = new Context();
  const api = new OmaApi(backend.url, fetcher);
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    backend.onApproval = async (payload) => {
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
      } });
    };
    await session.receive("web-turn", { content: "Research example" });
    const events = [...backend.publicLog.values()];
    const toolResults = events.filter((event) => event.type === "user").flatMap((event) => event.message.content).filter((block: Payload) => block.type === "tool_result");
    expect(toolResults).toHaveLength(2);
    expect(toolResults.every((event) => !event.is_error)).toBe(true);
    expect(JSON.stringify(backend.requests[1])).toContain(result.url);
    expect(JSON.stringify(backend.requests[2])).toContain(page.content);
    expect(backend.sandboxCount).toBe(0);
    expect(session.state.phase).toBe("completed");
    expect(calls).toHaveLength(2);
    expect(await calls[0]!.json()).toEqual({ query: "example" });
    expect(await calls[1]!.json()).toEqual({ url: result.url });
    session.stop();
    await session.close();
  } finally { await ctx.fiber.dispose(); await backend.stop(); }
});

test("denied web tools make no OMA web requests", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools.push("web_search", "web_fetch");
  backend.modelScript = [callEvents("web_search", { query: "example" }, "denied"), textEvents("Search denied")];
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    backend.onApproval = async (payload) => {
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: payload.request_id, response: { behavior: "deny" },
      } });
    };
    await session.receive("web-denied", { content: "Research example" });
    expect(backend.sandboxCount).toBe(0);
    expect([...backend.publicLog.values()].filter((event) => event.type === "user").flatMap((event) => event.message.content).some((block: Payload) => block.type === "tool_result" && block.is_error)).toBe(true);
    expect(session.state.phase).toBe("completed");
    session.stop();
    await session.close();
  } finally { await ctx.fiber.dispose(); await backend.stop(); }
});

test("web fetch retains full long content in readable storage and forwards the thread scope", async () => {
  const backend = new Backend();
  await backend.ready;
  const files = new Map<string, string>();
  const source = "头部\n" + "content ".repeat(20000) + "\n尾部";
  const api = new OmaApi(backend.url, Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    expect(request.headers.get("X-OMA-Session-Thread-ID")).toBe("child-thread");
    return Response.json({ url: "https://example.com", status_code: 200, title: "Source", content: "preview", full_content: source, truncated: true });
  }, { preconnect: fetch.preconnect }));
  api.assignments.set(backend.a.session_id, backend.a);
  const sandbox = {
    async provision() {},
    async call(tool: Payload, args: Payload) {
      expect(tool.remoteName).toBe("write");
      files.set(args.file_path, args.content);
      return { content: [{ type: "text", text: "saved" }] };
    },
  };
  try {
    const client = new WebClient(api, backend.a.session_id, sandbox, "child-thread");
    const output = await client.call(webToolDefinitions()[1]!, { url: "https://example.com" }, new AbortController().signal);
    expect(output.isError).not.toBe(true);
    expect(files.size).toBe(1);
    expect(JSON.parse([...files.values()][0]!).content).toBe(source);
    expect(output.content[0].text).toContain([...files.keys()][0]!);
    expect(JSON.parse(output.content[0].text).content).toBe("preview");
    expect(JSON.parse(output.content[0].text).full_content).toBeUndefined();
  } finally { await backend.stop(); }
});

test("always-allow web tools skip approval and refresh registrations with the live tool list", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = [];
  backend.a.launch.tool_permissions = { web_search: "allow", web_fetch: "allow" };
  let approvals = 0;
  let calls = 0;
  const api = new OmaApi(backend.url, Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.url.includes("/web/")) {
      calls++;
      return Response.json({ sources: [] });
    }
    return fetch(input, init);
  }, { preconnect: fetch.preconnect }));
  const ctx = new Context();
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    backend.onApproval = async (payload) => {
      approvals++;
      await session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "deny" } } });
    };
    backend.a.launch.tools = ["web_search", "web_fetch"];
    backend.modelScript = [callEvents("web_search", { query: "test" }, "allow-search"), callEvents("web_fetch", { url: "https://example.com" }, "allow-fetch"), textEvents("done")];
    await session.receive("web-enabled", { content: "Search", oma_refresh_tools: true });
    expect(calls).toBe(2);
    expect(approvals).toBe(0);
    expect(backend.requests[0].tools.map((tool: Payload) => tool.name)).toContain("web_search");
    backend.a.launch.tool_permissions.web_search = "ask";
    backend.modelScript = [callEvents("web_search", { query: "ask" }, "ask-search"), textEvents("denied")];
    await session.receive("web-ask", { content: "Ask permission" });
    expect(approvals).toBe(1);
    expect(calls).toBe(2);
    backend.a.launch.tools = [];
    backend.modelScript = [textEvents("removed")];
    await session.receive("web-disabled", { content: "Inspect tools", oma_refresh_tools: true });
    expect(backend.requests.at(-1)!.tools.map((tool: Payload) => tool.name)).not.toContain("web_search");
    expect(backend.requests.at(-1)!.tools.map((tool: Payload) => tool.name)).not.toContain("web_fetch");
    session.stop();
    await session.close();
  } finally { await ctx.fiber.dispose(); await backend.stop(); }
});

test("long fetch preserves the server preview when output storage is unavailable or denied", async () => {
  const backend = new Backend();
  await backend.ready;
  const api = new OmaApi(backend.url, Object.assign(async () => Response.json({ url: "https://example.com", content: "服务端预览", full_content: "full body", truncated: true }), { preconnect: fetch.preconnect }));
  api.assignments.set(backend.a.session_id, backend.a);
  try {
    const denied = { call: async () => ({ isError: true, content: [{ type: "text", text: "Write denied" }] }) };
    for (const sandbox of [undefined, denied]) {
      const result = await new WebClient(api, backend.a.session_id, sandbox).call(webToolDefinitions()[1]!, { url: "https://example.com" }, new AbortController().signal);
      expect(result.isError).not.toBe(true);
      const page = JSON.parse(result.content[0].text);
      expect(page.content).toBe("服务端预览");
      expect(page.truncated).toBe(true);
      expect(page.storage_error).toContain("only the preview");
      expect(page.output_file).toBeUndefined();
      expect(page.full_content).toBeUndefined();
    }
  } finally { await backend.stop(); }
});
