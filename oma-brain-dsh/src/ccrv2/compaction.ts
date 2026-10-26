import type { Context } from "@deepseek-ai/cordis";
import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";

interface CompactionState {
  startTokens?: number;
  pending?: { tokens?: number; sequence: number };
}

export class CcrCompaction {
  private readonly states = new WeakMap<Session, CompactionState>();

  constructor(
    private readonly ctx: Context,
    private readonly notifiedSequence: (sessionId: string) => number | undefined,
    private readonly publish: (sessionId: string, tokens: number | undefined, sequence: number) => Promise<void>,
  ) {}

  record(session: Session, event: SessionEvent): void {
    if (event.type === "compaction/start") {
      this.state(session).startTokens = this.ctx.tokenMeter.measure(session).totalTokens;
    } else if (event.type === "compaction/prune") {
      this.queueCompaction(session, event.seq, this.ctx.tokenMeter.measure(session).totalTokens);
    } else if (event.type === "compaction/end") {
      const state = this.state(session);
      if (event.data.error === undefined)
        this.queueCompaction(session, event.seq, state.startTokens ?? this.ctx.tokenMeter.measure(session).totalTokens);
      state.startTokens = undefined;
    }
  }

  async restore(session: Session): Promise<void> {
    const notified = this.notifiedSequence(session.id) ?? -1;
    for (const event of session.snapshotEvents()) {
      if (event.seq <= notified) continue;
      if (event.type === "compaction/prune" || (event.type === "compaction/end" && event.data.error === undefined))
        this.queueCompaction(session, event.seq);
    }
    if (!this.states.get(session)?.pending) return;
    await this.ctx.sessions.flush(session);
    await this.flush(session);
  }

  async flush(session: Session): Promise<void> {
    const state = this.states.get(session);
    const pending = state?.pending;
    if (!pending) return;
    await this.publish(session.id, pending.tokens, pending.sequence);
    if (state.pending === pending) state.pending = undefined;
  }

  private state(session: Session): CompactionState {
    let state = this.states.get(session);
    if (!state) {
      state = {};
      this.states.set(session, state);
    }
    return state;
  }

  private queueCompaction(session: Session, sequence: number, tokens?: number): void {
    const state = this.state(session);
    state.pending = { sequence, tokens: state.pending?.tokens ?? tokens };
  }
}
