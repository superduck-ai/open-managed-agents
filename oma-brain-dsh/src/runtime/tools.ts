import type { Payload } from "../json.ts";

export function toolText(result: Payload): string {
  if (typeof result.structuredContent?.rawOutput === "string")
    return result.structuredContent.rawOutput;
  if (!Array.isArray(result.content))
    throw new Error("MCP tool result has no content");
  if (
    result.content.some(
      (c: Payload) => c.type !== "text" || typeof c.text !== "string",
    )
  )
    throw new Error("Only text MCP results are supported");
  return result.content.map((c: Payload) => c.text).join("\n");
}
