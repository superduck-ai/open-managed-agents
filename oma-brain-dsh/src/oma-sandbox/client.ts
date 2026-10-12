import { Client, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { OmaApi } from "../api.ts";
import { object, type Payload } from "../json.ts";
import { sandboxEndpoint } from "./config.ts";
import type { SandboxExecutor, RemoteTool } from "../runtime/contracts.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

/** Use the same MCP SDK as DSH, with initialization gated by the approval checkpoint. */
export class SandboxMcp implements SandboxExecutor {
  private readonly client = new Client({ name: "oma-brain-dsh", version: "0.1.0" }, { versionNegotiation: { mode: "auto" } });
  private readonly transport: StreamableHTTPClientTransport;
  private initialization?: Promise<void>;
  private readonly calls = new Map<string, { signal: AbortSignal; cleanup?: () => void }>();

  constructor(readonly api: OmaApi, readonly sessionId: string) {
    const endpoint = sandboxEndpoint(api.assignment(sessionId).mcp_config, api.base);
    this.transport = new StreamableHTTPClientTransport(new URL(endpoint.url), {
      requestInit: { headers: { ...endpoint.headers, "X-OMA-MCP-Client-ID": crypto.randomUUID() }, redirect: "error" },
      fetch: async (input, init) => {
        const request = new Request(input, init);
        if (request.method === "POST") {
          const body = await request.clone().json() as Payload;
          const call = body.method === "tools/call" ? this.calls.get(body.params?._meta?.progressToken) : undefined;
          if (call) {
            call.signal.throwIfAborted();
            if (this.transport.protocolVersion !== "2026-07-28") {
              const cancel = () => {
                void this.client.notification({ method: "notifications/cancelled", params: { requestId: body.id, reason: "User interrupted" } }).catch(() => {});
              };
              call.signal.addEventListener("abort", cancel, { once: true });
              call.cleanup = () => call.signal.removeEventListener("abort", cancel);
            }
            delete body.params._meta.progressToken;
            if (!Object.keys(body.params._meta).length) delete body.params._meta;
            return api.fetcher(new Request(request, { body: JSON.stringify(body) }));
          }
        }
        return api.fetcher(request);
      },
    });
  }

  async provision(signal: AbortSignal): Promise<void> {
    signal.throwIfAborted();
    const start = Date.now();
    const initializing = !this.initialization;
    await (this.initialization ??= this.client.connect(this.transport, { signal, timeout: 185_000 }));
    signal.throwIfAborted();
    if (initializing && process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_sandbox_initialized", {
      session_id: this.sessionId, timestamp: new Date().toISOString(), duration_ms: Date.now() - start,
    });
  }

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload> {
    await this.provision(signal);
    const start = Date.now();
    const token = crypto.randomUUID();
    const call = { signal, cleanup: undefined as (() => void) | undefined };
    this.calls.set(token, call);
    let result: Payload;
    try {
      result = object(await this.client.callTool({ name: tool.remoteName, arguments: args, _meta: { progressToken: token } },
        { timeout: 185_000, ...(this.transport.protocolVersion === "2026-07-28" ? { signal } : {}) }));
    } finally {
      call.cleanup?.();
      this.calls.delete(token);
    }
    if (process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_sandbox_tool_finished", {
      session_id: this.sessionId, tool_name: tool.name, timestamp: new Date().toISOString(), duration_ms: Date.now() - start,
    });
    return result;
  }

  async close(): Promise<void> { await this.client.close(); }
}
