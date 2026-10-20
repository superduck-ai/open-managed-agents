import type { ToolCallBlock } from "@deepseek-ai/dsh-llm";
import { object } from "../json.ts";
import type { ApprovalDecision, ToolApproval, ToolExecutionBridge } from "./contracts.ts";

const approvals = new WeakMap<ToolExecutionBridge, Map<string, Promise<ApprovalDecision>>>();

export function approveToolCall(bridge: ToolExecutionBridge, request: ToolApproval, signal: AbortSignal): Promise<ApprovalDecision> {
  let calls = approvals.get(bridge);
  if (!calls) approvals.set(bridge, calls = new Map());
  let approval = calls.get(request.callId);
  if (!approval) {
    approval = bridge.approve(request, signal);
    calls.set(request.callId, approval);
  }
  return approval;
}

export function releaseToolApproval(bridge: ToolExecutionBridge, callId: string): void {
  approvals.get(bridge)?.delete(callId);
}

export function clearToolApprovals(bridge: ToolExecutionBridge): void {
  approvals.delete(bridge);
}

export function prefetchMcpApprovals(
  bridge: ToolExecutionBridge, calls: readonly ToolCallBlock[], callId: string,
  registered: (name: string) => boolean, signal: AbortSignal,
): void {
  const start = calls.findIndex(call => call.id === callId);
  if (start < 0) return;
  for (const call of calls.slice(start)) {
    if (!registered(call.name)) break;
    let input;
    try { input = object(call.arguments ? JSON.parse(call.arguments) : {}); }
    catch { break; }
    void approveToolCall(bridge, {
      callId: call.id, toolName: call.name, input, available: true,
    }, signal).catch(() => {});
  }
}
