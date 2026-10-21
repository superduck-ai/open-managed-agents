import { Client, SdkHttpError, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { OmaApi } from "../api.ts";
import { object, type Payload } from "../json.ts";
import { sandboxEndpoint } from "./config.ts";
import { ToolNotForwardedError, type SandboxExecutor, type RemoteTool } from "../runtime/contracts.ts";
import { SandboxRequestError } from "../runtime/sandbox-errors.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

interface SandboxConnection {
  client: Client;
  transport: StreamableHTTPClientTransport;
  initialization?: Promise<void>;
  initializingSignal?: AbortSignal;
}

interface SandboxCall {
  signal: AbortSignal;
  threadId?: string;
  cleanup?: () => void;
  rejection?: string;
  failure?: SandboxRequestError;
  dispatched?: boolean;
}

export class SandboxMcp implements SandboxExecutor {
  private connection?: SandboxConnection;
  private readonly lifetime = new AbortController();
  private readonly calls = new Map<string, SandboxCall>();

  constructor(readonly api: OmaApi, readonly sessionId: string) {
    sandboxEndpoint(api.assignment(sessionId).mcp_config, api.base);
  }

  private createConnection(): SandboxConnection {
    const endpoint = sandboxEndpoint(this.api.assignment(this.sessionId).mcp_config, this.api.base);
    const client = new Client({ name: "oma-brain-dsh", version: "0.1.0" }, { versionNegotiation: { mode: "auto" } });
    const transport = new StreamableHTTPClientTransport(new URL(endpoint.url), {
      requestInit: { headers: endpoint.headers, redirect: "error" },
      fetch: async (input, init) => {
        const request = new Request(input, init);
        if (request.method === "POST") {
          const body = await request.clone().json() as Payload;
          const call = body.method === "tools/call" ? this.calls.get(body.params?._meta?.progressToken) : undefined;
          if (call) {
            call.signal.throwIfAborted();
            if (call.threadId) request.headers.set("X-OMA-Session-Thread-ID", call.threadId);
            if (transport.protocolVersion !== "2026-07-28") {
              const cancel = () => {
                void client.notification({ method: "notifications/cancelled", params: { requestId: body.id, reason: "User interrupted" } }).catch(() => {});
              };
              call.signal.addEventListener("abort", cancel, { once: true });
              call.cleanup = () => call.signal.removeEventListener("abort", cancel);
            }
            delete body.params._meta.progressToken;
            if (!Object.keys(body.params._meta).length) delete body.params._meta;
            call.dispatched = true;
            const response = await this.fetch(new Request(request, { body: JSON.stringify(body) }));
            if (!response.ok) {
              call.failure = new SandboxRequestError("tool_call", response.status);
              if (response.headers.get("X-Oma-Sandbox-Not-Forwarded") === "true")
                call.rejection = await rejectionMessage(response.clone());
            }
            return response;
          }
        }
        return this.fetch(request, connection.initializingSignal);
      },
    });
    const connection: SandboxConnection = { client, transport };
    client.onclose = () => { if (this.connection === connection) this.connection = undefined; };
    return connection;
  }

  private fetch(request: Request, initialization?: AbortSignal): Promise<Response> {
    const signals = [request.signal, this.lifetime.signal];
    if (initialization) signals.push(initialization);
    return this.api.fetcher(new Request(request, { signal: AbortSignal.any(signals) }));
  }

  private async initialize(connection: SandboxConnection): Promise<void> {
    const start = Date.now();
    connection.initializingSignal = AbortSignal.any([this.lifetime.signal, AbortSignal.timeout(185_000)]);
    try {
      await connection.client.connect(connection.transport, { signal: connection.initializingSignal, timeout: 185_000 });
    } catch (error) {
      await connection.client.close().catch(() => {});
      if (this.connection === connection) this.connection = undefined;
      throw new SandboxRequestError("initialization", error instanceof SdkHttpError ? error.status : undefined);
    } finally {
      connection.initializingSignal = undefined;
    }
    if (process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_sandbox_initialized", {
      session_id: this.sessionId, timestamp: new Date().toISOString(), duration_ms: Date.now() - start,
    });
  }

  async provision(signal: AbortSignal): Promise<void> {
    await this.ready(signal);
  }

  private async ready(signal: AbortSignal): Promise<SandboxConnection> {
    signal.throwIfAborted();
    this.lifetime.signal.throwIfAborted();
    const connection = this.connection ??= this.createConnection();
    const initialization = connection.initialization ??= this.initialize(connection);
    await new Promise<void>((resolve, reject) => {
      const cancel = () => { signal.removeEventListener("abort", cancel); reject(signal.reason); };
      signal.addEventListener("abort", cancel, { once: true });
      initialization.then(resolve, reject).finally(() => signal.removeEventListener("abort", cancel));
      if (signal.aborted) cancel();
    });
    signal.throwIfAborted();
    return connection;
  }

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal, threadId?: string): Promise<Payload> {
    const connection = await this.ready(signal);
    const start = Date.now();
    const token = crypto.randomUUID();
    const call: SandboxCall = { signal, threadId };
    this.calls.set(token, call);
    let result: Payload;
    try {
      result = object(await connection.client.callTool({ name: tool.remoteName, arguments: args, _meta: { progressToken: token } },
        { timeout: 185_000, ...(connection.transport.protocolVersion === "2026-07-28" ? { signal } : {}) }));
    } catch {
      if (call.rejection !== undefined) throw new ToolNotForwardedError(call.rejection);
      if (!call.dispatched && !signal.aborted) throw new ToolNotForwardedError("Sandbox tool call was not sent. The tool was not executed.");
      throw call.failure ?? new SandboxRequestError("tool_call");
    } finally {
      call.cleanup?.();
      this.calls.delete(token);
    }
    if (process.env.OMA_DEBUG_TIMING === "1") workerDiagnostic("info", "oma_sandbox_tool_finished", {
      session_id: this.sessionId, tool_name: tool.name, timestamp: new Date().toISOString(), duration_ms: Date.now() - start,
    });
    return result;
  }

  async close(): Promise<void> {
    this.lifetime.abort();
    const connection = this.connection;
    if (!connection) return;
    await connection.client.close();
    await connection.initialization?.catch(() => {});
    if (this.connection === connection) this.connection = undefined;
  }
}

async function rejectionMessage(response: Response): Promise<string> {
  let detail = `HTTP ${response.status}`;
  const reader = response.body?.getReader();
  if (!reader) return `Sandbox tool call was not executed: ${detail}`;
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (length <= 2048) {
      const { value, done } = await reader.read();
      if (done) {
        const message = JSON.parse(Buffer.concat(chunks).toString())?.error?.message;
        if (typeof message === "string" && message && !/(?:authorization|cookie|token|secret|password|credential|bearer|api[_ -]?key|https?:\/\/)/i.test(message))
          detail += `: ${message.replace(/[\x00-\x1f\x7f]/g, " ").slice(0, 500)}`;
        break;
      }
      length += value.byteLength;
      if (length <= 2048) chunks.push(value);
    }
  } catch {}
  finally { void reader.cancel().catch(() => {}); }
  return `Sandbox tool call was not executed: ${detail}`;
}
