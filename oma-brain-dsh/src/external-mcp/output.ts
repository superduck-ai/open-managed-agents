import type { Payload } from "../json.ts";
import type { SandboxExecutor } from "../runtime/contracts.ts";
import { toolText } from "../runtime/tools.ts";
import { sandboxToolDefinitions } from "../sandbox-tools/definitions.ts";

export async function persistToolOutput(
  result: Payload,
  sandbox: SandboxExecutor | undefined,
  signal: AbortSignal,
  options: { threadId?: string; previewOnStorageFailure?: boolean } = {},
): Promise<Payload> {
  if (Array.isArray(result.content) && result.content.some(
    (block: Payload) => ["image", "audio", "resource", "resource_link"].includes(block.type),
  )) return {
    isError: true,
    content: [{ type: "text", text: "MCP tool returned unsupported non-text content. The remote call completed and was not retried." }],
  };
  const maxCharacters = 100_000;
  const text = toolText(result);
  if (text.length <= maxCharacters) return result;
  const characters = Array.from(text);
  if (characters.length <= maxCharacters) return result;
  signal.throwIfAborted();
  const preview = `Preview (head and tail):\n${characters.slice(0, 1000).join("")}\n\n... [preview middle omitted] ...\n\n${characters.slice(-1000).join("")}`;
  let path: string;
  try {
    if (!sandbox) throw new Error("Output storage unavailable");
    path = await saveToolOutput(text, sandbox, signal, options.threadId);
  } catch {
    signal.throwIfAborted();
    if (options.previewOnStorageFailure) return { content: [{ type: "text", text: `Full output could not be saved; only the preview is available.\n\n${preview}` }] };
    return { isError: true, content: [{ type: "text", text: "Tool output could not be saved to the session sandbox. The completed call was not retried." }] };
  }
  const persisted = `<persisted-output>\nOutput too large (${characters.length} characters). Full output saved to: ${path}\nThe file is temporary and becomes unavailable if the session sandbox is replaced.\n\n${preview}\n</persisted-output>`;
  // rawOutput must not bypass the persisted preview in toolText.
  return { ...result, structuredContent: undefined, content: [{ type: "text", text: persisted }] };
}

export async function saveToolOutput(text: string, sandbox: SandboxExecutor, signal: AbortSignal, threadId?: string): Promise<string> {
  signal.throwIfAborted();
  const path = `/tmp/tool-outputs/mcp_${crypto.randomUUID()}.txt`;
  const write = sandboxToolDefinitions().find(tool => tool.name === "Write")!;
  await sandbox.provision?.(signal);
  const saved = await sandbox.call(write, { file_path: path, content: text }, signal, threadId);
  if (saved.isError) throw new Error("Sandbox output persistence failed");
  signal.throwIfAborted();
  return path;
}
