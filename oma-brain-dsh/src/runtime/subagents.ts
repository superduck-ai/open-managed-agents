import type { Context } from "@deepseek-ai/cordis";
import { object, type Payload } from "../json.ts";
import { SessionRuntime } from "./session.ts";
import type { SubagentBridge, SubagentThread, ToolExecutionBridge } from "./contracts.ts";

/** Child turns are awaited so the existing single-dispatch journal stays valid. */
export class Subagents {
  private readonly runtimes = new Map<string, SessionRuntime>();
  private lastActivity = Date.now();
  get busy(): boolean { return this.threads.some((thread) => thread.status === "running"); }
  get lastIdle(): number { return Math.max(this.parent.lastIdle, this.lastActivity); }
  private readonly unbind = new Map<string, () => void>();
  constructor(readonly parent: SessionRuntime, readonly bridge: SubagentBridge) {}

  get threads(): SubagentThread[] {
    return this.bridge.threads();
  }

  async restore(): Promise<void> {
    for (const thread of this.threads) {
      this.bridge.bindPersistence(thread.id);
      if (thread.status !== "running") continue;
      thread.status = "idle";
      const unbind = this.parent.config.bindThread?.(thread.id);
      if (unbind) this.unbind.set(thread.id, unbind);
      const child = this.createRuntime(thread);
      this.runtimes.set(thread.id, child);
      await child.open();
      await child.flushLog();
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
    register("list_agents", "List predefined specialists and retained subagent threads. Idle threads retain history and count toward the 25-thread limit.", {}, [], async () => JSON.stringify({
      agents: (config.agents ?? []).map(({ name, description }) => ({ name, description })),
      inline_agents: config.inlineAgents !== false,
      threads: this.threads.map(({ id, name, status }) => ({ id, name, status })),
    }));
    register("send_to_agent", "Delegate a task and wait for its report. Supply thread_id for a follow-up retaining history, agent_name for a new predefined specialist, or name and system_prompt for a new inline agent. Inline agents inherit your model and sandbox tools. Only one delegation level is supported.", {
      message: { type: "string", minLength: 1 },
      thread_id: { type: "string", minLength: 1 },
      agent_name: { type: "string", minLength: 1 },
      name: { type: "string", minLength: 1 },
      system_prompt: { type: "string" },
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
      if (this.threads.filter((entry) => entry.status !== "terminated").length >= 25)
        throw new Error("A session can have at most 25 live subagent threads; archive an idle thread first");
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
        tools: definition?.tools ?? this.parent.config.tools, status: "idle" };
      this.threads.push(thread);
      this.bridge.bindPersistence(thread.id);
      await this.bridge.emit({ kind: "created", thread });
    }
    if (thread.status !== "idle") throw new Error("Subagent thread is running or archived");
    thread.status = "running";
    let runtime = this.runtimes.get(thread.id);
    if (!runtime) {
      const unbind = this.parent.config.bindThread?.(thread.id);
      if (unbind) this.unbind.set(thread.id, unbind);
      runtime = this.createRuntime(thread);
      this.runtimes.set(thread.id, runtime);
      try {
        await runtime.open();
      } catch (error) {
        this.runtimes.delete(thread.id);
        this.unbind.get(thread.id)?.();
        this.unbind.delete(thread.id);
        await runtime.close();
        thread.status = "idle";
        throw error;
      }
    }
    const child = runtime;
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
      if (!completed || this.parent.bridge.blocked()) throw new Error("Subagent turn stopped before completion");
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

  private createRuntime(thread: SubagentThread): SessionRuntime {
    const config = this.parent.config;
    let child: SessionRuntime;
    const execution = this.bridge.toolExecution(thread.id);
    const bridge: ToolExecutionBridge = {
      blocked: () => execution.blocked(),
      approve: (request, signal) => execution.approve(request, signal),
      recordApproved: (call) => execution.recordApproved(call),
      flushLog: async () => { await child.flushLog(); await execution.flushLog(); },
      recordDispatch: (callId) => execution.recordDispatch(callId),
      recordUnknown: (callId) => execution.recordUnknown(callId),
      recordResult: (outcome) => execution.recordResult(outcome),
      stop: () => execution.stop(),
    };
    child = new SessionRuntime(this.parent.ctx, {
      ...config, id: thread.id, model: thread.model, tools: thread.tools,
      subagents: undefined, bindThread: undefined, setup: undefined,
      systemPrompt: `${config.subagentSystemPrompt ?? config.systemPrompt}\n\nYou are subagent ${thread.name}. Your conversation is isolated. Delegation tools are unavailable. Report your findings to the parent.\nSpecialist instructions:\n${thread.systemPrompt}`,
    }, {
      beforeRequest: async () => { await child.flushLog(); await this.bridge.beforeRequest(); },
      onEvent: (event) => this.bridge.onEvent(thread.id, event),
      onStream: (frame) => this.bridge.onStream(thread.id, frame),
      stop: () => execution.stop(),
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
  stop(): void { for (const child of this.runtimes.values()) child.stop(); }
  async close(): Promise<void> {
    await Promise.all([...this.runtimes.values()].map((child) => child.close()));
    for (const unbind of this.unbind.values()) unbind();
    this.unbind.clear();
  }
}
