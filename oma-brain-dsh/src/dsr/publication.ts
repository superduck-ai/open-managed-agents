import {
  appendPluginRecord,
  pluginRecordOf,
  type Session,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import { DsrError } from "./http.ts";

interface PublicationReceipt {
  version: 1;
  through: number;
}

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/dsr-publication": PublicationReceipt;
  }
}

interface ProjectionState {
  session: Session;
  indexed: number;
  processed: number;
  confirmed: number;
  pendingThrough: number;
  turn?: number;
  inputs: Map<number, string>;
  calls: Map<string, string>;
  fallbackCalls: Map<string, string>;
}

export interface PublicationTarget {
  session: Session;
  through: number;
}

export class TranscriptPublication {
  private readonly states = new Map<string, ProjectionState>();

  prepare(session: Session): void {
    if (this.states.get(session.id)?.session === session) return;
    const state: ProjectionState = {
      session,
      indexed: 0,
      processed: 0,
      confirmed: 0,
      pendingThrough: 0,
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

  confirmed(session: Session, event: SessionEvent): boolean {
    return event.seq < this.states.get(session.id)!.confirmed;
  }

  input(session: Session, turn: number): string | undefined {
    return this.states.get(session.id)!.inputs.get(turn);
  }

  tool(session: Session, callId: string): string | undefined {
    const state = this.states.get(session.id)!;
    return state.calls.get(callId) ?? state.fallbackCalls.get(callId);
  }

  queued(session: Session, event: SessionEvent): void {
    const state = this.states.get(session.id)!;
    state.pendingThrough = Math.max(state.pendingThrough, event.seq + 1);
  }

  targets(): PublicationTarget[] {
    const targets: PublicationTarget[] = [];
    for (const state of this.states.values()) {
      if (state.pendingThrough > state.confirmed)
        targets.push({ session: state.session, through: state.pendingThrough });
      state.pendingThrough = 0;
    }
    return targets;
  }

  acknowledge(targets: readonly PublicationTarget[]): void {
    for (const target of targets) {
      const state = this.states.get(target.session.id)!;
      if (target.through <= state.confirmed) continue;
      appendPluginRecord(target.session, "plugin:oma/dsr-publication", {
        version: 1,
        through: target.through,
      });
    }
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
    const record = pluginRecordOf(event);
    if (record?.type === "plugin:oma/dsr-publication")
      this.receipt(state, record.data, event.seq);
  }

  private receipt(
    state: ProjectionState,
    data: unknown,
    sequence: number,
  ): void {
    if (!data || typeof data !== "object")
      throw new DsrError(0, "invalid_publication_receipt");
    const receipt = data as Partial<PublicationReceipt>;
    if (receipt.version !== 1)
      throw new DsrError(0, "unsupported_publication_version");
    if (
      !Number.isSafeInteger(receipt.through) ||
      receipt.through! < state.confirmed ||
      receipt.through! > sequence
    )
      throw new DsrError(0, "invalid_publication_receipt");
    state.confirmed = receipt.through!;
  }
}
