import type { OmaApi } from "../api.ts";
import type { Payload } from "../json.ts";
import type { RemoteTool } from "../runtime/contracts.ts";

export class PortUrlClient {
  constructor(readonly api: OmaApi, readonly sessionId: string, readonly threadId?: string) {}

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload> {
    if (!Number.isInteger(args.port) || args.port < 1 || args.port > 65535)
      return { isError: true, content: [{ type: "text", text: "port must be an integer between 1 and 65535" }] };
    try {
      const assignment = this.api.assignment(this.sessionId);
      const response = await this.api.requestResponse(
        `/v2/ccr-sessions/${encodeURIComponent(this.sessionId)}/${tool.remoteName}`,
        assignment.ingress_token, "POST", { port: args.port }, this.threadId ? { "X-OMA-Session-Thread-ID": this.threadId } : undefined, signal, 10_000,
      );
      const body = await response.json();
      if (response.ok && typeof body?.url === "string")
        return { content: [{ type: "text", text: body.url }] };
      const message = typeof body?.error?.message === "string" ? body.error.message : "Port URL request failed";
      return { isError: true, content: [{ type: "text", text: `OMA port URL HTTP ${response.status}: ${message}` }] };
    } catch {
      return { isError: true, content: [{ type: "text", text: signal.aborted ? "Port URL request cancelled" : "OMA port URL service unavailable or timed out" }] };
    }
  }
}
