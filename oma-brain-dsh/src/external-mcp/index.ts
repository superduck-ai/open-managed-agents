import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import { requestToolOAuth } from "../runtime/oauth.ts";
import type { RemoteTool, ToolExecutionBridge, SandboxExecutor } from "../runtime/contracts.ts";
import { externalServers } from "./config.ts";
import { connectedTools, ExternalMcpClient, mcpInvocation } from "./client.ts";
import { persistToolOutput } from "./output.ts";

interface McpRegistration {
  ctx: Context;
  bridge: ToolExecutionBridge;
  signal: AbortSignal;
  connectionFailure: (serverName: string) => Promise<void>;
  authenticationFailure: (serverName: string, phase?: "discovery" | "tool_call") => Promise<void>;
  sandbox: SandboxExecutor;
  threadId?: string;
  disposers: (() => void)[];
}

export class ExternalMcp {
  private clients: ExternalMcpClient[];
  private signature: string;
  private readonly failedClients = new Set<ExternalMcpClient>();
  private readonly registrations = new Set<McpRegistration>();
  private closing?: Promise<void>;
  private discovery?: Promise<
    { client: ExternalMcpClient; tool: RemoteTool }[]
  >;

  constructor(config: unknown, private readonly fetcher?: typeof fetch) {
    this.signature = JSON.stringify(config ?? null);
    this.clients = externalServers(config).map(
      (server) => new ExternalMcpClient(server, fetcher),
    );
  }

  private async discover(signal: AbortSignal, authenticationFailure: (serverName: string) => Promise<void>, connectionFailure: (serverName: string) => Promise<void>) {
    const bindings: { client: ExternalMcpClient; tool: RemoteTool }[] = [];
    const names = new Set<string>();
    try {
      const outcomes = await Promise.allSettled(this.clients.map(async (client) => {
        try {
          return await client.discover(signal);
        } catch (error) {
          await client.close();
          throw error;
        }
      }));
      signal.throwIfAborted();
      for (const [index, outcome] of outcomes.entries()) {
        const client = this.clients[index]!;
        if (outcome.status === "rejected") {
          this.failedClients.add(client);
          if (client.authenticationFailed) {
            if (!client.oauthRequired) await authenticationFailure(client.server.name);
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
    authenticationFailure: (serverName: string, phase?: "discovery" | "tool_call") => Promise<void>,
    sandbox: SandboxExecutor,
    connectionFailure: (serverName: string) => Promise<void>,
    threadId?: string,
  ): Promise<void> {
    const registration: McpRegistration = { ctx, bridge, signal, authenticationFailure, connectionFailure, sandbox, threadId, disposers: [] };
    this.registrations.add(registration);
    ctx.effect(() => () => {
      this.registrations.delete(registration);
      for (const dispose of registration.disposers.splice(0)) dispose();
    });
    await this.install(registration);
  }

  private async install(registration: McpRegistration): Promise<void> {
    const { ctx, bridge, signal, authenticationFailure, connectionFailure, sandbox, threadId } = registration;
    const bindings = await (this.discovery ??= this.discover(signal, authenticationFailure, connectionFailure));
    signal.throwIfAborted();
    for (const { client, tool } of bindings) {
      this.installTool(registration, client, tool);
    }
    for (const client of this.clients.filter(candidate => candidate.oauthRequired)) {
      registration.disposers.push(ctx.tools.register({
        name: `mcp__${client.server.prefix}__connect`,
        description: `连接 ${client.server.name} 并发现可用工具。仅在本次任务需要该服务时调用；可能需要用户 OAuth 授权。`,
        parameters: { type: "object", properties: {}, additionalProperties: false },
        output: { schema: { type: "string" }, render: (_args, value) => [{ type: "text", text: String(value) }] },
        isConcurrencySafe: () => false,
        execute: async (_args, exec) => {
          await requestToolOAuth(bridge, {
            kind: "mcp_oauth", mcp_server_name: client.server.name, mcp_server_url: client.server.url.href,
            parent_call_id: exec.callId, reason: "missing",
          }, exec.signal);
          const tools = await client.reconnect(exec.signal);
          this.failedClients.delete(client);
          this.discovery = Promise.resolve([...(await this.discovery ?? []).filter(binding => binding.client !== client), ...tools.map(tool => ({ client, tool }))]);
          for (const tool of tools) this.installTool(registration, client, tool);
          return connectedTools(tools);
        },
      }));
    }
  }

  private installTool(registration: McpRegistration, client: ExternalMcpClient, tool: RemoteTool): void {
    const { ctx, bridge, authenticationFailure, sandbox, threadId } = registration;
      const execution = new ToolExecution({
        provision: (signal) => client.provision(signal),
        call: async (remote, args, signal, _threadId, callId) => persistToolOutput(
          await client.call(remote, args, signal, authenticationFailure, callId ? async canRetry => {
            await requestToolOAuth(bridge, {
              kind: "mcp_oauth", mcp_server_name: client.server.name, mcp_server_url: client.server.url.href,
              parent_call_id: callId, reason: "reauthorization_required",
              ...(canRetry ? {invocation_id:mcpInvocation(remote,args)} : {}),
            }, signal);
          } : undefined), sandbox, signal, { threadId },
        ),
      }, bridge);
      registration.disposers.push(ctx.tools.register({
        name: tool.name,
        description: tool.description,
        parameters: tool.inputSchema,
        output: {
          schema: { type: "string" },
          render: (_args, value) => [{ type: "text", text: String(value) }],
        },
        isConcurrencySafe: () => false,
        execute: (args, exec) => execution.execute(tool, object(args), exec),
      }));
  }

  async update(config: unknown): Promise<void> {
    const signature = JSON.stringify(config ?? null);
    if (signature === this.signature && this.failedClients.size === 0) return;
    const servers = externalServers(config);
    for (const registration of this.registrations) {
      for (const dispose of registration.disposers.splice(0)) dispose();
    }
    await this.close();
    this.clients = servers.map(server => new ExternalMcpClient(server, this.fetcher));
    this.failedClients.clear();
    this.closing = undefined;
    this.discovery = undefined;
    for (const registration of this.registrations) {
      if (!registration.signal.aborted) await this.install(registration);
    }
    this.signature = signature;
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
