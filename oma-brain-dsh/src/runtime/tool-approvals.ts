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
