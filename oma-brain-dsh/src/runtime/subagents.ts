import type { Context } from "@deepseek-ai/cordis";
import { DshSubagents } from "./dsh-subagents.ts";
import { SessionRuntime } from "./session.ts";
import {
  MAX_LIVE_SUBAGENT_THREADS,
  type SubagentBridge,
  type SubagentThread,
  type ToolExecutionBridge,
} from "./contracts.ts";

export class Subagents {
  private reconciling?: Promise<void>;
  get busy(): boolean {
    return this.threads.some((thread) => thread.status === "running");
  }
  get lastIdle(): number {
    return Math.max(this.parent.lastIdle, this.dsh?.lastIdle ?? 0);
  }
  private dsh?: DshSubagents;
  constructor(
    readonly parent: SessionRuntime,
    readonly bridge: SubagentBridge,
  ) {}

  wait(): Promise<boolean> {
    return this.dsh?.wait() ?? Promise.resolve(false);
  }
  settleEvents(): Promise<void> {
    return this.dsh?.settleEvents() ?? Promise.resolve();
  }

  get threads(): SubagentThread[] {
    return this.bridge.threads();
  }

  async restore(): Promise<void> {
    if (this.dsh) this.dsh.recovering = true;
    try {
      for (const thread of this.threads) {
        this.bridge.bindPersistence(thread.id);
        if (thread.status !== "running") continue;
        thread.status = "idle";
        const unbind = this.parent.config.bindThread?.(thread.id);
        let child: SessionRuntime | undefined;
        try {
          child = await this.createRuntime(thread);
          await child.open();
          if (this.parent.config.preserveOpenTurns) {
            let open = false;
            for (const event of child.agent.session.snapshotEvents()) {
              if (event.type === "turn/start") open = true;
              if (event.type === "turn/end") open = false;
            }
            if (open) {
              await child.run("", {
                continuation: true,
                beforeRun: async () => {},
                afterRun: async () => {},
              });
              for (const event of child.agent.session.snapshotEvents()) {
                if (event.type === "turn/start") open = true;
                if (event.type === "turn/end") open = false;
              }
              if (open)
                throw Object.assign(new Error("Child turn is suspended"), {
                  code: "OMA_DSR_SUSPEND",
                });
            }
          }
          await child.flushLog();
          await this.bridge.emit({ kind: "idle", thread, completed: false });
        } finally {
          try {
            await child?.close();
          } finally {
            unbind?.();
          }
        }
      }
    } finally {
      if (this.dsh) this.dsh.recovering = false;
    }
  }

  async register(scope: Context): Promise<void> {
    if (!this.parent.config.subagents) return;
    this.dsh = new DshSubagents(this.parent, this.bridge, (thread) =>
      this.createRuntime(thread),
    );
    await this.dsh.register(scope, (signal) => this.reconcileCapacity(signal));
  }

  private async reconcileCapacity(signal: AbortSignal): Promise<void> {
    if (
      !this.reconciling &&
      this.threads.filter((thread) => thread.status !== "terminated").length >=
        MAX_LIVE_SUBAGENT_THREADS &&
      this.bridge.archivedThreads
    ) {
      this.reconciling = this.retireArchived(signal).finally(() => {
        this.reconciling = undefined;
      });
    }
    await this.reconciling;
    signal.throwIfAborted();
  }

  private async retireArchived(signal: AbortSignal): Promise<void> {
    const candidates = this.threads
      .filter((thread) => thread.status === "idle")
      .map((thread) => thread.id);
    const archived = new Set(
      await this.bridge.archivedThreads!(candidates, signal),
    );
    for (const thread of this.threads)
      if (thread.status === "idle" && archived.has(thread.id))
        thread.status = "terminated";
  }

  private async createRuntime(thread: SubagentThread): Promise<SessionRuntime> {
    const config = this.parent.config;
    const mcp = await config.threadMcpContext?.(thread);
    const execution = this.bridge.toolExecution(thread.id);
    const bridge: ToolExecutionBridge = {
      approve: (request, signal) => execution.approve(request, signal),
      runTool: (callId, execute, readOnly) =>
        execution.runTool(callId, execute, readOnly),
      approvalChanged: (name, callId, effective) =>
        execution.approvalChanged(name, callId, effective),
    };
    return new SessionRuntime(
      this.parent.ctx,
      {
        ...config,
        id: thread.id,
        model: thread.model,
        tools: thread.tools,
        toolPermissions: {
          ...(mcp ? mcp.toolPermissions : config.toolPermissions),
        },
        externalMcpConfig: mcp ? mcp.config : config.externalMcpConfig,
        externalMcpFetch: mcp?.fetcher ?? config.externalMcpFetch,
        subagents: undefined,
        bindThread: undefined,
        setup: undefined,
        systemPrompt:
          config.subagentSystemPrompt?.(thread) ??
          `${config.systemPrompt}\n\nYou are subagent ${thread.name}. Your conversation is isolated. Delegation tools are unavailable. Report your findings to the parent.\nSpecialist instructions:\n${thread.systemPrompt}`,
      },
      {
        onExternalMcpAuthenticationFailure: (serverName, phase) =>
          this.parent.hooks.onExternalMcpAuthenticationFailure?.(
            serverName,
            phase,
          ) ?? Promise.resolve(),
        beforeRequest: () => this.settleEvents(),
        onDiagnostic: (diagnostic) =>
          this.bridge.onDiagnostic?.(thread.id, diagnostic),
        setupTools: (scope) => this.bridge.setupTools?.(scope),
        onOpen: (session) => this.bridge.onOpen?.(session) ?? Promise.resolve(),
        flushLog: this.bridge.flushLog?.bind(this.bridge),
        onEvent: (event, session) =>
          this.bridge.onEvent(thread.id, event, session),
        onStream: (frame) => this.bridge.onStream(thread.id, frame),
        stop: () => this.parent.hooks.stop(),
      },
      bridge,
      this.parent,
    );
  }

  async flush(): Promise<void> {
    await this.dsh?.flush();
  }
  interrupt(id?: string): void {
    if (
      id &&
      !this.threads.some(
        (thread) => thread.id === id && thread.status !== "terminated",
      )
    )
      throw new Error("Unknown or archived subagent thread");
    this.dsh?.interrupt(id);
  }
  cancelForShutdown(): void {
    this.dsh?.interrupt();
  }
  stop(): void {
    this.dsh?.interrupt();
  }
  async close(): Promise<void> {
    await this.dsh?.close();
  }
}
