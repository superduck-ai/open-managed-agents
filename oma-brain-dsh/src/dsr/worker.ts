import type {Context} from "@deepseek-ai/cordis";
import * as JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import {OmaApi} from "../api.ts";
import {checkWritableDirectory} from "../storage.ts";
import {mountKernel} from "../kernel.ts";
import {mountDshSubagents} from "../runtime/dsh-subagents.ts";
import {SessionReplication} from "../runtime/replication.ts";
import {omaRuntimeConfig} from "../oma-session.ts";
import {mountTelemetry} from "../observability/index.ts";
import {workerDiagnostic} from "../observability/diagnostics.ts";
import {ConfigurationError} from "../config.ts";
import {brainMetrics} from "../telemetry.ts";
import {DsrClient} from "./client.ts";
import {DsrClaims} from "./claims.ts";
import {DsrInputStream} from "./input-stream.ts";
import {DsrSession} from "./session.ts";
import {DsrLease} from "./lease.ts";
import {dsrAssignment} from "./assignment.ts";
import {dsrTranscriptPort} from "./replication.ts";
import {createDsrTransport} from "./transport.ts";
import {delay, DsrError} from "./http.ts";
import type {Claim} from "./wire.ts";

export const dsrRuntimeServices = ["attachments", "llm", "tokenMeter", "sessions", "sessionPersistence", "sessionReplication", "agents", "agentLoop", "tools", "systemPrompt"];

export interface DsrWorkerOptions {
    maxSessions: number;
    claimConcurrency: number;
    shutdownDrainMs: number;
}

declare module "@deepseek-ai/cordis" {
    interface Context {
        dsrContinuation: boolean;
    }

    interface Events {
        "oma/shutdown-drain"(): Promise<void>;
    }
}

export async function mountDsr(ctx: Context, api: OmaApi, localRoot: string): Promise<Map<string, DsrClient>> {
    await checkWritableDirectory(localRoot, "OMA_BRAIN_TRANSCRIPT_DIR");
    await mountKernel(ctx, api);
    ctx.provide("dsrContinuation", true);
    await ctx.plugin(JsonlPersistence, {root: localRoot, compression: "none"});
    const clients = new Map<string, DsrClient>();
    await ctx.plugin(SessionReplication, {
        remote: dsrTranscriptPort(id => {
            const client = clients.get(id);
            if (!client) throw new Error("DSR transcript owner is unavailable");
            return client;
        })
    });
    await mountDshSubagents(ctx);
    await mountTelemetry(ctx, api);
    return clients;
}

export async function startDsrWorkers(ctx: Context, api: OmaApi, clients: Map<string, DsrClient>, claims: DsrClaims, options: DsrWorkerOptions): Promise<void> {
    const abort = new AbortController();
    const owners = new Map<string, DsrOwner>();
    const work = new Set<Promise<void>>();
    let reserved = 0;
    const watch = (task: Promise<void>) => {
        work.add(task);
        void task.catch(() => workerDiagnostic("warn", "dsr_owner_failed", {})).finally(() => work.delete(task));
    };
    const admit = async (claim: Claim) => {
        const previous = owners.get(claim.code_session_id);
        if (previous) await previous.close();
        const client = new DsrClient(createDsrTransport(api), claim);
        clients.set(claim.code_session_id, client);
        const owner = new DsrOwner(ctx, api, client, claims, options.shutdownDrainMs);
        owners.set(claim.code_session_id, owner);
        try {
            await owner.run();
        } finally {
            if (owners.get(claim.code_session_id) === owner) {
                owners.delete(claim.code_session_id);
                if (clients.get(claim.code_session_id) === client) clients.delete(claim.code_session_id);
                ctx.sessionReplication.forget(claim.code_session_id);
                api.assignments.delete(claim.code_session_id);
                api.modelResponses.delete(claim.code_session_id);
                api.modelRequestIDs.delete(claim.code_session_id);
                await api.telemetry?.close(claim.code_session_id);
            }
        }
    };
    const poll = async () => {
        while (!abort.signal.aborted) {
            if (owners.size + reserved >= options.maxSessions) {
                await delay(claims.transport, 500, abort.signal);
                continue;
            }
            reserved++;
            try {
                const claim = await claims.claim(25000, abort.signal);
                if (claim) watch(admit(claim));
            } catch (error) {
                if (abort.signal.aborted) return;
                if (error instanceof DsrError && [400, 401, 403].includes(error.status)) {
                    abort.abort(error);
                    throw error;
                }
                await delay(claims.transport, 1000, abort.signal);
            } finally {
                reserved--;
            }
        }
    };
    const shutdown = async () => {
        abort.abort();
        await Promise.allSettled([...owners.values()].map(owner => owner.close()));
        await Promise.allSettled([...work]);
    };
    ctx.on("oma/shutdown-drain", shutdown);
    ctx.effect(() => shutdown);
    await claims.claim(0, abort.signal).then(claim => {
        if (claim) watch(admit(claim));
    }).catch(error => {
        if (error instanceof DsrError && [400, 401, 403].includes(error.status)) throw new ConfigurationError(`Brain claim rejected with HTTP ${error.status}`);
        throw error;
    });
    for (let i = 0; i < options.claimConcurrency; i++) watch(poll());
    console.info("oma-brain-dsh ready");
    workerDiagnostic("info", "dsr_brain_ready", {
        max_sessions: options.maxSessions,
        claim_concurrency: options.claimConcurrency
    });
}

export class DsrOwner {
    session?: DsrSession;
    private lease?: DsrLease;
    private readonly stream = new AbortController();
    private tasks: Promise<void> = Promise.resolve();
    private controls: Promise<void> = Promise.resolve();
    private readonly unsettled = new Set<string>();
    private closing?: Promise<void>;
    private settled = Promise.withResolvers<void>();
    private reason: "idle" | "shutdown" | "permanent" | "admission_failed" = "admission_failed";
    private failure?: unknown;
    private stage = "register";

    constructor(readonly ctx: Context, readonly api: OmaApi, readonly client: DsrClient, readonly claims: DsrClaims, readonly drainMs: number) {
    }

    async run(): Promise<void> {
        let timer: ReturnType<typeof setInterval> | undefined;
        let progress: ReturnType<typeof setInterval> | undefined;
        try {
            const remaining = Date.parse(this.client.claim.claim_expires_at) - Date.now();
            if (remaining <= 0) throw new DsrError(0, "claim_expired");
            const registration = await this.client.register(AbortSignal.timeout(remaining));
            this.lease = new DsrLease(this.client, registration, error => this.fail(error));
            this.lease.start();
            this.stage = "context";
            const assignment = dsrAssignment(this.api, this.client.claim, registration, await this.client.context(undefined, this.stream.signal));
            this.api.assignments.set(assignment.session_id, assignment);
            this.session = new DsrSession(this.ctx, this.client, omaRuntimeConfig(this.ctx, this.api, assignment, (thread, signal) => this.client.context(thread, signal)));
            brainMetrics().sessionsActive.add(1);
            this.stage = "open";
            await this.session.open();
            this.stage = "inputs";
            this.reason = "shutdown";
            timer = setInterval(() => {
                if (this.session && !this.session.busy && Date.now() - this.session.lastIdle > 60000) {
                    this.reason = "idle";
                    this.stream.abort();
                }
            }, 1000);
            let reporting = false;
            progress = setInterval(() => {
                if (reporting) return;
                reporting = true;
                void this.reportReceived().catch(error => this.fail(error)).finally(() => {
                    reporting = false;
                });
            }, 10000);
            for await (const input of new DsrInputStream(this.client).read(this.stream.signal)) {
                this.unsettled.add(input.id);
                const received = this.client.delivery([{id: input.id, status: "received"}], this.stream.signal);
                void received.catch(error => this.fail(error));
                if (input.lane === "control") {
                    this.controls = this.controls.then(async () => {
                        await received;
                        await this.session!.control(input, input.transportSequence);
                        this.unsettled.delete(input.id);
                    });
                    void this.controls.catch(error => this.fail(error));
                } else {
                    this.tasks = this.tasks.then(async () => {
                        await received;
                        if (this.session!.suspended) return;
                        await this.session!.receive(input);
                        if (this.session!.suspended) {
                            this.reason = "idle";
                            this.stream.abort();
                        } else this.unsettled.delete(input.id);
                    });
                    void this.tasks.catch(error => this.fail(error));
                }
            }
        } catch (error) {
            if (!this.stream.signal.aborted) this.fail(error);
        } finally {
            clearInterval(timer);
            clearInterval(progress);
            try {
                await Promise.allSettled([this.tasks, this.controls]);
                await this.session?.close();
            } finally {
                if (this.session) brainMetrics().sessionsActive.add(-1);
                await this.lease?.close();
                try {
                    const rejected = this.failure instanceof DsrError && this.failure.status >= 400 && this.failure.status < 500;
                    if (!rejected) await this.claims.release(this.client.claim, this.reason, AbortSignal.timeout(15000)).catch(() => {
                    });
                } finally {
                    this.settled.resolve();
                }
            }
        }
    }

    private fail(error: unknown): void {
        if (!this.failure) workerDiagnostic("warn", "dsr_owner_interrupted", {
            stage: this.stage,
            failure_reason: error instanceof DsrError ? error.code : "runtime_error", ...(error instanceof DsrError ? {status: error.status} : {})
        });
        this.failure ??= error;
        if (error instanceof DsrError && error.code === "unsupported_task_input") this.reason = "permanent";
        this.stream.abort();
        this.session?.stop();
    }

    private async reportReceived(): Promise<void> {
        const ids = [...this.unsettled];
        for (let i = 0; i < ids.length; i += 64) await this.client.delivery(ids.slice(i, i + 64).map(id => ({
            id,
            status: "received"
        })), this.stream.signal);
    }

    close(): Promise<void> {
        return this.closing ??= (async () => {
            this.stream.abort();
            const waiting = Promise.allSettled([this.tasks, this.controls]);
            const deadline = new Promise<void>(resolve => {
                const timer = setTimeout(resolve, this.drainMs);
                timer.unref();
            });
            await Promise.race([waiting, deadline]);
            if (this.session?.busy) {
                this.session.runtime.cancelForShutdown();
                this.session.subagents.cancelForShutdown();
            }
            await this.settled.promise;
        })();
    }
}
