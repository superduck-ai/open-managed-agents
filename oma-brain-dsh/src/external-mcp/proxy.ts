import type { OmaApi } from "../api.ts";

export function externalMcpFetch(api: OmaApi, sessionId: string): typeof fetch {
  return ((input: string | URL | Request, init?: RequestInit) => {
    const request = new Request(input, init);
    const target = new URL(request.url);
    const base = new URL(api.base);
    const path = `/v2/ccr-sessions/${encodeURIComponent(sessionId)}/mcp`;
    const namedGateway = target.origin === base.origin && target.pathname.startsWith(`${path}/`);
    const gateway = namedGateway ? target : new URL(`${api.base.replace(/\/$/, "")}${path}`);
    if (!namedGateway) gateway.searchParams.set("mcp_url", target.href);
    const forwarded = new Request(gateway, request);
    forwarded.headers.set("Authorization", `Bearer ${api.assignment(sessionId).ingress_token}`);
    return api.fetcher(forwarded, { redirect: "error" });
  }) as typeof fetch;
}
