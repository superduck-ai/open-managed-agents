import type { Claim } from "./wire.ts";

export function createDsrMcpFetch(baseURL: string, claim: Readonly<Pick<Claim, "code_session_id" | "session_token">>, fetcher: typeof fetch, threadId?: string): typeof fetch {
  const base = new URL(baseURL);
  const token = claim.session_token;
  const path = `/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}/mcp`;
  const metadata = `/.well-known/oauth-protected-resource${path}`;
  return ((input: string | URL | Request, init?: RequestInit) => {
    const request = new Request(input, init);
    const target = new URL(request.url);
    const direct = target.origin === base.origin && (target.pathname === path || target.pathname.startsWith(`${path}/`) || target.pathname.startsWith(`${metadata}/`));
    const gateway = direct ? target : new URL(`${baseURL.replace(/\/$/, "")}${path}`);
    if (!direct) gateway.searchParams.set("mcp_url", target.href);
    const forwarded = new Request(gateway, request);
    forwarded.headers.set("Authorization", `Bearer ${token}`);
    forwarded.headers.set("DSR-Protocol", "1");
    forwarded.headers.delete("X-OMA-Session-Thread-ID");
    if (threadId) forwarded.headers.set("X-OMA-Session-Thread-ID", threadId);
    return fetcher(forwarded, { redirect: "error" });
  }) as typeof fetch;
}
