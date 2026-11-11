import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import type {
  RemoteTool,
  ToolExecutionBridge,
  SandboxExecutor,
} from "../runtime/contracts.ts";
import { externalServers } from "./config.ts";
import { ExternalMcpClient } from "./client.ts";
import { persistToolOutput } from "./output.ts";

interface McpRegistration {
  ctx: Context;
  bridge: ToolExecutionBridge;
  signal: AbortSignal;
  connectionFailure: (serverName: string) => Promise<void>;
  authenticationFailure: (
    serverName: string,
    phase?: "discovery" | "tool_call",
  ) => Promise<void>;
  sandbox: SandboxExecutor;
  threadId?: string;
  disposers: (() => void)[];
  installed: Set<string>;
}

export class ExternalMcp {
  private readonly clients: ExternalMcpClient[];
  private closing?: Promise<void>;
  private readonly successful = new Map<ExternalMcpClient, RemoteTool[]>();
  private readonly registrations = new Set<McpRegistration>();
  private lastInput?: string;
  private refreshing?: Promise<void>;
  private discovery?: Promise<
    { client: ExternalMcpClient; tool: RemoteTool }[]
  >;

  constructor(
    config: unknown,
    private readonly fetcher?: typeof fetch,
  ) {
    this.clients = externalServers(config).map(
      (server) => new ExternalMcpClient(server, fetcher),
    );
  }

  private async discover(
    signal: AbortSignal,
    authenticationFailure: (serverName: string) => Promise<void>,
    connectionFailure: (serverName: string) => Promise<void>,
  ) {
    const bindings: { client: ExternalMcpClient; tool: RemoteTool }[] = [];
    const names = new Set<string>();
    try {
      const outcomes = await Promise.allSettled(
        this.clients.map(async (client) => {
          try {
            const cached = this.successful.get(client);
            if (cached) return cached;
            const tools = await client.discover(signal);
            this.successful.set(client, tools);
            return tools;
          } catch (error) {
            await client.close();
            throw error;
          }
        }),
      );
      signal.throwIfAborted();
      for (const [index, outcome] of outcomes.entries()) {
        const client = this.clients[index]!;
        if (outcome.status === "rejected") {
          if (client.authenticationFailed) {
            await authenticationFailure(client.server.name);
          } else {
            await connectionFailure(client.server.name);
          }
          continue;
        }
        for (const tool of outcome.value) {
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
    authenticationFailure: (
      serverName: string,
      phase?: "discovery" | "tool_call",
    ) => Promise<void>,
    sandbox: SandboxExecutor,
    connectionFailure: (serverName: string) => Promise<void>,
    threadId?: string,
  ): Promise<void> {
    const registration: McpRegistration = {
      ctx,
      bridge,
      signal,
      authenticationFailure,
      connectionFailure,
      sandbox,
      threadId,
      disposers: [],
      installed: new Set(),
    };
    this.registrations.add(registration);
    ctx.effect(() => () => {
      this.registrations.delete(registration);
      for (const dispose of registration.disposers.splice(0)) dispose();
    });
    await this.install(registration);
  }

  private async install(registration: McpRegistration): Promise<void> {
    const {
      ctx,
      bridge,
      signal,
      authenticationFailure,
      connectionFailure,
      sandbox,
      threadId,
    } = registration;
    const bindings = await (this.discovery ??= this.discover(
      signal,
      authenticationFailure,
      connectionFailure,
    ));
    signal.throwIfAborted();
    for (const { client, tool } of bindings) {
      if (!this.registrations.has(registration) || this.closing) return;
      if (registration.installed.has(tool.name)) continue;
      registration.installed.add(tool.name);
      const execution = new ToolExecution(
        {
          provision: (signal) => client.provision(signal),
          call: async (remote, args, signal) =>
            persistToolOutput(
              await client.call(remote, args, signal, authenticationFailure),
              sandbox,
              signal,
              { threadId },
            ),
        },
        bridge,
      );
      registration.disposers.push(
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
        }),
      );
    }
  }

  async refreshForInput(input: string): Promise<void> {
    if (this.lastInput === input || this.closing) return this.refreshing;
    const first = this.lastInput === undefined;
    this.lastInput = input;
    if (first) return;
    if (this.refreshing) await this.refreshing;
    this.refreshing = this.retryDiscovery();
    try {
      await this.refreshing;
    } finally {
      this.refreshing = undefined;
    }
  }

  private async retryDiscovery(): Promise<void> {
    let retry = false;
    for (let i = 0; i < this.clients.length; i++) {
      const client = this.clients[i]!;
      if (!this.successful.has(client)) {
        this.clients[i] = new ExternalMcpClient(client.server, this.fetcher);
        retry = true;
      }
    }
    if (!retry) return;
    this.discovery = undefined;
    for (const registration of this.registrations)
      await this.install(registration);
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
