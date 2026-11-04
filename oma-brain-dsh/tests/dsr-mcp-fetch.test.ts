import { expect,test } from "bun:test";
import { createDsrMcpFetch } from "../src/dsr/mcp-fetch.ts";
import type { Claim } from "../src/dsr/wire.ts";

const claim: Claim = { code_session_id: "cse_root", worker_epoch: "7", session_token: "session", model_token: "model", claim_expires_at: "2026-11-03T00:00:00Z" };

test("DSR MCP never replays a failed side effect or adopts replacement owner credentials", async () => {
  const mutable = { ...claim };
  const requests: Request[] = [];
  const gateway = createDsrMcpFetch("https://oma.test", mutable, (async (input: RequestInfo | URL) => { requests.push(input as Request); throw new Error("lost tool outcome"); }) as unknown as typeof fetch);
  mutable.session_token = "new-owner";
  await expect(gateway("https://upstream.test/mcp", { method: "POST", body: '{"method":"tools/call"}' })).rejects.toThrow("lost tool outcome");
  expect(requests).toHaveLength(1);
  expect(requests[0]!.headers.get("Authorization")).toBe("Bearer session");
});

test("DSR MCP retains named gateways and OAuth discovery without proxy loops", async () => {
  const requests: Request[] = [];
  const gateway = createDsrMcpFetch("https://oma.test", claim, (async (input: RequestInfo | URL) => { requests.push(input as Request); return Response.json({}); }) as typeof fetch, "child");
  const urls = ["https://oma.test/v1/dsr/sessions/cse_root/mcp/server", "https://oma.test/.well-known/oauth-protected-resource/v1/dsr/sessions/cse_root/mcp/server"];
  for (const url of urls) await gateway(url);
  expect(requests.map(request => request.url)).toEqual(urls);
  expect(requests.every(request => request.headers.get("DSR-Protocol") === "1" && request.headers.get("X-OMA-Session-Thread-ID") === "child")).toBe(true);
  await gateway("https://upstream.test/mcp", { method: "POST", body: "{}" });
  const target = new URL(requests[2]!.url);
  expect(target.pathname).toBe("/v1/dsr/sessions/cse_root/mcp");
  expect(target.searchParams.get("mcp_url")).toBe("https://upstream.test/mcp");
});
