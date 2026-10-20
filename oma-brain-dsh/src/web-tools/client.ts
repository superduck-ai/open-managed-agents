import type { OmaApi } from "../api.ts";
import type { Payload } from "../json.ts";
import type { RemoteTool } from "../runtime/contracts.ts";

export class WebClient {
  constructor(readonly api: OmaApi, readonly sessionId: string) {}

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload> {
    try {
      const assignment = this.api.assignment(this.sessionId);
      const response = await this.api.requestResponse(
        `/v2/ccr-sessions/${encodeURIComponent(this.sessionId)}/web/${tool.remoteName}`,
        assignment.ingress_token, "POST", args, undefined, signal, 35_000,
      );
      const body = await response.json();
      if (response.ok) return { content: [{ type: "text", text: JSON.stringify(body) }] };
      const message = typeof body?.error?.message === "string" ? body.error.message : "Web tool request failed";
      return { isError: true, content: [{ type: "text", text: `OMA web tool HTTP ${response.status}: ${message}` }] };
    } catch {
      return { isError: true, content: [{ type: "text", text: signal.aborted ? "Web request cancelled" : "OMA web tool unavailable or timed out" }] };
    }
  }
}
