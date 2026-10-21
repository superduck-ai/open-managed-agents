import { object } from "../json.ts";

export interface McpEndpoint { url: string; headers: Record<string, string> }

export function sandboxEndpoint(config: unknown, apiBase?: string): McpEndpoint {
  const servers = object(object(config).mcpServers);
  const server = object(servers.sandbox);
  if (server.type !== "http" && server.type !== "streamable-http")
    throw new Error("Sandbox MCP requires Streamable HTTP");
  if (typeof server.url !== "string") throw new Error("Missing sandbox MCP URL");
  const relative = server.url.startsWith("/") && !server.url.startsWith("//");
  const url = relative ? new URL(server.url, apiBase ?? "http://oma.invalid") : new URL(server.url);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.hash)
    throw new Error("Invalid sandbox MCP URL");
  const headers = object(server.headers ?? {});
  for (const value of Object.values(headers))
    if (typeof value !== "string") throw new Error("Invalid sandbox MCP headers");
  new Headers(headers);
  return { url: url.href, headers: { ...headers } };
}
