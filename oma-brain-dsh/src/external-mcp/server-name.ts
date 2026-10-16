import { createHash } from "node:crypto";

export function mcpServerPrefix(name: string): string {
  const length = [...name].length;
  if (length < 1 || length > 255)
    throw new Error("Invalid external MCP server name length");
  if (!/[^a-zA-Z0-9_.-]/u.test(name)) return name.replaceAll(".", "_");
  return `mcp_${createHash("sha256").update(name, "utf8").digest("hex").slice(0, 24)}`;
}
