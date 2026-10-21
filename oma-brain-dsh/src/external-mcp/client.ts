import { isDeepStrictEqual } from "node:util";
import {
  Client,
  ProtocolError,
  ProtocolErrorCode,
  SdkHttpError,
  SSEClientTransport,
  StreamableHTTPClientTransport,
} from "@modelcontextprotocol/client";
import { object, type Payload } from "../json.ts";
import type { RemoteTool } from "../runtime/contracts.ts";
import type { ExternalServer } from "./config.ts";

export class ExternalMcpClient {
  private client: Client;
  private transport: SSEClientTransport | StreamableHTTPClientTransport;
  private readonly options;
  private authenticationRejected = false;
  private rejectedCallSession = false;

  get authenticationFailed(): boolean { return this.authenticationRejected; }

  constructor(readonly server: ExternalServer, fetcher: typeof fetch = fetch) {
    this.client = this.createClient();
    this.options = {
      requestInit: { headers: server.headers, redirect: "error" as const },
      fetch: async (input: string | URL | Request, init?: RequestInit) => {
        const response = await fetcher(input, { ...init, redirect: "error" });
        if (response.status === 401 || response.status === 403) this.authenticationRejected = true;
        if (this.isRejectedSessionCall(response, init)) this.rejectedCallSession = true;
        return response;
      },
    };
    this.transport =
      server.type === "sse"
        ? new SSEClientTransport(server.url, this.options)
        : new StreamableHTTPClientTransport(server.url, this.options);
  }

  async discover(signal: AbortSignal): Promise<RemoteTool[]> {
    try {
      await this.client.connect(this.transport, { signal, timeout: 30_000 });
    } catch (error) {
      signal.throwIfAborted();
      if (this.server.type !== "http" || this.authenticationRejected || !(error instanceof SdkHttpError) || ![400, 404, 405].includes(error.status))
        throw error;
      await this.client.close();
      signal.throwIfAborted();
      this.client = this.createClient(true);
      this.transport = new SSEClientTransport(this.server.url, this.options);
      await this.client.connect(this.transport, { signal, timeout: 30_000 });
    }
    const { tools } = await this.client.listTools(undefined, {
      signal,
      timeout: 30_000,
    });
    return tools
      .filter((tool) => !this.server.disabledTools.has(tool.name))
      .map((tool) => ({
        name: `mcp__${this.server.prefix}__${tool.name}`,
        remoteName: tool.name,
        description: tool.description ?? tool.name,
        inputSchema: tool.inputSchema,
      }));
  }

  async provision(signal: AbortSignal): Promise<void> {
    signal.throwIfAborted();
  }

  async call(
    tool: RemoteTool,
    args: Payload,
    signal: AbortSignal,
    authenticationFailure?: (serverName: string, phase?: "discovery" | "tool_call") => Promise<void>,
  ): Promise<Payload> {
    this.authenticationRejected = false;
    try {
      return object(
        await this.callWithSessionRecovery(tool, args, signal),
      );
    } catch (error) {
      signal.throwIfAborted();
      if (this.authenticationRejected) {
        await authenticationFailure?.(this.server.name, "tool_call");
        return {
          isError: true,
          content: [{ type: "text", text: "MCP server authentication failed during tool execution." }],
        };
      }
      if (!(error instanceof ProtocolError) || error.code !== ProtocolErrorCode.InvalidParams)
        throw error;
      return {
        isError: true,
        content: [{ type: "text", text: `MCP tool call failed: ${error.message}` }],
      };
    }
  }

  private createClient(legacy = this.server.type === "sse"): Client {
    return new Client(
      { name: "oma-brain-dsh", version: "0.1.0" },
      { versionNegotiation: { mode: legacy ? "legacy" : "auto" } },
    );
  }

  private isRejectedSessionCall(response: Response, init?: RequestInit): boolean {
    if (!(this.transport instanceof StreamableHTTPClientTransport) || response.status !== 404 || !this.transport.sessionId || init?.method !== "POST" || typeof init.body !== "string") return false;
    if (new Headers(init.headers).get("Mcp-Session-Id") !== this.transport.sessionId) return false;
    try { return object(JSON.parse(init.body)).method === "tools/call"; }
    catch { return false; }
  }

  private requestTool(tool: RemoteTool, args: Payload, signal: AbortSignal) {
    return this.client.callTool(
      { name: tool.remoteName, arguments: args },
      { signal, timeout: 185_000 },
    );
  }

  private async callWithSessionRecovery(tool: RemoteTool, args: Payload, signal: AbortSignal) {
    this.rejectedCallSession = false;
    try { return await this.requestTool(tool, args, signal); }
    catch (error) {
      signal.throwIfAborted();
      if (!this.rejectedCallSession || !(error instanceof SdkHttpError) || error.status !== 404) throw error;
    }
    this.rejectedCallSession = false;
    await this.client.close();
    signal.throwIfAborted();
    this.client = this.createClient();
    this.transport = new StreamableHTTPClientTransport(this.server.url, this.options);
    try {
      await this.client.connect(this.transport, { signal, timeout: 30_000 });
      const { tools } = await this.client.listTools(undefined, { signal, timeout: 30_000 });
      const current = tools.find(candidate => candidate.name === tool.remoteName);
      if (!current || this.server.disabledTools.has(current.name) || !isDeepStrictEqual(current.inputSchema, tool.inputSchema))
        return { isError: true, content: [{ type: "text" as const, text: "MCP tool definition changed after session expiration; tool was not replayed." }] };
    } catch (error) {
      signal.throwIfAborted();
      if (this.authenticationRejected) throw error;
      return { isError: true, content: [{ type: "text" as const, text: "MCP session reinitialization failed; tool was not replayed." }] };
    }
    return this.requestTool(tool, args, signal);
  }

  async close(): Promise<void> {
    await this.client.close();
  }
}
