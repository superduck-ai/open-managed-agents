import { Client, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { OmaApi } from "../api.ts";
import { object, type Payload } from "../json.ts";
import { sandboxEndpoint } from "./config.ts";
import { ToolNotForwardedError, isConnectFailure, notForwardedDetail, type SandboxExecutor, type RemoteTool } from "../runtime/contracts.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

/** Use the same MCP SDK as DSH, with initialization gated by the approval checkpoint. */
export class SandboxMcp implements SandboxExecutor {
  private readonly client = new Client({ name: "oma-brain-dsh", version: "0.1.0" }, { versionNegotiation: { mode: "auto" } });
  private readonly transport: StreamableHTTPClientTransport;
  private initialization?: Promise<void>;
  private readonly calls = new Map<string, { signal: AbortSignal; timeoutMs: number; threadId?: string; cleanup?: () => void; rejection?: string }>();

  constructor(readonly api: OmaApi, readonly sessionId: string) {
    const endpoint = sandboxEndpoint(api.assignment(sessionId).mcp_config, api.base);
    this.transport = new StreamableHTTPClientTransport(new URL(endpoint.url), {
      requestInit: { headers: endpoint.headers, redirect: "error" },
      fetch: async (input, init) => {
        const request = new Request(input, init);
        if (request.method === "POST") {
          const body = await request.clone().json() as Payload;
          const call = body.method === "tools/call" ? this.calls.get(body.params?._meta?.progressToken) : undefined;
          if (call) {
            call.signal.throwIfAborted();
            if (call.threadId) request.headers.set("X-OMA-Session-Thread-ID", call.threadId);
            if (this.transport.protocolVersion !== "2026-07-28") {
              const cancel = () => {
                void this.client.notification({ method: "notifications/cancelled", params: { requestId: body.id, reason: "User interrupted" } }).catch(() => {});
              };
              call.signal.addEventListener("abort", cancel, { once: true });
              call.cleanup = () => call.signal.removeEventListener("abort", cancel);
            }
            delete body.params._meta.progressToken;
            if (!Object.keys(body.params._meta).length) delete body.params._meta;
            let response: Response;
            try {
              response = await api.fetcher(new Request(request, { body: JSON.stringify(body) }), { timeout: call.timeoutMs + SOCKET_IDLE_MARGIN_MS });
            } catch (error) {
              if (isConnectFailure(error)) call.rejection = "Sandbox tool call was not executed: OMA sandbox gateway is unreachable";
              throw error;
            }
            if (response.headers.get("X-Oma-Sandbox-Not-Forwarded") === "true")
              call.rejection = `Sandbox tool call was not executed: ${await notForwardedDetail(response.clone())}`;
            return response;
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

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal, threadId?: string): Promise<Payload> {
    await this.provision(signal);
    const start = Date.now();
    const token = crypto.randomUUID();
    const timeoutMs = sandboxCallTimeoutMs(tool, args);
    const call = { signal, timeoutMs, threadId, cleanup: undefined as (() => void) | undefined, rejection: undefined as string | undefined };
    this.calls.set(token, call);
    let result: Payload;
    try {
      result = object(await this.client.callTool({ name: tool.remoteName, arguments: args, _meta: { progressToken: token } },
        { timeout: timeoutMs, ...(this.transport.protocolVersion === "2026-07-28" ? { signal } : {}) }));
    } catch (error) {
      if (call.rejection !== undefined) throw new ToolNotForwardedError(call.rejection);
      throw error;
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

const SANDBOX_CALL_TIMEOUT_MS = 185_000;
const SOCKET_IDLE_MARGIN_MS = 15_000;
const BASH_DEFAULT_TIMEOUT_SECONDS = 120;
const BASH_MAX_TIMEOUT_SECONDS = 600;
const BASH_RESPONSE_MARGIN_MS = 65_000;

export function sandboxCallTimeoutMs(tool: RemoteTool, args: Payload): number {
  if (tool.remoteName !== "bash") return SANDBOX_CALL_TIMEOUT_MS;
  const requested = Number.isFinite(args.timeout) && args.timeout > 0 ? args.timeout : BASH_DEFAULT_TIMEOUT_SECONDS;
  const seconds = Math.min(requested, BASH_MAX_TIMEOUT_SECONDS);
  return Math.max(SANDBOX_CALL_TIMEOUT_MS, seconds * 1000 + BASH_RESPONSE_MARGIN_MS);
}
