import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import type { RemoteTool, ToolExecutionBridge } from "../runtime/contracts.ts";
import { externalServers } from "./config.ts";
import { ExternalMcpClient } from "./client.ts";

export class ExternalMcp {
  private readonly clients: ExternalMcpClient[];
  private closing?: Promise<void>;
  private discovery?: Promise<
    { client: ExternalMcpClient; tool: RemoteTool }[]
  >;

  constructor(config: unknown) {
    this.clients = externalServers(config).map(
      (server) => new ExternalMcpClient(server),
    );
  }

  private async discover(signal: AbortSignal) {
    const bindings: { client: ExternalMcpClient; tool: RemoteTool }[] = [];
    const names = new Set<string>();
    try {
      for (const client of this.clients) {
        for (const tool of await client.discover(signal)) {
          if (!/^mcp__[a-zA-Z0-9_-]+$/.test(tool.name) || names.has(tool.name))
            throw new Error("Invalid or duplicate external MCP tool name");
          names.add(tool.name);
          bindings.push({ client, tool });
        }
      }
      return bindings;
    } catch {
      try {
        await this.close();
      } finally {
        throw new Error(
          "External MCP discovery failed; session initialization stopped",
        );
      }
    }
  }

  async register(
    ctx: Context,
    bridge: ToolExecutionBridge,
    signal: AbortSignal,
  ): Promise<void> {
    const bindings = await (this.discovery ??= this.discover(signal));
    signal.throwIfAborted();
    for (const { client, tool } of bindings) {
      const execution = new ToolExecution(client, bridge);
      ctx.tools.register({
        name: tool.name,
        description: tool.description,
        parameters: tool.inputSchema,
        output: {
          schema: { type: "string" },
          render: (_args, value) => [{ type: "text", text: String(value) }],
        },
        isConcurrencySafe: () => false,
        execute: (args, exec) => execution.execute(tool, object(args), exec),
      });
    }
  }

  close(): Promise<void> {
    return (this.closing ??= this.closeClients());
  }

  private async closeClients(): Promise<void> {
    const results = await Promise.allSettled(
      this.clients.map((client) => client.close()),
    );
    if (results.some((result) => result.status === "rejected"))
      throw new Error("External MCP connection cleanup failed");
  }
}
