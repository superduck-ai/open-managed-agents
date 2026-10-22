import { checkSessionBudget, sessionBilling } from "./billing.ts";
import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { externalMcpFetch } from "./external-mcp/proxy.ts";
import * as OmaSandboxProvider from "./oma-sandbox/index.ts";
import { validateAssignment, validateToolPermissions, validateModels, type Assignment } from "./assignment.ts";
import type { CcrSession } from "./ccrv2/index.ts";
import { childSystemPrompt, rootSystemPrompt } from "./prompt/assemble.ts";
import { WebClient } from "./web-tools/client.ts";
import * as WebTools from "./web-tools/index.ts";

/** Bind OMA credentials and launch metadata before opening the protocol bridge. */
export function createOmaSession(
  ctx: Context,
  api: OmaApi,
  assignment: Assignment,
): CcrSession {
  validateAssignment(assignment);
  api.assignments.set(assignment.session_id, assignment);
  return ctx.ccrv2.createSession({
    id: assignment.session_id,
    provider: "oma",
    model: assignment.launch.model,
    workDir: assignment.launch.work_dir,
    tools: assignment.launch.tools,
    externalMcpConfig: assignment.external_mcp_config,
    externalMcpFetch: externalMcpFetch(api, assignment.session_id),
    threadMcpContext: async (threadId) => {
      const context = await omaToolContext(api, assignment.session_id, threadId);
      return { config: context.mcp_config, fetcher: externalMcpFetch(api, assignment.session_id, threadId) };
    },
    refreshTools: async (signal) => {
      const context = await omaToolContext(api, assignment.session_id, undefined, signal);
      return { tools: context.runtime_tools, externalMcpConfig: context.mcp_config };
    },
    toolPermissions: { ...assignment.launch.tool_permissions },
    refreshToolPermissions: async (signal, threadId) => {
      const response = await api.request(`/v2/sessions/${encodeURIComponent(assignment.session_id)}`,
        assignment.ingress_token, "GET", undefined, threadId ? { "X-OMA-Session-Thread-ID": threadId } : undefined, signal);
      validateModels(response.session_context?.models);
      assignment.launch.models = response.session_context?.models;

      const billing = sessionBilling(response.session_context?.billing);
      assignment.launch.billing = billing;
      checkSessionBudget(billing);
      const permissions = response.session_context?.runtime_tool_permissions;
      validateToolPermissions(permissions);
      return permissions;
    },
    systemPrompt: rootSystemPrompt(assignment.launch),
    subagentSystemPrompt: (thread) => childSystemPrompt(assignment.launch, thread),
    setup: async (scope) => {
      await scope.plugin(OmaSandboxProvider, { api, sessionId: assignment.session_id });
    },
    setupTools: async (scope, bridge, tools, threadId, permissions) =>
      scope.plugin(WebTools, { client: new WebClient(api, assignment.session_id, scope.get("sandboxExecutor"), threadId), bridge, tools, permissions }),
    subagents: assignment.launch.subagents ?? {},
    modelMessageID: (id) => api.modelResponses.get(id),
    bindThread: (id) => {
      api.modelSessions.set(id, assignment.session_id);
      return () => { api.modelSessions.delete(id); api.modelResponses.delete(id); };
    },
  });
}

async function omaToolContext(api: OmaApi, sessionId: string, threadId?: string, signal?: AbortSignal) {
  const response = await api.request(`/v2/sessions/${encodeURIComponent(sessionId)}`,
    api.assignment(sessionId).ingress_token, "GET", undefined,
    threadId ? { "X-OMA-Session-Thread-ID": threadId } : undefined, signal);
  const context = response.session_context;
  if (!context || typeof context !== "object" || Array.isArray(context)
    || !Array.isArray(context.runtime_tools) || context.runtime_tools.some((tool: unknown) => typeof tool !== "string")
    || !context.mcp_config || typeof context.mcp_config !== "object" || Array.isArray(context.mcp_config))
    throw new Error("Invalid current session tool context");
  validateModels(context.models);
  api.assignment(sessionId).launch.models = context.models;
  return context;
}
