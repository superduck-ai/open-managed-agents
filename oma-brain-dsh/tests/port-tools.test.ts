import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { validateAssignment } from "../src/assignment.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, assignment, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";
import { PortUrlClient } from "../src/port-tools/client.ts";
import { portToolDefinition } from "../src/port-tools/definitions.ts";

const legacyTools = ["Bash", "Read", "Write", "Edit", "Grep", "Glob", "web_search", "web_fetch", "ask_user_question"];
const previewURL = "https://4000-abcdefghijklmnopqrstuvwxyz.preview.example.com/";

function callEvents(name: string, args: Payload, id: string): Payload[] {
  const events = toolEvents("unused", id);
  events[1]!.content_block.name = name;
  events[2]!.delta.partial_json = JSON.stringify(args);
  return events;
}

function toolResults(backend: Backend): Payload[] {
  return [...backend.publicLog.values()].filter((event) => event.type === "user")
    .flatMap((event) => event.message.content).filter((block: Payload) => block.type === "tool_result");
}

function resultText(block: Payload): string {
  return typeof block.content === "string" ? block.content : block.content.map((part: Payload) => part.text).join("");
}

async function runSession(backend: Backend, fetcher: typeof fetch, turns: { id: string; content: Payload }[], approvals?: () => void) {
  const ctx = new Context();
  const api = new OmaApi(backend.url, fetcher);
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    backend.onApproval = async (payload) => {
      approvals?.();
      await session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "deny" } } });
    };
    for (const turn of turns) await session.receive(turn.id, turn.content);
    session.stop();
    await session.close();
  } finally { await ctx.fiber.dispose(); }
}

function portFetcher(calls: Request[], reply: () => Response): typeof fetch {
  return Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.url.includes("/port-url")) {
      calls.push(request);
      return reply();
    }
    return fetch(input, init);
  }, { preconnect: fetch.preconnect });
}

test("assignment rejects unknown tools and accepts get_port_url", () => {
  const unknown = assignment();
  unknown.launch.tools = ["get_port_link"];
  expect(() => validateAssignment(unknown)).toThrow("Invalid launch tools");
  const enabled = assignment();
  enabled.launch.tools = [...legacyTools, "get_port_url"];
  expect(() => validateAssignment(enabled)).not.toThrow();
});

test("model tools and system prompt are unchanged when get_port_url is not launched", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = legacyTools;
  backend.modelScript = [textEvents("ok")];
  try {
    await runSession(backend, fetch, [{ id: "golden", content: { content: "hi" } }]);
    const golden = await Bun.file(new URL("./model-tools.golden.json", import.meta.url)).json();
    expect(JSON.stringify(backend.requests[0].tools)).toBe(JSON.stringify(golden.tools));
    expect(JSON.stringify(backend.requests[0].system)).toBe(JSON.stringify(golden.system));
  } finally { await backend.stop(); }
});

test("server errors become tool errors and invalid ports never reach OMA", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = ["get_port_url"];
  backend.a.launch.tool_permissions = { get_port_url: "allow" };
  backend.modelScript = [
    callEvents("get_port_url", { port: 49983 }, "blocked"),
    callEvents("get_port_url", { port: 0 }, "zero"),
    callEvents("get_port_url", { port: "4000" }, "string"),
    textEvents("done"),
  ];
  const calls: Request[] = [];
  const fetcher = portFetcher(calls, () => Response.json({ type: "error", error: { type: "invalid_request_error", message: "port 49983 is reserved by the sandbox and has no preview URL" } }, { status: 400 }));
  try {
    await runSession(backend, fetcher, [{ id: "errors", content: { content: "links" } }]);
    const results = toolResults(backend);
    expect(results).toHaveLength(3);
    expect(results.every((block) => block.is_error)).toBe(true);
    expect(resultText(results[0]!)).toBe("Error: OMA port URL HTTP 400: port 49983 is reserved by the sandbox and has no preview URL");
    expect(results.some((block) => resultText(block).includes("preview.example.com"))).toBe(false);
    expect(resultText(results[1]!)).toBe("Error: port must be an integer between 1 and 65535");
    expect(resultText(results[2]!)).toContain("must be an integer");
    expect(calls).toHaveLength(1);
  } finally { await backend.stop(); }
});

test("disabled feature and unavailable OMA are reported without an address", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = ["get_port_url"];
  backend.a.launch.tool_permissions = { get_port_url: "allow" };
  backend.modelScript = [callEvents("get_port_url", { port: 4000 }, "disabled"), callEvents("get_port_url", { port: 4000 }, "down"), textEvents("done")];
  const calls: Request[] = [];
  const fetcher = portFetcher(calls, () => calls.length === 1
    ? Response.json({ type: "error", error: { type: "invalid_request_error", message: "Session port preview URLs are not enabled on this server" } }, { status: 409 })
    : new Response("bad gateway", { status: 502 }));
  try {
    await runSession(backend, fetcher, [{ id: "disabled", content: { content: "link" } }]);
    const results = toolResults(backend);
    expect(results.map(resultText)).toEqual([
      "Error: OMA port URL HTTP 409: Session port preview URLs are not enabled on this server",
      "Error: OMA port URL service unavailable or timed out",
    ]);
    expect(results.every((block) => block.is_error)).toBe(true);
  } finally { await backend.stop(); }
});

test("enabled get_port_url returns the server URL as text without approval", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = [...legacyTools, "get_port_url"];
  backend.a.launch.tool_permissions = { get_port_url: "allow" };
  backend.modelScript = [callEvents("get_port_url", { port: 4000 }, "port_1"), textEvents(`Preview: ${previewURL}`)];
  const calls: Request[] = [];
  let approvals = 0;
  const fetcher = portFetcher(calls, () => Response.json({ type: "session_port", port: 4000, url: previewURL }));
  try {
    await runSession(backend, fetcher, [{ id: "port", content: { content: "Give me the preview link for port 4000" } }], () => approvals++);
    const tool = backend.requests[0].tools.find((entry: Payload) => entry.name === "get_port_url");
    expect(tool.input_schema).toEqual({ type: "object", additionalProperties: false, required: ["port"], properties: { port: { type: "integer", minimum: 1, maximum: 65535 } } });
    expect(tool.description).toContain("whether or not a service is already listening");
    expect(tool.description).toContain("A service listening only on 127.0.0.1 (localhost) is reachable through the URL as is");
    expect(tool.description).toContain("do not ask the user to restart it with --host or --bind 0.0.0.0");
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe(`${backend.url}/v2/ccr-sessions/${encodeURIComponent(backend.a.session_id)}/port-url`);
    expect(calls[0]!.headers.get("authorization")).toBe(`Bearer ${backend.a.ingress_token}`);
    expect(await calls[0]!.json()).toEqual({ port: 4000 });
    const results = toolResults(backend);
    expect(results).toHaveLength(1);
    expect(results[0]!.is_error).toBeFalsy();
    expect(resultText(results[0]!)).toBe(previewURL);
    expect(approvals).toBe(0);
    expect(JSON.stringify(backend.requests[1])).toContain(previewURL);
  } finally { await backend.stop(); }
});

test("tool refresh adds and removes get_port_url", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.tools = [];
  backend.modelScript = [textEvents("before")];
  const names = (index: number) => backend.requests.at(index)!.tools.map((tool: Payload) => tool.name);
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  try {
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    await session.open();
    await session.receive("before", { content: "tools" });
    expect(names(-1)).not.toContain("get_port_url");
    backend.a.launch.tools = ["get_port_url"];
    backend.modelScript = [textEvents("enabled")];
    await session.receive("enabled", { content: "tools", oma_refresh_tools: true });
    expect(names(-1)).toContain("get_port_url");
    backend.a.launch.tools = [];
    backend.modelScript = [textEvents("removed")];
    await session.receive("removed", { content: "tools", oma_refresh_tools: true });
    expect(names(-1)).not.toContain("get_port_url");
    session.stop();
    await session.close();
  } finally { await ctx.fiber.dispose(); await backend.stop(); }
});

test("child thread calls forward the thread scope", async () => {
  const a = assignment();
  const calls: Request[] = [];
  const api = new OmaApi("http://oma.test", portFetcher(calls, () => Response.json({ type: "session_port", port: 5173, url: previewURL })));
  api.assignments.set(a.session_id, a);
  const result = await new PortUrlClient(api, a.session_id, "child-thread").call(portToolDefinition(), { port: 5173 }, new AbortController().signal);
  expect(result).toEqual({ content: [{ type: "text", text: previewURL }] });
  expect(calls[0]!.headers.get("X-OMA-Session-Thread-ID")).toBe("child-thread");
});
