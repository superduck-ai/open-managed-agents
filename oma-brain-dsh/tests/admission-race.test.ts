import { expect, test } from "bun:test";
import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import type { CcrClient, CcrSession } from "../src/ccrv2/index.ts";
import type { ClaimClient, ClaimedTask, ReleaseReason } from "../src/claim.ts";
import { startWorkers } from "../src/worker.ts";
import { eventually } from "./backend.ts";

const sessionId = "cse_admission_race";
const base = "http://oma.test";

type Deferred<T> = ReturnType<typeof Promise.withResolvers<T>>;

class FencedBackend {
  epoch = 1;
  readonly contextGates = new Map<number, Deferred<void>>();

  gate(epoch: number): Deferred<void> {
    let gate = this.contextGates.get(epoch);
    if (!gate) {
      gate = Promise.withResolvers<void>();
      this.contextGates.set(epoch, gate);
    }
    return gate;
  }

  fetch: typeof fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input));
    const epoch = Number(String(new Headers(init?.headers).get("Authorization")).replace("Bearer ingress-", ""));
    if (url.pathname.endsWith("/worker/register")) {
      if (epoch !== this.epoch) return new Response("{}", { status: 409 });
      return Response.json({ worker_epoch: String(epoch) });
    }
    if (url.pathname === `/v2/sessions/${sessionId}`) {
      await this.gate(epoch).promise;
      return Response.json({ session_context: {
        models: { "test-model": { id: "test-model", api_backend: "messages" } },
        model: "test-model", cwd: "/workspace", system_prompt: "test", runtime_tools: ["Bash"], mcp_config: {},
      } });
    }
    return new Response("{}", { status: 404 });
  }) as typeof fetch;
}

class QueuedClaims implements ClaimClient {
  readonly pending: Deferred<unknown>[] = [];
  readonly releases: { task: ClaimedTask; reason: ReleaseReason }[] = [];
  private waiters: (() => void)[] = [];

  async claim(blockMs: number, signal: AbortSignal): Promise<unknown | undefined> {
    if (blockMs === 0) return undefined;
    const next = Promise.withResolvers<unknown>();
    this.pending.push(next);
    for (const wake of this.waiters.splice(0)) wake();
    signal.addEventListener("abort", () => next.resolve(undefined), { once: true });
    return next.promise;
  }

  async waitForPolls(count: number): Promise<void> {
    while (this.pending.length < count) await new Promise<void>((resolve) => this.waiters.push(resolve));
  }

  async release(task: ClaimedTask, reason: ReleaseReason): Promise<void> {
    this.releases.push({ task, reason });
  }
}

interface FakeSession extends CcrSession {
  readonly epoch: number;
  closed: boolean;
}

function harness() {
  const backend = new FencedBackend();
  const api = new OmaApi(base, backend.fetch);
  const claims = new QueuedClaims();
  const created: FakeSession[] = [];
  const disposers: (() => Promise<void> | void)[] = [];
  const ccrv2 = {
    forget: () => {},
    createSession: (config: { id: string }) => {
      const abort = new AbortController();
      const epoch = api.assignment(config.id).worker_epoch;
      const session = {
        id: config.id, epoch, closed: false, busy: false, lastIdle: Date.now(), abort,
        stop: () => abort.abort(),
        close: async () => { session.closed = true; abort.abort(); },
        open: async () => {
          if (api.assignment(config.id).worker_epoch !== backend.epoch) throw new Error("HTTP 409 epoch mismatch");
        },
        receive: async () => {},
        drain: async () => true,
        interruptForShutdown: async () => {},
      } as unknown as FakeSession;
      created.push(session);
      return session;
    },
  };
  const ctx = {
    ccrv2,
    on: () => () => {},
    effect: (setup: () => () => Promise<void> | void) => { disposers.push(setup()); },
  } as unknown as Context;
  const client = {
    async *inbound(id: string, signal: AbortSignal) {
      if (api.assignment(id).worker_epoch !== backend.epoch) throw new Error("HTTP 409 epoch mismatch");
      await new Promise<void>((resolve) => signal.addEventListener("abort", () => resolve(), { once: true }));
    },
  } as unknown as CcrClient;
  return { backend, api, claims, created, ctx, client, shutdown: async () => { for (const dispose of disposers) await dispose(); } };
}

const task = (epoch: number) => ({ session_id: sessionId, worker_epoch: epoch, ingress_token: `ingress-${epoch}`, model_token: "model" });

test.failing("concurrent admissions of one session in one process leave no unclosed or uncredentialed session", async () => {
  const { backend, api, claims, created, ctx, client, shutdown } = harness();
  await startWorkers(ctx, api, client, claims, { maxSessions: 2, claimConcurrency: 2 });
  await claims.waitForPolls(2);

  backend.epoch = 2;
  claims.pending[0]!.resolve(task(2));
  await eventually(() => backend.contextGates.has(2), 2_000);

  backend.epoch = 3;
  claims.pending[1]!.resolve(task(3));
  await eventually(() => backend.contextGates.has(3), 2_000);
  backend.gate(3).resolve();
  await eventually(() => created.some((session) => session.epoch === 3), 2_000);

  backend.gate(2).resolve();
  await eventually(() => created.some((session) => session.epoch === 2), 2_000);
  await eventually(() => claims.releases.some(({ task }) => task.worker_epoch === 2), 2_000);

  const current = created.find((session) => session.epoch === 3)!;
  const stale = created.find((session) => session.epoch === 2)!;
  const observed = {
    staleStopped: stale.abort.signal.aborted,
    currentRunning: !current.abort.signal.aborted,
    currentCredentialEpoch: api.assignments.get(sessionId)?.worker_epoch,
    shutdownCompleted: false,
    unclosedAfterShutdown: [] as number[],
  };
  observed.shutdownCompleted = await Promise.race([shutdown().then(() => true), Bun.sleep(1_000).then(() => false)]);
  observed.unclosedAfterShutdown = created.filter((session) => !session.closed).map((session) => session.epoch);
  expect(observed).toEqual({ staleStopped: true, currentRunning: true, currentCredentialEpoch: 3, shutdownCompleted: true, unclosedAfterShutdown: [] });
});
