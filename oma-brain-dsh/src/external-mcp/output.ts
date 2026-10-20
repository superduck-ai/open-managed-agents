import type { Payload } from "../json.ts";
import type { SandboxExecutor } from "../runtime/contracts.ts";
import { toolText } from "../runtime/tools.ts";
import { sandboxToolDefinitions } from "../sandbox-tools/definitions.ts";

export async function persistToolOutput(
  result: Payload,
  sandbox: SandboxExecutor,
  signal: AbortSignal,
  maxCharacters = 100_000,
): Promise<Payload> {
  if (Array.isArray(result.content) && result.content.some(
    (block: Payload) => ["image", "audio", "resource", "resource_link"].includes(block.type),
  )) return {
    isError: true,
    content: [{ type: "text", text: "MCP tool returned unsupported non-text content. The remote call completed and was not retried." }],
  };
  const text = toolText(result);
  if (text.length <= maxCharacters) return result;
  const characters = Array.from(text);
  if (characters.length <= maxCharacters) return result;
  signal.throwIfAborted();
  const path = `/tmp/tool-outputs/mcp_${crypto.randomUUID()}.txt`;
  const write = sandboxToolDefinitions().find(tool => tool.name === "Write")!;
  try {
    await sandbox.provision?.(signal);
    const saved = await sandbox.call(write, { file_path: path, content: text }, signal);
    if (saved.isError) throw new Error("Sandbox output persistence failed");
    signal.throwIfAborted();
  } catch {
    signal.throwIfAborted();
    // The external call is complete. A local persistence failure must never
    // replay it or claim a readable file exists.
    return { isError: true, content: [{ type: "text", text: "Tool output could not be saved to the session sandbox. The completed call was not retried." }] };
  }
  const preview = `<persisted-output>\nOutput too large (${characters.length} characters). Full output saved to: ${path}\nThe file is temporary and becomes unavailable if the session sandbox is replaced.\n\nPreview (head and tail):\n${characters.slice(0, Math.min(1000, Math.floor(maxCharacters / 2))).join("")}\n\n... [preview middle omitted] ...\n\n${characters.slice(characters.length - Math.min(1000, Math.floor(maxCharacters / 2))).join("")}\n</persisted-output>`;
  // rawOutput must not bypass the persisted preview in toolText.
  return { ...result, structuredContent: undefined, content: [{ type: "text", text: preview }] };
}
