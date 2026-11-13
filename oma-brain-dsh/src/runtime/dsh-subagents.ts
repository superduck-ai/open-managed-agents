import { SHUTDOWN_CANCEL } from "./contracts.ts";
import { AsyncLocalStorage } from "node:async_hooks";
import Loader from "@deepseek-ai/cordis-plugin-loader";
import AgentPresets from "@deepseek-ai/dsh-agent-preset-registry";
import AgentPreset from "@deepseek-ai/dsh-agent-preset";
import type { Context } from "@deepseek-ai/cordis";
import type { Agent } from "@deepseek-ai/dsh-agent";
import DshSubagentRuntime, { foldSubagentDescriptor } from "@deepseek-ai/dsh-subagent";
import * as Spawn from "@deepseek-ai/dsh-subagent-spawn-in-process";
import * as Delegation from "@deepseek-ai/dsh-tool-subagent";
import * as Control from "@deepseek-ai/dsh-tool-subagent-control";
import SessionQuery from "@deepseek-ai/dsh-session-query-sqlite";
import { appendPluginRecord, pluginRecordOf, SessionId, type Session, type SessionEvent } from "@deepseek-ai/dsh-session";
import type {} from "@deepseek-ai/dsh-working-directory";
import { MAX_LIVE_SUBAGENT_THREADS, type SubagentBridge, type SubagentDefinition, type SubagentThread } from "./contracts.ts";
import type { SessionRuntime } from "./session.ts";

declare module "@deepseek-ai/cordis" {
  interface Context { omaSubagentDefinition: SubagentDefinition; }
}

interface DshThreadConfig {
  version: 1;
  model?: string;
  systemPrompt: string;
  name?: string;
  inline?: boolean;
  tools?: string[];
}

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/subagent-config": DshThreadConfig;
  }
}

export function dshThreadConfig(events: readonly SessionEvent[]): DshThreadConfig | undefined {
  const record = events.map(pluginRecordOf).find(record => record?.type === "plugin:oma/subagent-config");
  if (!record) return;
  const data = record.data;
  if (!data || typeof data !== "object" || !("version" in data) || data.version !== 1
    || !("systemPrompt" in data) || typeof data.systemPrompt !== "string"
    || ("model" in data && typeof data.model !== "string")
    || ("name" in data && typeof data.name !== "string")
    || ("inline" in data && typeof data.inline !== "boolean")
    || ("tools" in data && (!Array.isArray(data.tools) || data.tools.some(tool => typeof tool !== "string"))))
    throw new Error("Invalid OMA native subagent configuration");
  return { version: 1, systemPrompt: data.systemPrompt,
    ...("model" in data ? { model: data.model as string } : {}),
    ...("name" in data ? { name: data.name as string } : {}), ...("inline" in data ? { inline: data.inline as boolean } : {}), ...("tools" in data ? { tools: data.tools as string[] } : {}) };
}

export async function mountDshSubagents(ctx: Context): Promise<void> {
  await ctx.plugin(Loader);
  ctx.get("loader")!.builtins.omaSubagent = {
    apply(scope: Context, config: SubagentDefinition) {
      scope.isolate("omaSubagentDefinition").provide("omaSubagentDefinition", config);
    },
  };
  await ctx.plugin(AgentPresets, { default: "oma" });
  await ctx.plugin({
    name: "oma-working-directory",
    apply(scope: Context) {
      scope.provide("workingDirectory", {
        get(session: Session) {
          if (!session.header.cwd) throw new Error("OMA session has no sandbox working directory");
          return session.header.cwd;
        },
        async ensure(agent: Agent, signal?: AbortSignal) {
          signal?.throwIfAborted();
          return this.get(agent.session);
        },
        async set(agent: Agent, path: string, signal?: AbortSignal) {
          signal?.throwIfAborted();
          if (path !== this.get(agent.session)) throw new Error("OMA subagents must share the configured sandbox working directory");
          return path;
        },
      });
    },
  });
  await ctx.plugin(SessionQuery, { path: ":memory:", openAt: "never" });
  await ctx.plugin(DshSubagentRuntime, { maxActiveSubagents: MAX_LIVE_SUBAGENT_THREADS, maxDepth: 1 });
}

export class DshSubagents {
  recovering = false;
  lastIdle = Date.now();
  private readonly manager: DshSubagentRuntime;
  private readonly runtimes = new Map<string, SessionRuntime>();
  private readonly provider: string;
  private pending: Promise<void> = Promise.resolve();
  private readonly inlineSystems = new AsyncLocalStorage<string>();
  private readonly disposers: (() => void)[] = [];

  constructor(
    readonly parent: SessionRuntime,
    readonly bridge: SubagentBridge,
    createRuntime: (thread: SubagentThread) => Promise<SessionRuntime>,
  ) {
    this.manager = parent.ctx.get("subagents")!;
    this.provider = `oma-spawn:${parent.config.id}`;
    this.disposers.push(parent.ctx.on("session/created", session => {
      if (session.header.parentSession === parent.config.id && session.header.origin === "subagent") bridge.bindPersistence(session.id);
    }));
    this.disposers.push(parent.ctx.on("agent/created", async ({ agent, signal }) => {
      if (this.recovering || agent.session.header.parentSession !== parent.config.id) return;
      const descriptor = foldSubagentDescriptor(agent.session.snapshotEvents());
      if (!descriptor || descriptor.mode !== "continuable") return;
      let thread = bridge.threads().find(thread => thread.id === agent.id);
      const created = !thread;
      if (!thread) {
        if (descriptor.provider !== this.provider) return;
        if (bridge.threads().filter(thread => thread.status !== "terminated").length >= MAX_LIVE_SUBAGENT_THREADS)
          throw new Error(`A session can have at most ${MAX_LIVE_SUBAGENT_THREADS} live subagent threads; archive an idle thread through the session API first`);
        const definition = parent.ctx.get("agentPresets")!.serviceFor(agent, "omaSubagentDefinition");
        thread = { id: agent.id, name: definition?.name ?? descriptor.label, inline: !definition,
          systemPrompt: definition?.systemPrompt ?? this.inlineSystems.getStore() ?? "", model: definition?.model ?? descriptor.agentModel ?? parent.config.model,
          tools: definition ? [...(definition.tools ?? [])] : parent.config.tools, status: "idle" };
        bridge.threads().push(thread);
      }
      const current = thread;
      try {
        if (created) await bridge.emit({ kind: "created", thread });
        const unbind = parent.config.bindThread?.(thread.id);
        agent.ctx.effect(() => () => { this.runtimes.delete(current.id); unbind?.(); });
        signal?.throwIfAborted();
        if (!dshThreadConfig(agent.session.snapshotEvents())) appendPluginRecord(agent.session, "plugin:oma/subagent-config", {
          version: 1, name: thread.name, inline: thread.inline, systemPrompt: thread.systemPrompt, model: thread.model,
          ...(thread.tools ? { tools: [...thread.tools] } : {}),
        });
        const runtime = await createRuntime(thread);
        await runtime.attach(agent);
        this.runtimes.set(thread.id, runtime);
        await runtime.flushLog();
      } catch (error) {
        thread.status = created ? "terminated" : "idle";
        try {
          if (created) await bridge.emit({ kind: "terminated", thread });
        } catch { parent.hooks.stop(); }
        finally { throw error; }
      }
    }));
    this.disposers.push(parent.ctx.on("agent/inbox/inserted", ({ agent, message }) => {
      const thread = bridge.threads().find(thread => thread.id === agent.id);
      if (!thread) return;
      const initial = message.source.kind === "user" && !agent.session.snapshotEvents().some(event => event.type === "turn/start");
      const followup = message.source.kind === "agent-message" && message.source.senderSessionId === parent.config.id;
      if (!initial && !followup) return;
      thread.status = "running";
      this.pending = this.pending.then(async () => {
        const text = message.content.filter(block => block.type === "text").map(block => block.text).join("\n");
        await bridge.emit({ kind: "message-sent", thread, text });
        await bridge.emit({ kind: "running", thread });
      }).catch(() => parent.hooks.stop());
    }));
    this.disposers.push(parent.ctx.on("subagent/end", info => {
      const thread = bridge.threads().find(thread => thread.id === info.id);
      if (!thread) return;
      this.pending = this.pending.then(async () => {
        const report = info.lastAssistantMessage?.filter(block => block.type === "text").map(block => block.text).join("\n") ?? "";
        if (info.stopReason === "completed") await bridge.emit({ kind: "report-received", thread, text: report });
        thread.status = "idle";
        this.lastIdle = Date.now();
        await bridge.emit({ kind: "idle", thread, completed: info.stopReason === "completed" });
      }).catch(() => parent.hooks.stop());
    }));
  }

  async register(scope: Context, reconcileCapacity: (signal: AbortSignal) => Promise<void>): Promise<void> {
    const config = this.parent.config.subagents!;
    const presetIds: string[] = [];
    for (const definition of config.agents ?? []) {
      const id = `${this.parent.config.id}/${definition.name}`;
      presetIds.push(id);
      await scope.plugin(AgentPreset, {
        id, name: definition.name, description: definition.description,
        plugins: [{ name: "cordis:omaSubagent", config: definition }],
      });
    }
    const provider = this.provider;
    await scope.plugin({
      name: "oma-delegation",
      inject: ["tools", "subagents", "systemPrompt", "sessionProjections", "agentPresets"],
      async apply(ctx: Context) {
        Spawn.apply(ctx, { providerName: provider });
        await Delegation.apply(ctx, { provider, toolName: "subagent", maxDepth: 1,
          ...(presetIds.length ? { allowedAgentPresets: presetIds } : {}),
        });
      },
    });
    await scope.plugin(Control);
    this.parent.delegationPrompt = [
      ...(config.inlineAgents === false ? ["Choose an agent_preset for every subagent call. General workers are disabled."] : []),
      "Use send_message with agent_id to steer a running child or continue an idle child. Use list_agents to inspect children and interrupt_agent to stop work. Reports arrive separately after the initial child id. Archiving is managed through the OMA session API.",
    ].join("\n");
    scope.on("tools/execute", async (exec, next) => {
      await this.pending;
      if (exec.name === "subagent") {
        if (config.inlineAgents === false && !(exec.arguments as { agent_preset?: string }).agent_preset)
          throw new Error("A predefined agent_preset is required; inline subagents are disabled");
        await reconcileCapacity(exec.signal);
      }
      if (exec.name === "send_message" || exec.name === "interrupt_agent") {
        const args = exec.arguments as { agent_id: string };
        const thread = this.bridge.threads().find(thread => thread.id === args.agent_id);
        if (!thread || thread.status === "terminated") throw new Error("Unknown or archived subagent thread");
        await this.bridge.validateThread?.(thread.id, exec.signal, true);
      }
      const args = exec.arguments as { agent_preset?: string; prompt?: unknown };
      const result = exec.name === "subagent" && args.agent_preset === undefined && typeof args.prompt === "string"
        ? await this.inlineSystems.run(args.prompt, () => next()) : await next();
      await this.pending;
      return result;
    });
  }

  async wait(): Promise<boolean> {
    const joined = await this.manager.waitForChildren(this.parent.agent);
    await this.pending;
    return joined;
  }

  async flush(): Promise<void> {
    for (const runtime of this.runtimes.values()) await runtime.flushLog();
  }

  settleEvents(): Promise<void> { return this.pending; }

  interrupt(id?: string, shutdown = false): void {
    if (!this.parent.handle) return;
    const agent = this.parent.agent;
    if (this.parent.ctx.agents.get(agent.id) !== agent) return;
    for (const thread of this.bridge.threads()) if (!id || thread.id === id)
      if (shutdown) this.parent.ctx.agents.get(SessionId(thread.id))?.cancel(SHUTDOWN_CANCEL);
      else this.manager.interrupt(SessionId(thread.id), { kind: "ancestor", agent });
  }

  async close(): Promise<void> {
    try {
      if (this.parent.handle) await this.manager.drainDescendants([this.parent.agent]);
      await this.pending;
    } finally { for (const dispose of this.disposers.splice(0)) dispose(); }
  }
}
