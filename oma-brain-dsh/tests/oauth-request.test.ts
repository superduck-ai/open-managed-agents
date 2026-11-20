import { expect, test } from "bun:test";
import { requestToolOAuth } from "../src/runtime/oauth.ts";
import type { ToolExecutionBridge, ToolApproval } from "../src/runtime/contracts.ts";

test("OAuth 未完成不能放行，也不能充当工具权限批准", async () => {
  const requests: ToolApproval[] = [];
  const bridge: ToolExecutionBridge = {
    approve: async request => { requests.push(request); return { allowed: true }; },
    runTool: async (_id, execute) => execute(), approvalChanged: () => {},
  };
  const requirement = { kind: "env_oauth" as const, provider: "custom:catalog", secret_name: "CATALOG_TOKEN", parent_call_id: "bash-1", reason: "missing" as const };
  await expect(requestToolOAuth(bridge, requirement, new AbortController().signal)).rejects.toThrow("授权未完成");
  expect(requests[0]).toEqual({ kind: "oauth", toolName: "RequestOAuth", callId: "bash-1:oauth:custom:catalog", input: requirement });
  bridge.approve = async () => ({ allowed: true, externalResult: { isError: false, content: [{ type: "text", text: '{"authorized":true,"placeholder":"oma_ph_test"}' }] } });
  expect(await requestToolOAuth(bridge, requirement, new AbortController().signal)).toEqual({ authorized: true, placeholder: "oma_ph_test" });
});
