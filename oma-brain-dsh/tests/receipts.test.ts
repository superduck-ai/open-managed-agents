import { expect, test } from "bun:test";
import { appendPluginRecord, Session, SessionId, type PluginRecordMap } from "@deepseek-ai/dsh-session";
import { CcrReceipts, receiptOf, restoreReceipts, type Receipt } from "../src/ccrv2/receipts.ts";

const rootId = "root-session";
const call = { callId: "call-1", name: "write_file", original: { path: "before" }, effective: { path: "approved" }, readOnly: false };

function transcript(receipts: Receipt[]) {
  const session = Session.create(SessionId(rootId));
  for (const receipt of receipts) appendPluginRecord(session, "plugin:oma/receipt", { version: 1, receipt });
  return session.snapshotEvents();
}

function malformed(data: unknown) {
  const session = Session.create(SessionId(rootId));
  appendPluginRecord(session, "plugin:oma/receipt", data as PluginRecordMap["plugin:oma/receipt"]);
  return session.snapshotEvents();
}

test("owned malformed or unsupported transcript receipts fail closed", () => {
  const invalid = [
    { version: 2, receipt: { type: "input/accepted", inputId: "input-1" } },
    { version: 1, receipt: { type: "input/accepted", inputId: "" } },
    { version: 1, receipt: { type: "tool/dispatched", threadId: rootId, call: { ...call, effective: [] } } },
    { version: 1, receipt: { type: "tool/outcome", threadId: rootId, outcome: { ...call, text: "done", failed: false, remoteOutcome: "success" } } },
    { version: 1, receipt: { type: "public/queued", payloads: [{ type: "result" }] } },
    { version: 1, receipt: { type: "future/required" } },
  ];
  for (const data of invalid) expect(() => restoreReceipts(malformed(data), rootId)).toThrow();
});

test("dispatch remains distinguishable from unknown and settled tool outcomes", () => {
  const events: Receipt[] = [
    { type: "input/accepted", inputId: "input-1" },
    { type: "thread/started", threadId: rootId, inputId: "input-1" },
    { type: "tool/dispatched", threadId: rootId, call },
  ];
  const dispatched = restoreReceipts(transcript(events), rootId);
  expect(dispatched.phase).toBe("dispatch");
  expect(dispatched.executions?.[rootId]?.callPhases?.[call.callId]).toBe("dispatch");
  expect(dispatched.approvedCalls[call.callId]?.effective).toEqual({ path: "approved" });
  events.push({ type: "tool/unknown", threadId: rootId, callId: call.callId });
  expect(restoreReceipts(transcript(events), rootId).phase).toBe("blocked");
  events.push({ type: "tool/settled", threadId: rootId, callId: call.callId, text: "Operator acknowledged unknown effects" });
  const settled = restoreReceipts(transcript(events), rootId);
  expect(settled.phase).toBe("completed");
  expect(settled.executions?.[rootId]?.callPhases?.[call.callId]).toBe("result");
  expect(settled.toolOutcomes[call.callId]).toEqual({ text: "Operator acknowledged unknown effects", failed: true, remoteOutcome: "unknown" });
});

test("completed inputs deduplicate across later turns while each execution starts clean", () => {
  const state = restoreReceipts(transcript([
    { type: "input/accepted", inputId: "input-1" },
    { type: "tool/dispatched", threadId: rootId, call },
    { type: "tool/outcome", threadId: rootId, outcome: { ...call, text: "done", failed: false } },
    { type: "input/completed", inputId: "input-1", phase: "completed" },
    { type: "input/completed", inputId: "input-1", phase: "completed" },
    { type: "input/accepted", inputId: "input-2" },
  ]), rootId);
  expect(state.completed).toEqual(["input-1"]);
  expect(state.inputId).toBe("input-2");
  expect(state.phase).toBe("accepted");
  expect(state.approvedCalls).toEqual({});
  expect(state.toolOutcomes).toEqual({});
  expect(state.executions).toEqual({});
});

test("public event recovery retains stable UUIDs and only removes confirmed sends", () => {
  const first = { type: "assistant", uuid: "public-1", message: { role: "assistant", content: "hello" } };
  const second = { type: "result", uuid: "public-2", subtype: "success" };
  const records: Receipt[] = [
    { type: "public/queued", payloads: [first, second] },
    { type: "public/queued", payloads: [first] },
  ];
  expect(restoreReceipts(transcript(records), rootId).outbox).toEqual([first, second]);
  records.push({ type: "public/sent", uuids: [first.uuid] });
  expect(restoreReceipts(transcript(records), rootId).outbox).toEqual([second]);
});

test("child configuration and tool execution restore independently of the root", () => {
  const child = { id: "child-session", name: "specialist", systemPrompt: "Inspect files", model: "child-model", tools: ["read_file"], status: "running" as const, inline: true };
  const state = restoreReceipts(transcript([
    { type: "input/accepted", inputId: "input-1" },
    { type: "thread/started", threadId: rootId, inputId: "input-1" },
    { type: "thread/updated", thread: child },
    { type: "thread/started", threadId: child.id, inputId: "input-1" },
    { type: "tool/dispatched", threadId: child.id, call },
    { type: "tool/outcome", threadId: child.id, outcome: { ...call, text: "child done", failed: false } },
    { type: "thread/ended", threadId: child.id, completed: true },
    { type: "thread/updated", thread: { ...child, status: "idle" } },
  ]), rootId);
  expect(state.threads).toEqual([{ ...child, status: "idle" }]);
  expect(state.executions?.[child.id]?.phase).toBe("completed");
  expect(state.executions?.[child.id]?.toolOutcomes[call.callId]?.text).toBe("child done");
  expect(state.executions?.[rootId]?.phase).toBe("accepted");
  expect(state.approvedCalls).toEqual({});
  expect(state.toolOutcomes).toEqual({});
});

test("receipt drain snapshots facts and does not enqueue restored public UUIDs twice", () => {
  const payload = { type: "result", uuid: "public-1" };
  const session = Session.create(SessionId(rootId));
  const writer = new CcrReceipts();
  const accepted: Receipt = { type: "input/accepted", inputId: "input-1" };
  writer.note(accepted);
  accepted.inputId = "mutated-after-note";
  writer.drain(session, [payload]);
  const restored = new CcrReceipts();
  expect(restored.restore(session.snapshotEvents(), rootId).inputId).toBe("input-1");
  restored.drain(session, [payload]);
  expect(session.snapshotEvents().map(receiptOf)).toEqual([
    { type: "input/accepted", inputId: "input-1" },
    { type: "public/queued", payloads: [payload] },
  ]);
});

test("approval receipts preserve effective arguments without claiming tool dispatch", () => {
  const state = restoreReceipts(transcript([
    { type: "input/accepted", inputId: "input-1" },
    { type: "tool/approved", threadId: rootId, call },
  ]), rootId);
  expect(state.executions?.[rootId]?.callPhases?.[call.callId]).toBe("approved");
  expect(state.approvedCalls[call.callId]?.effective).toEqual({ path: "approved" });
  expect(state.toolOutcomes[call.callId]).toBeUndefined();
  expect(state.phase).not.toBe("dispatch");
});

test("optional undefined fields are omitted before native JSON receipt serialization", () => {
  const session = Session.create(SessionId(rootId));
  const writer = new CcrReceipts();
  writer.note({ type: "thread/updated", thread: {
    id: "child-session", name: "specialist", systemPrompt: "Inspect", model: "child-model", status: "idle",
    tools: undefined, inline: undefined, agentSnapshot: undefined,
  } });
  writer.note({ type: "tool/outcome", threadId: rootId, outcome: { ...call, text: "done", failed: false, remoteOutcome: undefined } });
  expect(() => writer.drain(session, [])).not.toThrow();
  const state = restoreReceipts(session.snapshotEvents(), rootId);
  expect(state.threads?.[0]).not.toHaveProperty("tools");
  expect(state.threads?.[0]).not.toHaveProperty("agentSnapshot");
  expect(state.toolOutcomes[call.callId]).toEqual({ text: "done", failed: false });
});
