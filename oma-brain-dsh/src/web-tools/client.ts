import { persistToolOutput } from "../external-mcp/output.ts";
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
          if (!this.sandbox) throw new Error("Web output storage unavailable");
          const { full_content, max_content_tokens, ...page } = body;
          const full = { content: [{ type: "text", text: JSON.stringify({ ...page, content: full_content, truncated: false }) }] };
          return await persistToolOutput(full, this.sandbox, signal, Math.max(0, Math.floor(max_content_tokens / 4)));
        }
        const result = { content: [{ type: "text", text: JSON.stringify(body) }] };
        return this.sandbox ? await persistToolOutput(result, this.sandbox, signal) : result;
      }
      const message = typeof body?.error?.message === "string" ? body.error.message : "Web tool request failed";
      return { isError: true, content: [{ type: "text", text: `OMA web tool HTTP ${response.status}: ${message}` }] };
    } catch {
      return { isError: true, content: [{ type: "text", text: signal.aborted ? "Web request cancelled" : "OMA web tool unavailable or timed out" }] };
    }
  }
}
