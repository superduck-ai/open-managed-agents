import { object } from "../json.ts";

export interface ExternalServer {
  name: string;
  prefix: string;
  type: "http" | "sse";
  url: URL;
  headers: Record<string, string>;
  disabledTools: Set<string>;
}

export function externalServers(config: unknown): ExternalServer[] {
  if (config === undefined || config === null) return [];
  const document = object(config);
  const servers =
    document.mcpServers === undefined ? {} : object(document.mcpServers);
  const prefixes = new Set<string>();
  return Object.entries(servers).map(([name, value]) => {
    const server = object(value);
    const prefix = name.replaceAll(".", "_");
    if (!/^[a-zA-Z0-9_-]+$/.test(prefix) || prefixes.has(prefix))
      throw new Error("Invalid or ambiguous external MCP server name");
    prefixes.add(prefix);
    if (!["http", "streamable-http", "sse"].includes(server.type))
      throw new Error(`Unsupported external MCP transport for ${name}`);
    if (typeof server.url !== "string")
      throw new Error(`Missing external MCP URL for ${name}`);
    const url = new URL(server.url);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.hash
    )
      throw new Error(`Invalid external MCP URL for ${name}`);
    const headers = object(server.headers ?? {});
    if (Object.values(headers).some((value) => typeof value !== "string"))
      throw new Error(`Invalid external MCP headers for ${name}`);
    const tools = server.tools ?? [];
    if (
      !Array.isArray(tools) ||
      tools.some(
        (tool) =>
          !tool ||
          typeof tool.name !== "string" ||
          (tool.enabled !== undefined && typeof tool.enabled !== "boolean"),
      )
    )
      throw new Error(`Invalid external MCP tool configuration for ${name}`);
    return {
      name,
      prefix,
      type: server.type === "sse" ? "sse" : "http",
      url,
      headers,
      disabledTools: new Set(
        tools.filter((tool) => tool.enabled === false).map((tool) => tool.name),
      ),
    };
  });
}
