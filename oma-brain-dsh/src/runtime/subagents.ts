import { snapshotSubagentDescriptor } from "@deepseek-ai/dsh-subagent";
import type { Context } from "@deepseek-ai/cordis";
import { object, type Payload } from "../json.ts";
import { SessionRuntime } from "./session.ts";
import { MAX_LIVE_SUBAGENT_THREADS, type SubagentBridge, type SubagentThread, type ToolExecutionBridge } from "./contracts.ts";

/** Child turns are awaited so the existing single-dispatch journal stays valid. */
export class Subagents {
  private readonly runtimes = new Map<string, SessionRuntime>();
  private lastActivity = Date.now();
  private reconciling?: Promise<void>;
  get busy(): boolean { return this.threads.some((thread) => thread.status === "running"); }
  get lastIdle(): number { return Math.max(this.parent.lastIdle, this.lastActivity); }
  private readonly unbind = new Map<string, () => void>();
  constructor(readonly parent: SessionRuntime, readonly bridge: SubagentBridge) {}

  get threads(): SubagentThread[] {
    return this.bridge.threads();
  }

  async restore(): Promise<void> {
    await this.bridge.restoreThreads?.();
    for (const thread of this.threads) {
      this.bridge.bindPersistence(thread.id);
      if (thread.status !== "running") continue;
      thread.status = "idle";
      const unbind = this.parent.config.bindThread?.(thread.id);
      if (unbind) this.unbind.set(thread.id, unbind);
      const child = await this.createRuntime(thread);
      this.runtimes.set(thread.id, child);
      await child.open();
      await child.flushLog();
      await this.bridge.emit({ kind: "idle", thread, completed: false });
    }
  }

  register(scope: Context): void {
    const config = this.parent.config.subagents;
    if (!config) return;
    const register = (name: string, description: string, properties: Payload,
      required: string[], execute: (args: Payload, signal: AbortSignal) => Promise<string>) => {
      scope.tools.register({
        name, description,
        parameters: { type: "object", properties, required, additionalProperties: false },
        output: { schema: { type: "string" }, render: (_args, value) => [{ type: "text", text: String(value) }] },
        isConcurrencySafe: () => name === "send_to_agent",
        execute: (args, exec) => execute(object(args), exec.signal),
      });
    };
    register("list_agents", `List predefined specialists and retained subagent threads. Idle threads retain history and count toward the ${MAX_LIVE_SUBAGENT_THREADS}-thread limit.`, {}, [], async () => JSON.stringify({
      agents: (config.agents ?? []).map(({ name, description }) => ({ name, description })),
      inline_agents: config.inlineAgents !== false,
      threads: this.threads.map(({ id, name, status }) => ({ id, name, status })),
    }));
    register("send_to_agent", "Delegate a task and wait for the subagent's final report, which is returned as this tool's result. Supply exactly one target: thread_id for a follow-up that keeps the thread's history, agent_name for a new predefined specialist, or name with system_prompt for a new inline agent. Inline agents inherit your model and sandbox tools. Subagents cannot delegate further.", {
      message: { type: "string", minLength: 1, description: "The complete task. The subagent sees nothing else from your conversation, so include every path, constraint and expected output it needs." },
      thread_id: { type: "string", minLength: 1, description: "Follow up on an existing thread from list_agents. Use instead of agent_name and name." },
      agent_name: { type: "string", minLength: 1, description: "Start a new thread from a predefined specialist listed by list_agents. Use instead of thread_id and name." },
      name: { type: "string", minLength: 1, description: "Name for a new inline agent. Requires system_prompt. Use instead of thread_id and agent_name." },
      system_prompt: { type: "string", description: "Role and standing instructions for a new inline agent created with name." },
    }, ["message"], (args, signal) => this.send(args, signal));
    register("archive_agent", "Archive an idle subagent thread to free capacity. Archived threads cannot receive follow-up messages.", {
      thread_id: { type: "string" },
    }, ["thread_id"], async (args) => {
      const thread = this.thread(args.thread_id);
      if (thread.status !== "idle") throw new Error("Only idle threads can be archived");
      thread.status = "terminated";
      await this.bridge.emit({ kind: "terminated", thread });
      await this.runtimes.get(thread.id)?.close();
      this.runtimes.delete(thread.id);
      this.unbind.get(thread.id)?.();
      this.unbind.delete(thread.id);
      return JSON.stringify({ thread_id: thread.id, status: thread.status });
    });
  }

  private thread(id: string): SubagentThread {
    const thread = this.threads.find((entry) => entry.id === id);
    if (!thread) throw new Error("Unknown subagent thread");
    return thread;
  }

  private async reconcileCapacity(signal: AbortSignal): Promise<void> {
    if (!this.reconciling && this.threads.filter((thread) => thread.status !== "terminated").length >= MAX_LIVE_SUBAGENT_THREADS && this.bridge.archivedThreads) {
      this.reconciling = this.retireArchived(signal).finally(() => { this.reconciling = undefined; });
    }
    await this.reconciling;
    signal.throwIfAborted();
  }

  private async retireArchived(signal: AbortSignal): Promise<void> {
    const candidates = this.threads.filter((thread) => thread.status === "idle").map((thread) => thread.id);
    const archived = await this.bridge.archivedThreads!(candidates, signal);
    for (const id of archived) {
      const thread = this.thread(id);
      if (thread.status !== "idle") continue;
      thread.status = "terminated";
      try {
        await this.runtimes.get(id)?.close();
      } catch (error) {
        thread.status = "idle";
        throw error;
      }
      this.runtimes.delete(id);
      this.unbind.get(id)?.();
      this.unbind.delete(id);
    }
  }

  async send(args: Payload, signal: AbortSignal): Promise<string> {
    signal.throwIfAborted();
    const config = this.parent.config.subagents;
    if (!config) throw new Error("Subagents are disabled");
    let thread: SubagentThread;
    if (args.thread_id) {
      if (args.agent_name || args.name || args.system_prompt !== undefined)
        throw new Error("Follow-up accepts only thread_id and message");
      thread = this.thread(args.thread_id);
    } else {
      await this.reconcileCapacity(signal);
      if (this.threads.filter((entry) => entry.status !== "terminated").length >= MAX_LIVE_SUBAGENT_THREADS)
        throw new Error(`A session can have at most ${MAX_LIVE_SUBAGENT_THREADS} live subagent threads; archive an idle thread first`);
      const definition = args.agent_name
        ? config.agents?.find((agent) => agent.name === args.agent_name) : undefined;
      if (args.agent_name && !definition) throw new Error("Unknown predefined agent");
      if (definition && (args.name || args.system_prompt !== undefined))
        throw new Error("Predefined agents cannot override name or system prompt");
      if (!definition && (config.inlineAgents === false || !args.name || typeof args.system_prompt !== "string"))
        throw new Error("Inline agents require name and system_prompt and must be enabled");
      thread = { id: `sthr_${crypto.randomUUID()}`, inline: !definition, agentSnapshot: definition?.agentSnapshot ? structuredClone(definition.agentSnapshot) : undefined,
        name: definition?.name ?? args.name,
        systemPrompt: definition?.systemPrompt ?? args.system_prompt,
        model: definition?.model ?? this.parent.config.model,
        tools: inheritedTools(definition?.tools, this.parent.config.tools), status: "idle" };
      this.threads.push(thread);
      this.bridge.bindPersistence(thread.id);
    }
    if (args.thread_id && thread.status === "idle") await this.bridge.validateThread?.(thread.id, signal);
    if (thread.status !== "idle") throw new Error("Subagent thread is running or archived");
    thread.status = "running";
    const child = this.runtimes.get(thread.id) ?? await this.openRuntime(thread, !args.thread_id);
    const cancel = () => child.agent.cancel({ kind: "user" });
    signal.addEventListener("abort", cancel, { once: true });
    let report = "";
    let completed = false;
    // Report text is accumulated only from this child's completed assistant messages.
    const previous = child.hooks.onEvent;
    child.hooks.onEvent = (event) => {
      previous(event);
      if (event.type === "assistant/message") {
        const data = event.data as Payload;
        const text = data.message.content.filter((block: Payload) => block.type === "text")
          .map((block: Payload) => block.text).join("\n");
        if (text) report = text;
      }
    };
    try {
      signal.throwIfAborted();
      await child.run(args.message, {
        beforeRun: async () => {
          thread.status = "running";
          await this.bridge.emit({ kind: "message-sent", thread, text: args.message });
          await this.bridge.emit({ kind: "running", thread });
        },
        afterRun: async (done) => { completed = done; },
      });
      signal.throwIfAborted();
      if (!completed) throw new Error("Subagent turn stopped before completion");
      await this.bridge.emit({ kind: "report-received", thread, text: report });
      return JSON.stringify({ thread_id: thread.id, agent_name: thread.name, report });
    } finally {
      child.hooks.onEvent = previous;
      signal.removeEventListener("abort", cancel);
      thread.status = "idle";
      this.lastActivity = Date.now();
      await this.bridge.emit({ kind: "idle", thread, completed });
    }
  }

  private async openRuntime(thread: SubagentThread, created: boolean): Promise<SessionRuntime> {
    let runtime: SessionRuntime | undefined;
    try {
      if (created) await this.bridge.emit({ kind: "created", thread });
      const unbind = this.parent.config.bindThread?.(thread.id);
      if (unbind) this.unbind.set(thread.id, unbind);
      runtime = await this.createRuntime(thread);
      this.runtimes.set(thread.id, runtime);
      await runtime.open();
      return runtime;
    } catch (error) {
      await this.failInitialization(thread, runtime, error);
      throw error;
    }
  }

  private async failInitialization(thread: SubagentThread, runtime: SessionRuntime | undefined, cause: unknown): Promise<void> {
    thread.status = "terminated";
    this.lastActivity = Date.now();
    this.runtimes.delete(thread.id);
    const unbind = this.unbind.get(thread.id);
    this.unbind.delete(thread.id);
    const failures: unknown[] = [];
    try { await this.bridge.emit({ kind: "terminated", thread }); }
    catch (error) { failures.push(error); }
    try { await runtime?.close(); }
    catch (error) { failures.push(error); }
    try { unbind?.(); }
    catch (error) { failures.push(error); }
    if (failures.length) throw new AggregateError([cause, ...failures], "Subagent initialization failed and cleanup was incomplete", { cause });
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
      descriptor: snapshotSubagentDescriptor({
        mode: "continuable", provider: thread.inline ? "oma-inline" : "oma-specialist", label: thread.name,
        agentProvider: config.provider, agentModel: thread.model, persona: thread.systemPrompt,
        ...(thread.tools ? { toolFilter: { allow: thread.tools } } : {}),
      }),
      toolPermissions: { ...config.toolPermissions },
      refreshToolPermissions: config.refreshToolPermissions
        ? (signal) => config.refreshToolPermissions!(signal, thread.id) : undefined,
      externalMcpConfig: mcp ? mcp.config : config.externalMcpConfig,
      externalMcpFetch: mcp?.fetcher ?? config.externalMcpFetch,
      subagents: undefined, bindThread: undefined, setup: undefined,
      systemPrompt: config.subagentSystemPrompt?.(thread)
        ?? `${config.systemPrompt}\n\nYou are subagent ${thread.name}. Your conversation is isolated. Delegation tools are unavailable. Report your findings to the parent.\nSpecialist instructions:\n${thread.systemPrompt}`,
    }, {
      onExternalMcpAuthenticationFailure: (serverName, phase) => this.parent.hooks.onExternalMcpAuthenticationFailure?.(serverName, phase) ?? Promise.resolve(),
      onContextCompacted: tokens => this.bridge.onContextCompacted?.(thread.id, tokens) ?? Promise.resolve(),
      onEvent: (event) => this.bridge.onEvent(thread.id, event),
      onStream: (frame) => this.bridge.onStream(thread.id, frame),
      stop: () => this.parent.hooks.stop(),
    }, bridge, this.parent);
    return child;
  }

  async flush(): Promise<void> {
    for (const child of this.runtimes.values()) if (child.handle) await child.flushLog();
  }
  interrupt(id?: string): void {
    if (id && !this.threads.some((thread) => thread.id === id && thread.status !== "terminated"))
      throw new Error("Unknown or archived subagent thread");
    for (const [threadId, child] of this.runtimes)
      if (!id || threadId === id) child.handle?.agent.cancel({ kind: "user" });
  }
  cancelForShutdown(): void { for (const child of this.runtimes.values()) child.cancelForShutdown(); }
  stop(): void { for (const child of this.runtimes.values()) child.stop(); }
  async close(): Promise<void> {
    await Promise.all([...this.runtimes.values()].map((child) => child.close()));
    for (const unbind of this.unbind.values()) unbind();
    this.unbind.clear();
  }
}

function inheritedTools(own: string[] | undefined, parent: string[] | undefined): string[] | undefined {
  if (!own) return parent;
  return parent ? own.filter((name) => parent.includes(name)) : own;
}
