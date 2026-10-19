import { expect, test } from "bun:test";
import { OmaApi } from "../src/api.ts";
import { externalMcpFetch } from "../src/external-mcp/proxy.ts";
import { assignment } from "./backend.ts";

test("external MCP proxy rejects a missing assignment before sending credentials", async () => {
  let sent = false;
  const api = new OmaApi("https://oma.test", (async (_input: string | URL | Request, _init?: RequestInit) => {
    sent = true;
    return new Response();
  }) as typeof fetch);
  expect(() => externalMcpFetch(api, "missing")("https://peer.test/mcp")).toThrow();
  expect(sent).toBe(false);
});

test("external MCP HTTP and SSE requests use current OMA credentials and retain upstream URLs", async () => {
  const calls: Request[] = [];
  const api = new OmaApi("https://oma.test", (async (input, init) => {
    const request = new Request(input, init);
    expect(init?.redirect).toBe("error");
    calls.push(request);
    return new Response(null, { status: 202 });
  }) as typeof fetch);
  const a = assignment();
  api.assignments.set(a.session_id, a);
  const send = externalMcpFetch(api, a.session_id);
  const original = new Request("https://peer.test/mcp?x=1", {
    method: "POST", headers: { Authorization: "Bearer upstream", "Mcp-Session-Id": "peer-session" },
    body: JSON.stringify({ method: "tools/list" }),
  });
  await send(original);
  expect(original.headers.get("Authorization")).toBe("Bearer upstream");
  expect(new URL(calls[0]!.url).searchParams.get("mcp_url")).toBe(original.url);
  expect(calls[0]!.headers.get("Authorization")).toBe(`Bearer ${a.ingress_token}`);
  expect(calls[0]!.headers.get("Mcp-Session-Id")).toBe("peer-session");
  expect(await calls[0]!.json()).toEqual({ method: "tools/list" });
  a.ingress_token = "rotated-ingress";
  await send("https://peer.test/messages?sessionId=owned", { method: "POST", body: "{}" });
  expect(new URL(calls[1]!.url).searchParams.get("mcp_url")).toBe("https://peer.test/messages?sessionId=owned");
  expect(calls[1]!.headers.get("Authorization")).toBe("Bearer rotated-ingress");
  const tunnel = `https://oma.test/v2/ccr-sessions/${a.session_id}/mcp/local`;
  await send(tunnel);
  expect(calls[2]!.url).toBe(tunnel);
  expect(calls[2]!.headers.get("Authorization")).toBe("Bearer rotated-ingress");
  await send(`https://other.test/v2/ccr-sessions/${a.session_id}/mcp/local`);
  expect(new URL(calls[3]!.url).origin).toBe("https://oma.test");
  expect(new URL(calls[3]!.url).searchParams.get("mcp_url")).toContain("https://other.test/");
});


test("MCP thread scope is host-owned, retained for named gateways and isolated from root", async () => {
  const calls: Request[] = [];
  const api = new OmaApi("https://oma.test", (async (input, init) => {
    calls.push(new Request(input, init));
    return new Response(null, { status: 202 });
  }) as typeof fetch);
  const a = assignment();
  api.assignments.set(a.session_id, a);
  const send = externalMcpFetch(api, a.session_id, "sthr_child");
  for (const endpoint of ["https://peer.test/mcp", `https://oma.test/v2/ccr-sessions/${a.session_id}/mcp/local`]) {
    await send(endpoint, { headers: { "X-OMA-Session-Thread-ID": "sthr_forged" } });
    expect(calls.at(-1)!.headers.get("X-OMA-Session-Thread-ID")).toBe("sthr_child");
  }
  await externalMcpFetch(api, a.session_id)("https://peer.test/mcp", { headers: { "X-OMA-Session-Thread-ID": "sthr_forged" } });
  expect(calls.at(-1)!.headers.has("X-OMA-Session-Thread-ID")).toBe(false);
});
