import { HttpError } from "../api.ts";
import type { Payload } from "../json.ts";
import type { SandboxRecovery } from "../runtime/contracts.ts";
import {
  CANCELLATION_UNCONFIRMED,
  type SandboxCalls,
} from "../oma-sandbox/client.ts";
import type { ExternalServer } from "./config.ts";
import { connectedTools, ExternalMcpClient, mcpInvocation } from "./client.ts";
import { persistToolOutput } from "./output.ts";

function failure(errorCode: string, text: string): Payload {
  return { isError: true, errorCode, content: [{ type: "text", text }] };
}

export async function recoverMcpOAuth(
  call: { callId: string; name: string; arguments: string },
  state: Payload,
  server: ExternalServer | undefined,
  fetcher: typeof fetch,
  sandbox: SandboxCalls,
  signal: AbortSignal,
  markDispatched: (callId: string) => Promise<void>,
  threadId?: string,
  recovery?: SandboxRecovery,
): Promise<Payload> {
  const { callId, name } = call;
  const connect =
    state.discovery === true && name === `mcp__${server?.prefix}__connect`;

  if (!server || state.dispatched || (!connect && !state.invocation))
    return failure("TOOL_OUTCOME_UNKNOWN", CANCELLATION_UNCONFIRMED);
  recovery?.blocking();
  const waiting = recovery
    ? AbortSignal.any([signal, recovery.interrupt])
    : signal;
  try {
    const grant = await sandbox.credential(
      {
        kind: "mcp_oauth",
        mcp_server_name: server.name,
        mcp_server_url: server.url.href,
        parent_call_id: callId,
        reason: connect ? "missing" : "reauthorization_required",
        invocation_id: state.invocation,
      },
      waiting,
      true,
      threadId,
    );
    if (grant.authorized !== true) return failure("TOOL_NOT_STARTED", "原连接未获授权。");
  } catch (error) {
    signal.throwIfAborted();
    if (
      !waiting.aborted &&
      (!(error instanceof HttpError) || error.status >= 500)
    )
      throw error;
    return failure("TOOL_NOT_STARTED", "原连接未获授权。");
  }
  const client = new ExternalMcpClient(server, fetcher);
  try {
    const tools = await client.discover(waiting);
    if (connect)
      return { content: [{ type: "text", text: connectedTools(tools) }] };
    const current = tools.find((tool) => tool.name === name);
    const args = JSON.parse(call.arguments);
    if (!current || mcpInvocation(current, args) !== state.invocation)
      return failure(
        "TOOL_NOT_STARTED",
        "工具定义或获准参数已变化，原操作未重放。",
      );
    await markDispatched(callId);
    const result = await persistToolOutput(
      await client.call(current, args, waiting),
      sandbox,
      waiting,
      { threadId },
    );
    return result;
  } finally {
    await client.close();
  }
}
