import type { Context } from "@deepseek-ai/cordis";
import { DshSubagents } from "./dsh-subagents.ts";
import { SessionRuntime } from "./session.ts";
import { MAX_LIVE_SUBAGENT_THREADS, type SubagentBridge, type SubagentThread, type ToolExecutionBridge } from "./contracts.ts";

export class Subagents {
  private reconciling?: Promise<void>;
  get busy(): boolean { return this.threads.some((thread) => thread.status === "running"); }
  get lastIdle(): number { return Math.max(this.parent.lastIdle, this.dsh?.lastIdle ?? 0); }
  private dsh?: DshSubagents;
  constructor(readonly parent: SessionRuntime, readonly bridge: SubagentBridge) {}

  wait(): Promise<boolean> { return this.dsh?.wait() ?? Promise.resolve(false); }
  settleEvents(): Promise<void> { return this.dsh?.settleEvents() ?? Promise.resolve(); }

  get threads(): SubagentThread[] {
    return this.bridge.threads();
  }

  async loadThreads(): Promise<void> {
    await this.bridge.restoreThreads?.();
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
          await child.flushLog();
          await this.bridge.emit({ kind: "idle", thread, completed: false });
        } finally {
          try { await child?.close(); } finally { unbind?.(); }
        }
      }
    } finally { if (this.dsh) this.dsh.recovering = false; }
  }

  async register(scope: Context): Promise<void> {
    if (!this.parent.config.subagents) return;
    this.dsh = new DshSubagents(this.parent, this.bridge, thread => this.createRuntime(thread));
    await this.dsh.register(scope, signal => this.reconcileCapacity(signal));
  }

  private async reconcileCapacity(signal: AbortSignal): Promise<void> {
    if (!this.reconciling && this.threads.filter((thread) => thread.status !== "terminated").length >= MAX_LIVE_SUBAGENT_THREADS && this.bridge.archivedThreads) {
      this.reconciling = this.retireArchived(signal).finally(() => { this.reconciling = undefined; });
    }
    await this.reconciling;
    signal.throwIfAborted();
  }

  private async retireArchived(signal: AbortSignal): Promise<void> {
    const candidates = this.threads.filter(thread => thread.status === "idle").map(thread => thread.id);
    const archived = new Set(await this.bridge.archivedThreads!(candidates, signal));
    for (const thread of this.threads)
      if (thread.status === "idle" && archived.has(thread.id)) thread.status = "terminated";
  }

  private async createRuntime(thread: SubagentThread): Promise<SessionRuntime> {
    const config = this.parent.config;
    const mcp = await config.threadMcpContext?.(thread.id);
    let child: SessionRuntime;
    const execution = this.bridge.toolExecution(thread.id);
    const bridge: ToolExecutionBridge = {
      approve: (request, signal) => execution.approve(request, signal),
      runTool: (callId, execute, readOnly) => execution.runTool(callId, execute, readOnly),
      approvalChanged: (name, callId, effective) => execution.approvalChanged(name, callId, effective),
    };
    child = new SessionRuntime(this.parent.ctx, {
      ...config, id: thread.id, model: thread.model, tools: thread.tools,
      toolPermissions: { ...config.toolPermissions },
      refreshSessionContext: config.refreshSessionContext
        ? (signal) => config.refreshSessionContext!(signal, thread.id) : undefined,
      externalMcpConfig: mcp ? mcp.config : config.externalMcpConfig,
      externalMcpFetch: mcp?.fetcher ?? config.externalMcpFetch,
      subagents: undefined, bindThread: undefined, setup: undefined,
      systemPrompt: config.subagentSystemPrompt?.(thread)
        ?? `${config.systemPrompt}\n\nYou are subagent ${thread.name}. Your conversation is isolated. Delegation tools are unavailable. Report your findings to the parent.\nSpecialist instructions:\n${thread.systemPrompt}`,
    }, {
      onExternalMcpAuthenticationFailure: (serverName, phase) => this.parent.hooks.onExternalMcpAuthenticationFailure?.(serverName, phase) ?? Promise.resolve(),
      beforeRequest: async () => { await this.settleEvents(); await child.flushLog(); await this.bridge.beforeRequest?.(thread.id); },
      onDiagnostic: diagnostic => this.bridge.onDiagnostic?.(thread.id, diagnostic),
      onOpen: session => this.bridge.onOpen?.(session) ?? Promise.resolve(),
      afterFlushLog: session => this.bridge.afterFlushLog?.(session) ?? Promise.resolve(),
      onEvent: (event, session) => this.bridge.onEvent(thread.id, event, session),
      onStream: (frame) => this.bridge.onStream(thread.id, frame),
      stop: () => this.parent.hooks.stop(),
    }, bridge, this.parent);
    return child;
  }

  async flush(): Promise<void> { await this.dsh?.flush(); }
  interrupt(id?: string): void {
    if (id && !this.threads.some(thread => thread.id === id && thread.status !== "terminated"))
      throw new Error("Unknown or archived subagent thread");
    this.dsh?.interrupt(id);
  }
  cancelForShutdown(): void { this.dsh?.interrupt(undefined, true); }
  stop(): void { this.cancelForShutdown(); }
  async close(): Promise<void> { await this.dsh?.close(); }
}
