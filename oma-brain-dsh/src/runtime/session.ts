import type { Context } from "@deepseek-ai/cordis";
import type { Agent, AgentHandle } from "@deepseek-ai/dsh-agent";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { ExternalMcp } from "../external-mcp/index.ts";
import * as SandboxTools from "../sandbox-tools/index.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";
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
  private finishing?: ReturnType<typeof Promise.withResolvers<void>>;
  private readonly sandboxScope: symbol;
  readonly externalMcp: ExternalMcp;

  constructor(
    readonly ctx: Context,
    readonly config: SessionRuntimeConfig,
    readonly hooks: SessionRuntimeHooks,
    readonly bridge: ToolExecutionBridge,
    readonly parent?: SessionRuntime,
  ) {
    this.externalMcp = parent?.externalMcp ?? new ExternalMcp(config.externalMcpConfig, config.externalMcpFetch);
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
      if (!this.parent) {
        scope.effect(() => () => this.externalMcp.close());
        await this.config.setup?.(scope);
      }
      if (!scope.get("sandboxExecutor")) throw new Error("Session requires a SandboxExecutor provider");
      scope.systemPrompt.section({
        name: "oma",
        order: 0,
        complete: true,
        interpolate: false,
        text: this.config.systemPrompt,
      });
      await scope.plugin(SandboxTools, {
        bridge: this.bridge,
        tools: this.config.tools,
      });
      await this.externalMcp.register(scope, this.bridge, this.abort.signal);
      this.hooks.setupTools?.(scope);
      scope.on("session/event", (session, event) => {
        if (session.id !== this.config.id) return;
        if (process.env.OMA_DEBUG_TIMING === "1" && ["assistant/message", "tool/result", "turn/end"].includes(event.type)) {
          const data = event.data as { message?: { content?: { type: string }[] }; reason?: { kind: string } };
          workerDiagnostic("info", "oma_runtime_event", {
            session_id: session.id, event_type: event.type, timestamp: new Date().toISOString(),
            block_types: data.message?.content?.map(block => block.type), reason: data.reason?.kind,
          });
        }
        this.hooks.onEvent(event);
        if (event.type === "turn/end") {
          this.turnCompleted = event.data.reason?.kind === "completed";
        }
      });
      scope.on("agent/request", async ({ signal }, next) => {
        signal.throwIfAborted();
        await this.hooks.beforeRequest();
        signal.throwIfAborted();
        return next();
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

  async run(text: string, hooks: TurnHooks): Promise<void> {
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
      this.agent.followup(createUserMessage({
        source: { kind: "user" },
        content: [{ type: "text", text }],
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
    finally { if (!this.parent) await this.externalMcp.close(); }
  }
}
