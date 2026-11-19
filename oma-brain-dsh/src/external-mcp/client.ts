import { isConnectFailure, notForwardedDetail } from "../ccrv2/tool-transport.ts";
import { canonicalJson } from "../ccrv2/json-equality.ts";
import { createHash } from "node:crypto";
import { isDeepStrictEqual } from "node:util";
import {
  Client,
  ProtocolError,
  ProtocolErrorCode,
  SdkHttpError,
  SSEClientTransport,
  StreamableHTTPClientTransport,
  TRACEPARENT_META_KEY,
  TRACESTATE_META_KEY,
} from "@modelcontextprotocol/client";
import { currentToolTrace, traceHooks } from "../observability/trace-context.ts";
import { object, type Payload } from "../json.ts";
import { type RemoteTool } from "../runtime/contracts.ts";
import type { ExternalServer } from "./config.ts";

export function mcpInvocation(tool: RemoteTool, args: Payload, legacy = false): string {
  const input = [tool.name, tool.inputSchema, !!tool.readOnly, args];
  const digest = createHash("sha256").update(JSON.stringify(legacy ? input : canonicalJson(input))).digest("hex");
  return legacy ? digest : "v2:" + digest;
}

export function connectedTools(tools: RemoteTool[]): string {
  return JSON.stringify({tools:tools.map(({name,description}) => ({name,description}))});
}

export class ExternalMcpClient {
  private client: Client;
  private transport: SSEClientTransport | StreamableHTTPClientTransport;
  private readonly options;
  private authenticationRejected = false;
  private oauthRejected = false;
  private rejectedCallSession = false;
  private notForwarded?: string;

  get authenticationFailed(): boolean { return this.authenticationRejected; }
  get oauthRequired(): boolean { return this.oauthRejected; }

  constructor(readonly server: ExternalServer, fetcher: typeof fetch = fetch) {
    traceHooks.registerMcpServer(server.prefix, server.name);
    this.client = this.createClient();
    this.options = {
      requestInit: { headers: server.headers, redirect: "error" as const },
      fetch: async (input: string | URL | Request, init?: RequestInit) => {
        const toolCall = isToolCall(init);
        let response: Response;
        try {
          response = await fetcher(input, { ...init, redirect: "error" });
        } catch (error) {
          if (toolCall && isConnectFailure(error)) this.notForwarded = "OMA MCP gateway is unreachable";
          throw error;
        }
        if (response.status === 401 || response.status === 403) this.authenticationRejected = true;
        if (response.status === 401 && /\bBearer\b/i.test(response.headers.get("WWW-Authenticate") ?? "")) this.oauthRejected = true;
        if (this.isRejectedSessionCall(response, init)) this.rejectedCallSession = true;
        if (toolCall && response.headers.get("X-Oma-Mcp-Not-Forwarded") === "true")
          this.notForwarded = await notForwardedDetail(response.clone());
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
        ...(tool.annotations?.readOnlyHint === true ? { readOnly: true } : {}),
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
    authorize?: (canRetry: boolean) => Promise<void>,
  ): Promise<Payload> {
    this.authenticationRejected = false;
    this.oauthRejected = false;
    this.notForwarded = undefined;
    try {
      return object(
        await this.callWithSessionRecovery(tool, args, signal),
      );
    } catch (error) {
      signal.throwIfAborted();
      if (this.authenticationRejected) {
        if (this.oauthRejected && authorize) {
          const mayRetry = tool.readOnly === true || this.notForwarded !== undefined;
          await authorize(mayRetry);
          if (mayRetry) {
            const tools = await this.reconnect(signal);
            const current = tools.find(candidate => candidate.remoteName === tool.remoteName);
            if (!current || !isDeepStrictEqual(current.inputSchema, tool.inputSchema) || tool.readOnly && !current.readOnly)
              return { isError: true, content: [{ type: "text", text: "授权后工具定义已变化，原操作未重放。" }] };
            return this.call(tool, args, signal, authenticationFailure);
          }
          return { isError: true, content: [{ type: "text", text: "授权已恢复，但原操作结果未知；请先核对外部状态，不要重复执行写操作。" }] };
        }
        await authenticationFailure?.(this.server.name, "tool_call");
        return {
          isError: true,
          content: [{ type: "text", text: "MCP server authentication failed during tool execution." }],
        };
      }
      if (this.notForwarded !== undefined)
        return {
          isError: true,
          content: [{ type: "text", text: `MCP tool call was not executed: ${this.notForwarded}` }],
        };
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
    if (!(this.transport instanceof StreamableHTTPClientTransport) || response.status !== 404 || !this.transport.sessionId || !isToolCall(init)) return false;
    return new Headers(init?.headers).get("Mcp-Session-Id") === this.transport.sessionId;
  }

  private requestTool(tool: RemoteTool, args: Payload, signal: AbortSignal) {
    this.notForwarded = undefined;
    const trace = currentToolTrace();
    const meta = trace ? { _meta: { [TRACEPARENT_META_KEY]: trace.traceparent, ...(trace.tracestate ? { [TRACESTATE_META_KEY]: trace.tracestate } : {}) } } : {};
    return this.client.callTool(
      { name: tool.remoteName, arguments: args, ...meta },
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

  async reconnect(signal: AbortSignal): Promise<RemoteTool[]> {
    await this.client.close();
    signal.throwIfAborted();
    this.authenticationRejected = false;
    this.oauthRejected = false;
    this.client = this.createClient();
    this.transport = this.server.type === "sse"
      ? new SSEClientTransport(this.server.url, this.options)
      : new StreamableHTTPClientTransport(this.server.url, this.options);
    return this.discover(signal);
  }
}

function isToolCall(init?: RequestInit): boolean {
  if (init?.method !== "POST" || typeof init.body !== "string") return false;
  try { return object(JSON.parse(init.body)).method === "tools/call"; }
  catch { return false; }
}
