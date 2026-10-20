import { persistToolOutput, saveToolOutput } from "../external-mcp/output.ts";
import type { OmaApi } from "../api.ts";
import type { Payload } from "../json.ts";
import type { RemoteTool, SandboxExecutor } from "../runtime/contracts.ts";

export class WebClient {
  constructor(readonly api: OmaApi, readonly sessionId: string, readonly sandbox?: SandboxExecutor, readonly threadId?: string) {}

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload> {
    try {
      const assignment = this.api.assignment(this.sessionId);
      const response = await this.api.requestResponse(
        `/v2/ccr-sessions/${encodeURIComponent(this.sessionId)}/web/${tool.remoteName}`,
        assignment.ingress_token, "POST", args, this.threadId ? { "X-OMA-Session-Thread-ID": this.threadId } : undefined, signal, 35_000,
      );
      const body = await response.json();
      if (response.ok) {
        if (typeof body.full_content === "string") {
          const { full_content, ...page } = body;
          try {
            if (!this.sandbox) throw new Error("Web output storage unavailable");
            page.output_file = await saveToolOutput(JSON.stringify({ ...page, content: full_content, truncated: false }), this.sandbox, signal);
          } catch {
            signal.throwIfAborted();
            page.storage_error = "Full content could not be saved; only the preview is available.";
          }
          return { content: [{ type: "text", text: JSON.stringify(page) }] };
        }
        const result = { content: [{ type: "text", text: JSON.stringify(body) }] };
        return this.sandbox && tool.remoteName !== "fetch" ? await persistToolOutput(result, this.sandbox, signal) : result;
      }
      const message = typeof body?.error?.message === "string" ? body.error.message : "Web tool request failed";
      return { isError: true, content: [{ type: "text", text: `OMA web tool HTTP ${response.status}: ${message}` }] };
    } catch {
      return { isError: true, content: [{ type: "text", text: signal.aborted ? "Web request cancelled" : "OMA web tool unavailable or timed out" }] };
    }
  }
}
