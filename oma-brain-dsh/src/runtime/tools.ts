import { isDeepStrictEqual } from "node:util";
import type { Payload } from "../json.ts";

export function toolText(result: Payload, toolName?: string): string {
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
  const stdout = successfulBashOutput(result, toolName);
  if (stdout !== undefined) return stdout;
  return result.content.map((c: Payload) => c.text).join("\n");
}

function successfulBashOutput(result: Payload, toolName?: string): string | undefined {
  const data = result.structuredContent;
  if (toolName !== "Bash" || result.isError || !data || data.status !== "completed"
    || data.exit_code !== 0 || data.stderr !== "" || typeof data.stdout !== "string"
    || data.timed_out !== false || data.cancelled !== false
    || data.stdout_truncated !== false || data.stderr_truncated !== false
    || data.stdout_error || data.stderr_error || data.error)
    return undefined;
  const content = result.content;
  if (content.length !== 1 && content.length !== 2) return undefined;
  if (content[0].text !== data.stdout && !(data.stdout === "" && content[0].text === "(no output)"))
    return undefined;
  if (content.length === 2) {
    try {
      if (!isDeepStrictEqual(JSON.parse(content[1].text), data)) return undefined;
    } catch {
      return undefined;
    }
  }
  return data.stdout;
}
