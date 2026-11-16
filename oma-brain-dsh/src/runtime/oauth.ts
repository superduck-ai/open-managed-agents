import { object, type Payload } from "../json.ts";
import type { ToolExecutionBridge } from "./contracts.ts";
import { toolText } from "./tools.ts";

export interface OAuthRequirement {
  kind: "env_oauth" | "mcp_oauth";
  provider?: string;
  secret_name?: string;
  mcp_server_name?: string;
  mcp_server_url?: string;
  parent_call_id: string;
  reason: "missing" | "reauthorization_required";
}

export async function requestToolOAuth(bridge: ToolExecutionBridge, requirement: OAuthRequirement, signal: AbortSignal): Promise<Payload> {
  const connection = requirement.provider ?? requirement.mcp_server_name;
  const decision = await bridge.approve({
    kind: "oauth", toolName: "RequestOAuth",
    callId: `${requirement.parent_call_id}:oauth:${connection}`,
    input: requirement,
  }, signal);
  signal.throwIfAborted();
  if (decision.externalResult?.isError) throw new Error(toolText(decision.externalResult));
  if (!decision.allowed || !decision.externalResult)
    throw new Error("目标服务授权未完成，原操作未继续。");
  const result = object(JSON.parse(toolText(decision.externalResult)));
  if (result.authorized !== true) throw new Error("目标服务授权尚未确认。");
  return result;
}
