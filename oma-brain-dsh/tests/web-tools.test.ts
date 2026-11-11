

import { expect,test } from "bun:test";


import { OmaApi } from "../src/api.ts";

import { WebClient } from "../src/web-tools/client.ts";

import { webToolDefinitions } from "../src/web-tools/definitions.ts";

import { assignment,Backend,toolEvents } from "./backend.ts";

import type { Payload } from "../src/json.ts";

import { SandboxMcp } from "../src/oma-sandbox/client.ts";

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

test("web fetch retains full long content in readable storage and forwards the thread scope", async () => {
  const backend = new Backend();
  await backend.ready;
  const files = new Map<string, string>();
  const source = "头部\n" + "content ".repeat(20000) + "\n尾部";
  let truncated = true;
  const api = new OmaApi(backend.url, Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    expect(request.headers.get("X-OMA-Session-Thread-ID")).toBe("child-thread");
    return Response.json({ url: "https://example.com", status_code: 200, title: "Source", content: truncated ? "preview" : source, full_content: truncated ? source : undefined, truncated });
  }, { preconnect: fetch.preconnect }));
  api.assignments.set(backend.a.session_id, backend.a);
  const sandbox = {
    async provision() {},
    async call() { throw new Error("model Write must not store internal output"); },
    async saveOutput(content: string) {
      const path = `/tmp/tool-outputs/mcp_${crypto.randomUUID()}.txt`;
      files.set(path, content);
      return path;
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
    truncated = false;
    const complete = await client.call(webToolDefinitions()[1]!, { url: "https://example.com" }, new AbortController().signal);
    expect(JSON.parse(complete.content[0].text).content).toBe(source);
    expect(JSON.parse(complete.content[0].text).truncated).toBe(false);
    expect(files.size).toBe(1);
  } finally { await backend.stop(); }
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

test("oversized search returns a bounded preview when Write is denied or storage unavailable", async () => {
 const a = assignment();
 const api = new OmaApi("http://oma.test", Object.assign(async () => Response.json({ sources: [{ url: "https://example.com", snippet: "🦆".repeat(100001) }] }), { preconnect: fetch.preconnect }));
 api.assignments.set(a.session_id, a);
 for (const sandbox of [undefined, { call: async () => ({ isError: true, content: [{ type: "text", text: "denied" }] }) }]) {
  const result = await new WebClient(api, a.session_id, sandbox).call(webToolDefinitions()[0]!, { query: "test" }, new AbortController().signal);
  expect(result.isError).not.toBe(true);
  expect(result.content[0].text).toContain("only the preview");
  expect(result.content[0].text).not.toContain("/tmp/tool-outputs/");
  expect(Array.from(result.content[0].text).length).toBeLessThan(2500);
 }
});

test("parallel root and child Fetch keeps request scope and stores outputs independently of Write", async () => {
 const a = assignment();
 a.mcp_config = {mcpServers:{sandbox:{type:"http",url:`http://oma.test/api/internal/code-sessions/${a.session_id}/sandbox/mcp`,headers:{Authorization:"Bearer test-mcp"}}}};
 const scopes: (string|null)[] = [], contents: string[] = [];
 const api = new OmaApi("http://oma.test", Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
  const request = new Request(input, init);
  if (request.url.includes("/web/fetch")) {
   scopes.push(request.headers.get("X-OMA-Session-Thread-ID"));
   return Response.json({url:"https://example.com",content:"preview",full_content:"full",truncated:true});
  }
  expect(request.url.endsWith("/sandbox/tool-outputs")).toBe(true);
  const body=await request.json();
  expect(Object.keys(body)).toEqual(["content"]);
  contents.push(body.content);
  return Response.json({path:`/tmp/tool-outputs/mcp_${crypto.randomUUID()}.txt`});
 }, {preconnect:fetch.preconnect}));
 api.assignments.set(a.session_id,a);
 const sandbox = new SandboxMcp(api,a.session_id);
 try {
  const outputs=await Promise.all(["child",undefined].map(thread=>new WebClient(api,a.session_id,sandbox,thread).call(webToolDefinitions()[1]!,{url:"https://example.com"},new AbortController().signal)));
  for (const output of outputs) expect(JSON.parse(output!.content[0].text).output_file).toStartWith("/tmp/tool-outputs/");
  expect(scopes).toEqual(["child",null]);
  expect(contents).toHaveLength(2);
 } finally {await sandbox.close();}
});
