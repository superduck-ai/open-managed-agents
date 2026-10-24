import { appendPluginRecord, pluginRecordOf, type Session, type SessionEvent } from "@deepseek-ai/dsh-session";
import type { ApprovedToolCall, ToolOutcome, SubagentThread } from "../runtime/contracts.ts";
import { emptyProjection, object, type SessionProjection, type ThreadExecutionState, type Payload } from "./protocol.ts";

export type Receipt =
  | { type: "input/accepted"; inputId: string }
  | { type: "input/completed"; inputId: string; phase: "completed" | "blocked" }
  | { type: "thread/started"; threadId: string; inputId: string }
  | { type: "thread/ended"; threadId: string; completed: boolean }
  | { type: "thread/updated"; thread: SubagentThread }
  | { type: "tool/approved"; threadId: string; call: ApprovedToolCall }
  | { type: "tool/dispatched"; threadId: string; call: ApprovedToolCall }
  | { type: "tool/outcome"; threadId: string; outcome: ToolOutcome }
  | { type: "tool/interrupted"; threadId: string; callId: string }
  | { type: "tool/unknown"; threadId: string; callId: string }
  | { type: "tool/settled"; threadId: string; callId: string; text: string }
  | { type: "public/queued"; payloads: Payload[] }
  | { type: "public/sent"; uuids: string[] };

declare module "@deepseek-ai/dsh-session" {
  interface PluginRecordMap {
    "plugin:oma/receipt": { version: 1; receipt: Receipt };
  }
}

function text(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function isCall(value: unknown): boolean {
  const call = object(value);
  return text(call.callId) && text(call.name) && typeof call.readOnly === "boolean"
    && !!object(call.original) && !!object(call.effective);
}

function validateReceipt(value: unknown): Receipt {
  const receipt = object(value);
  let valid = false;
  switch (receipt.type) {
    case "input/accepted": valid = text(receipt.inputId); break;
    case "input/completed": valid = text(receipt.inputId) && ["completed", "blocked"].includes(receipt.phase); break;
    case "thread/started": valid = text(receipt.threadId) && typeof receipt.inputId === "string"; break;
    case "thread/ended": valid = text(receipt.threadId) && typeof receipt.completed === "boolean"; break;
    case "thread/updated": {
      const thread = object(receipt.thread);
      valid = text(thread.id) && text(thread.name) && typeof thread.systemPrompt === "string" && text(thread.model)
        && ["running", "idle", "terminated"].includes(thread.status);
      break;
    }
    case "tool/approved":
    case "tool/dispatched": valid = text(receipt.threadId) && isCall(receipt.call); break;
    case "tool/outcome": {
      const outcome = object(receipt.outcome);
      valid = text(receipt.threadId) && isCall(outcome) && typeof outcome.text === "string" && typeof outcome.failed === "boolean"
        && (outcome.remoteOutcome === undefined || outcome.remoteOutcome === "unknown");
      break;
    }
    case "tool/interrupted":
    case "tool/unknown": valid = text(receipt.threadId) && text(receipt.callId); break;
    case "tool/settled": valid = text(receipt.threadId) && text(receipt.callId) && typeof receipt.text === "string"; break;
    case "public/queued": valid = Array.isArray(receipt.payloads) && receipt.payloads.every((payload: unknown) => text(object(payload).uuid) && text(object(payload).type)); break;
    case "public/sent": valid = Array.isArray(receipt.uuids) && receipt.uuids.every(text); break;
  }
  if (!valid) throw new Error("Invalid CCRv2 transcript receipt");
  return receipt as Receipt;
}

export function receiptOf(event: SessionEvent): Receipt | undefined {
  const record = pluginRecordOf(event);
  if (record?.type !== "plugin:oma/receipt") return;
  const data = object(record.data);
  if (data.version !== 1) throw new Error("Unsupported CCRv2 transcript receipt");
  return validateReceipt(data.receipt);
}

function execution(state: SessionProjection, threadId: string): ThreadExecutionState {
  return (state.executions ??= {})[threadId] ??= {
    inputId: state.inputId, phase: "accepted", approvedCalls: {}, toolOutcomes: {},
  };
}

function syncPhase(state: SessionProjection): void {
  const entries = Object.values(state.executions ?? {});
  if (entries.some(entry => entry.phase === "blocked")) state.phase = "blocked";
  else if (entries.some(entry => entry.phase === "dispatch")) state.phase = "dispatch";
  else state.phase = "result";
}

function applyToolReceipt(state: SessionProjection, receipt: Extract<Receipt, { threadId: string }>, rootId: string): void {
  const entry = execution(state, receipt.threadId);
  const phases = entry.callPhases ??= {};
  if (receipt.type === "tool/dispatched" || receipt.type === "tool/approved") {
    const { call } = receipt;
    entry.approvedCalls[call.callId] = { name: call.name, original: call.original, effective: call.effective };
    if (!call.readOnly) phases[call.callId] = receipt.type === "tool/dispatched" ? "dispatch" : "approved";
  } else if (receipt.type === "tool/outcome") {
    const { outcome } = receipt;
    entry.approvedCalls[outcome.callId] = { name: outcome.name, original: outcome.original, effective: outcome.effective };
    entry.toolOutcomes[outcome.callId] = { text: outcome.text, failed: outcome.failed,
      ...(outcome.remoteOutcome ? { remoteOutcome: outcome.remoteOutcome } : {}) };
    if (!outcome.readOnly) phases[outcome.callId] = "result";
  } else if (receipt.type === "tool/unknown") phases[receipt.callId] = "blocked";
  else if (receipt.type === "tool/interrupted") phases[receipt.callId] = "interrupted";
  else if (receipt.type === "tool/settled") {
    phases[receipt.callId] = "result";
    entry.toolOutcomes[receipt.callId] = { text: receipt.text, failed: true, remoteOutcome: "unknown" };
  }
  const active = Object.values(phases);
  entry.phase = active.includes("blocked") ? "blocked" : active.includes("dispatch") ? "dispatch" : active.includes("approved") ? "approved" : "result";
  if (receipt.threadId === rootId || !rootId) {
    state.approvedCalls = entry.approvedCalls;
    state.toolOutcomes = entry.toolOutcomes;
  }
  syncPhase(state);
  if (receipt.type === "tool/settled" && state.phase !== "blocked") state.phase = "completed";
}

function applyReceipt(state: SessionProjection, receipt: Receipt, rootId: string): void {
  switch (receipt.type) {
    case "input/accepted":
      state.inputId = receipt.inputId;
      state.phase = "accepted";
      state.approvedCalls = {};
      state.toolOutcomes = {};
      state.executions = {};
      break;
    case "input/completed":
      if (!state.completed.includes(receipt.inputId)) state.completed.push(receipt.inputId);
      state.phase = receipt.phase;
      break;
    case "thread/started":
      (state.executions ??= {})[receipt.threadId] = {
        inputId: receipt.inputId, phase: "accepted", approvedCalls: {}, toolOutcomes: {},
      };
      break;
    case "thread/ended": {
      const entry = execution(state, receipt.threadId);
      if (!["dispatch", "blocked"].includes(entry.phase)) entry.phase = receipt.completed ? "completed" : "interrupted";
      syncPhase(state);
      break;
    }
    case "thread/updated": {
      const threads = state.threads ??= [];
      const index = threads.findIndex(thread => thread.id === receipt.thread.id);
      if (index < 0) threads.push(receipt.thread);
      else threads[index] = receipt.thread;
      break;
    }
    case "public/queued":
      for (const payload of receipt.payloads) if (!state.outbox.some(entry => entry.uuid === payload.uuid)) state.outbox.push(payload);
      break;
    case "public/sent":
      state.outbox = state.outbox.filter(payload => !receipt.uuids.includes(payload.uuid));
      break;
    default: applyToolReceipt(state, receipt, rootId);
  }
}

export function restoreReceipts(events: readonly SessionEvent[], rootId = ""): SessionProjection {
  const state = emptyProjection();
  for (const event of events) {
    const receipt = receiptOf(event);
    if (!receipt) continue;
    applyReceipt(state, structuredClone(receipt), rootId);
    state.checkpointRevision++;
  }
  return state;
}

export class CcrReceipts {
  private readonly pending: Receipt[] = [];
  private readonly queued = new Set<string>();

  restore(events: readonly SessionEvent[], rootId: string): SessionProjection {
    const state = restoreReceipts(events, rootId);
    for (const event of events) {
      const receipt = receiptOf(event);
      if (receipt?.type === "public/queued") for (const payload of receipt.payloads) this.queued.add(payload.uuid);
    }
    return state;
  }

  note(receipt: Receipt): void {
    this.pending.push(validateReceipt(JSON.parse(JSON.stringify(receipt))));
  }

  drain(session: Session, outbox: readonly Payload[]): void {
    const fresh = outbox.filter(payload => !this.queued.has(payload.uuid));
    if (fresh.length) {
      this.note({ type: "public/queued", payloads: fresh });
      for (const payload of fresh) this.queued.add(payload.uuid);
    }
    while (this.pending.length) {
      appendPluginRecord(session, "plugin:oma/receipt", { version: 1, receipt: this.pending[0]! });
      this.pending.shift();
    }
  }
}
