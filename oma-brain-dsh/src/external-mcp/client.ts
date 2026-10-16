import {
  Client,
  ProtocolError,
  ProtocolErrorCode,
  SSEClientTransport,
  StreamableHTTPClientTransport,
} from "@modelcontextprotocol/client";
import { object, type Payload } from "../json.ts";
import type { RemoteTool, SandboxExecutor } from "../runtime/contracts.ts";
import type { ExternalServer } from "./config.ts";

export class ExternalMcpClient implements SandboxExecutor {
  private readonly client: Client;
  private readonly transport;

  constructor(readonly server: ExternalServer, fetcher: typeof fetch = fetch) {
    this.client = new Client(
      { name: "oma-brain-dsh", version: "0.1.0" },
      {
        versionNegotiation: { mode: server.type === "sse" ? "legacy" : "auto" },
      },
    );
    const options = {
      requestInit: { headers: server.headers, redirect: "error" as const },
      fetch: (input: string | URL | Request, init?: RequestInit) =>
        fetcher(input, { ...init, redirect: "error" }),
    };
    this.transport =
      server.type === "sse"
        ? new SSEClientTransport(server.url, options)
        : new StreamableHTTPClientTransport(server.url, options);
  }

  async discover(signal: AbortSignal): Promise<RemoteTool[]> {
    await this.client.connect(this.transport, { signal, timeout: 30_000 });
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
  ): Promise<Payload> {
    try {
      return object(
        await this.client.callTool(
          { name: tool.remoteName, arguments: args },
          { signal, timeout: 185_000 },
        ),
      );
    } catch (error) {
      if (!(error instanceof ProtocolError) || error.code !== ProtocolErrorCode.InvalidParams)
        throw error;
      return {
        isError: true,
        content: [{ type: "text", text: `MCP tool call failed: ${error.message}` }],
      };
    }
  }

  async close(): Promise<void> {
    await this.client.close();
  }
}
