import type { Context, Fiber } from "@deepseek-ai/cordis";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import { createUserMessage, type ContentBlock, type ToolCallBlock } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { ContextRecovery } from "./compaction.ts";
import { ExternalMcp } from "../external-mcp/index.ts";
import * as SandboxTools from "../sandbox-tools/index.ts";
import { failureDiagnostic, workerDiagnostic } from "../ccrv2/diagnostics.ts";
import { approveToolCall, releaseToolApproval, clearToolApprovals, prefetchMcpApprovals } from "./tool-approvals.ts";
import { object } from "../json.ts";
import type {
  SessionRuntimeConfig,
  SessionRuntimeHooks,
  ToolExecutionBridge,
  TurnHooks,
} from "./contracts.ts";

/** Native dsh setup, serial tools, turn execution and cancellation. */
export class SessionRuntime {
  handle?: AgentHandle;
  readonly abort = new AbortController();
  busy = false;
  lastIdle = Date.now();
  private turnCompleted = false;
  private readonly contextRecovery = new ContextRecovery();
  private toolCalls: ToolCallBlock[] = [];
  appendedSystem = "";
  private finishing?: ReturnType<typeof Promise.withResolvers<void>>;
  private readonly sandboxScope: symbol;
  readonly externalMcp: ExternalMcp;
  private sandboxTools?: Fiber;

  constructor(
    readonly ctx: Context,
    readonly config: SessionRuntimeConfig,
    readonly hooks: SessionRuntimeHooks,
    readonly bridge: ToolExecutionBridge,
    readonly parent?: SessionRuntime,
  ) {
    this.externalMcp = new ExternalMcp(config.externalMcpConfig, config.externalMcpFetch);
    this.sandboxScope = parent?.sandboxScope ?? Symbol("sandboxExecutor");
  }

  get agent(): Agent {
    if (!this.handle) throw new Error("Agent is not initialized");
    return this.handle.agent;
  }

  async open(): Promise<void> {
    const setup = async (agentScope: Context) => {
      // dsh creates agent scopes from the loop context. Explicitly join the
      // parent's service label; agent ownership alone does not inherit services.
      const scope = agentScope.isolate("sandboxExecutor", this.sandboxScope);
      scope.effect(() => () => this.externalMcp.close());
      if (!this.parent) {
        await this.config.setup?.(scope);
      }
      if (!scope.get("sandboxExecutor")) throw new Error("Session requires a SandboxExecutor provider");
      scope.systemPrompt.section({
        name: "oma",
        order: 0,
        complete: true,
        interpolate: false,
        text: () => [this.config.systemPrompt, this.appendedSystem].filter(Boolean).join("\n\n"),
      });
      this.sandboxTools = await scope.plugin(SandboxTools, {
        bridge: this.bridge,
        tools: this.config.tools,
      });
      await this.externalMcp.register(scope, this.bridge, this.abort.signal, async (serverName, phase) => {
        await this.hooks.onExternalMcpAuthenticationFailure?.(serverName, phase);
      }, scope.get("sandboxExecutor")!, async (serverName) => {
        await this.hooks.onExternalMcpConnectionFailure?.(serverName);
      });
      this.hooks.setupTools?.(scope);
      scope.on("tools/pre-execute", async (exec, next) => {
        const decision = await next();
        if (decision.kind !== "allow") return decision;
        if (scope.tools.get(exec.name, exec.agent)) return decision;
        const approval = await approveToolCall(this.bridge, {
          callId: exec.callId, toolName: exec.name, input: object(exec.arguments),
          available: false,
        }, exec.signal);
        return approval.allowed ? decision : { kind: "deny", reason: approval.reason || "Tool use denied by OMA" };
      });
      scope.on("tools/execute", async (exec, next) => {
        prefetchMcpApprovals(this.bridge, this.toolCalls, exec.callId,
          name => name.startsWith("mcp__") && !!scope.tools.get(name, exec.agent), exec.signal);
        const approval = await approveToolCall(this.bridge, {
          callId: exec.callId, toolName: exec.name, input: object(exec.arguments), available: true,
        }, exec.signal);
        if (!approval.allowed) throw new Error(approval.reason || "Tool use denied by OMA");
        return next();
      });
      scope.on("session/event", (session, event) => {
        if (session.id !== this.config.id) return;
        if (process.env.OMA_DEBUG_TIMING === "1" && ["assistant/message", "tool/result", "turn/end"].includes(event.type)) {
          const data = event.data as { message?: { content?: { type: string }[] }; reason?: { kind: string } };
          workerDiagnostic("info", "oma_runtime_event", {
            session_id: session.id, event_type: event.type, timestamp: new Date().toISOString(),
            block_types: data.message?.content?.map(block => block.type), reason: data.reason?.kind,
          });
        }
        if (event.type === "assistant/message")
          this.toolCalls = event.data.message.content.filter((block): block is ToolCallBlock => block.type === "tool-call");
        this.hooks.onEvent(event);
        if (event.type === "tool/result") releaseToolApproval(this.bridge, event.data.message.toolCallId);
        if (event.type === "turn/end") {
          this.toolCalls = [];
          clearToolApprovals(this.bridge);
          this.turnCompleted = event.data.reason?.kind === "completed";
        }
      });
      scope.on("agent/request", async ({ signal }, next) => {
        signal.throwIfAborted();
        await this.hooks.beforeRequest();
        signal.throwIfAborted();
        return next();
      });
      scope.on("agent/request-error", async ({ agent, failure, turn, step, signal }, next) => {
        signal.throwIfAborted();
        const recovered = await this.contextRecovery.recover(agent.session, failure, turn, step,
          () => this.ctx.sessions.flush(agent.session),
          async tokens => {
            signal.throwIfAborted();
            await this.hooks.onContextCompacted?.(tokens);
          });
        return recovered ? { kind: "retry" } : next();
      });
      scope.on("agent/assistant-stream", ({ frame }) => this.hooks.onStream(frame));
    };
    const options = {
      agentOptions: { provider: this.config.provider, model: this.config.model },
      setup,
      parentAgent: this.parent?.agent,
      signal: this.abort.signal,
    };
    this.handle = (await this.ctx.sessionPersistence.stat(SessionId(this.config.id)))
      ? await this.ctx.agents.resume({ resumeSessionId: SessionId(this.config.id), ...options })
      : await this.ctx.agents.create({
          sessionId: SessionId(this.config.id),
          meta: { cwd: this.config.workDir, ...(this.parent ? {
            parentSession: this.parent.agent.session.id,
            origin: "subagent" as const,
            delegationDepth: 1,
          } : {}) },
          ...options,
        });
  }

  async refreshTools(): Promise<void> {
    if (this.parent || !this.config.refreshTools) return;
    this.abort.signal.throwIfAborted();
    const next = await this.config.refreshTools(this.abort.signal);
    this.abort.signal.throwIfAborted();
    if (JSON.stringify(next.tools) !== JSON.stringify(this.config.tools)) {
      if (!this.sandboxTools) throw new Error("Session tools are not initialized");
      this.sandboxTools.update({ bridge: this.bridge, tools: next.tools });
      await this.sandboxTools.await();
      this.config.tools = next.tools;
    }
    await this.externalMcp.update(next.externalMcpConfig);
    this.config.externalMcpConfig = next.externalMcpConfig;
  }

  async run(input: string | (() => Promise<ContentBlock[]>), hooks: TurnHooks): Promise<void> {
    if (this.busy || this.abort.signal.aborted)
      throw new Error("Session is busy or stopped");
    this.busy = true;
    this.finishing = Promise.withResolvers<void>();
    try {
      this.turnCompleted = false;
      if (await hooks.beforeRun() === false) {
        await hooks.afterRun(false);
        return;
      }
      let content: ContentBlock[];
      try {
        content = typeof input === "string" ? [{ type: "text", text: input }] : await input();
      } catch (error) {
        workerDiagnostic("error", "oma_runtime_input_failed", { session_id: this.config.id, ...failureDiagnostic(error) });
        await hooks.afterRun(false);
        return;
      }
      this.agent.followup(createUserMessage({
        source: { kind: "user" },
        content,
      }));
      await this.agent.whenIdle();
      await this.flushLog();
      await hooks.afterRun(this.turnCompleted);
    } catch (error) {
      this.hooks.stop();
      throw error;
    } finally {
      this.busy = false;
      this.lastIdle = Date.now();
      this.finishing.resolve();
    }
  }

  async flushLog(): Promise<void> {
    await this.ctx.sessions.flush(this.agent.session);
  }

  stop(): void {
    this.abort.abort();
    this.handle?.agent.cancel({ kind: "disposed" });
  }

  async close(): Promise<void> {
    this.stop();
    await this.finishing?.promise;
    try { await this.handle?.dispose(); }
    finally { await this.externalMcp.close(); }
  }
}
