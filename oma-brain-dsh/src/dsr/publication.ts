import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";
import { DsrError } from "./http.ts";

interface LegacyPublicationReceipt {
  version: 1;
  through: number;
}

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/dsr-publication": LegacyPublicationReceipt;
  }
}

interface ProjectionState {
  session: Session;
  indexed: number;
  processed: number;
  turn?: number;
  inputs: Map<number, string>;
  calls: Map<string, string>;
  fallbackCalls: Map<string, string>;
}

export class TranscriptPublication {
  private readonly states = new Map<string, ProjectionState>();

  prepare(session: Session): void {
    if (this.states.get(session.id)?.session === session) return;
    const state: ProjectionState = {
      session,
      indexed: 0,
      processed: 0,
      inputs: new Map(),
      calls: new Map(),
      fallbackCalls: new Map(),
    };
    this.states.set(session.id, state);
    for (const event of session.snapshotEvents()) this.index(state, event);
  }

  read(event: SessionEvent, session: Session): readonly SessionEvent[] {
    if (!this.states.has(session.id)) this.prepare(session);
    const state = this.states.get(session.id)!;
    if (event.seq < state.processed) return [];
    if (event.seq === state.processed && event.seq === state.indexed) {
      this.index(state, event);
      state.processed++;
      return [event];
    }
    const snapshot = state.session.snapshotEvents();
    for (let index = state.indexed; index <= event.seq; index++)
      this.index(state, snapshot[index]!);
    const pending = snapshot.slice(state.processed, event.seq + 1);
    state.processed = event.seq + 1;
    return pending;
  }

  input(session: Session, turn: number): string | undefined {
    return this.states.get(session.id)!.inputs.get(turn);
  }

  tool(session: Session, callId: string): string | undefined {
    const state = this.states.get(session.id)!;
    return state.calls.get(callId) ?? state.fallbackCalls.get(callId);
  }

  private index(state: ProjectionState, event: SessionEvent): void {
    if (event.seq !== state.indexed)
      throw new DsrError(0, "publication_history_gap");
    state.indexed++;
    if (event.type === "turn/start") state.turn = event.data.turn;
    if (
      event.type === "user/message" &&
      event.data.source.kind === "user" &&
      state.turn !== undefined &&
      !state.inputs.has(state.turn)
    )
      state.inputs.set(state.turn, event.data.id);
    if (event.type === "tool/call" && !state.calls.has(event.data.callId))
      state.calls.set(event.data.callId, event.data.name);
    if (event.type === "assistant/message")
      for (const block of event.data.message.content)
        if (block.type === "tool-call" && !state.fallbackCalls.has(block.id))
          state.fallbackCalls.set(block.id, block.name);
  }
}
