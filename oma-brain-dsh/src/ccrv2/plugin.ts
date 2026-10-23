import { Context, Service } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { CcrPersistence } from "./persistence.ts";
import { CcrSession } from "./session.ts";
import type { CcrBackend } from "./contracts.ts";
import type { SessionRuntimeConfig } from "../runtime/contracts.ts";
import type { CcrHeartbeatOptions, CcrHeartbeatHealth, CcrLeaseCheck, CcrLeaseFailure } from "./heartbeat.ts";
import type { CcrActivityOptions } from "./activity.ts";

export const name = "ccrv2-bridge";
export interface Config {
  backend: CcrBackend;
  localRoot?: string;
  keepAlive?: CcrActivityOptions;
  heartbeat?: CcrHeartbeatOptions & {
    bind(id: string): CcrLeaseCheck;
    onLeaseLost(session: CcrSession, failure: CcrLeaseFailure): void;
    onHealth?(id: string, health: CcrHeartbeatHealth): void;
  };
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    ccrv2: CcrBridge;
  }
}

/** Owns protocol bridges and heartbeats; the host claims and releases leases. */
export class CcrBridge extends Service {
  static inject = [
    "attachments",
    "llm",
    "sessions",
    "sessionPersistence",
    "agents",
    "agentLoop",
    "tools",
    "systemPrompt",
  ];
  private readonly sessions = new Map<string, CcrSession>();

  constructor(ctx: Context, readonly config: Config) {
    super(ctx, "ccrv2");
    ctx.effect(() => async () => {
      for (const session of this.sessions.values()) session.stop();
      await Promise.allSettled([...this.sessions.values()].map((s) => s.close()));
    });
  }

  createSession(config: SessionRuntimeConfig): CcrSession {
    if (this.sessions.has(config.id))
      throw new Error("CCRv2 session already exists in this bridge");
    const heartbeat = this.config.heartbeat;
    const session: CcrSession = new CcrSession(this.ctx, this.config.backend, config, heartbeat ? {
      ...heartbeat,
      check: heartbeat.bind(config.id),
      onLeaseLost: (failure) => heartbeat.onLeaseLost(session, failure),
      onHealth: (health) => heartbeat.onHealth?.(config.id, health),
    } : undefined, this.config.keepAlive);
    this.sessions.set(config.id, session);
    return session;
  }

  /** Call after closing the bridge and releasing the host's worker lease. */
  forget(id: string): void {
    (this.ctx.sessionPersistence as CcrPersistence).forget(SessionId(id));
    this.sessions.delete(id);
  }
}

export async function apply(ctx: Context, config: Config): Promise<void> {
  await ctx.plugin(CcrPersistence, { backend: config.backend, localRoot: config.localRoot });
  await ctx.plugin(CcrBridge, config);
}
