import { isDeepStrictEqual } from "node:util";
import type { Payload } from "../json.ts";

export function normalizeSandboxResult(result: Payload, toolName: string): Payload {
  if (typeof result.structuredContent?.rawOutput === "string" || !Array.isArray(result.content)
    || result.content.some((block: Payload) => !block || block.type !== "text" || typeof block.text !== "string")) return result;
  const stdout = successfulBashOutput(result, toolName);
  return stdout === undefined ? result : { ...result, content: [{ type: "text", text: stdout }] };
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
