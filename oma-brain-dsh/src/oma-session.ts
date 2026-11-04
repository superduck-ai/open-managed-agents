import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { validateModels,validateToolPermissions,type Assignment } from "./assignment.ts";
import { createDsrMcpFetch } from "./dsr/mcp-fetch.ts";
import * as LlmAiSdk from "./llm-ai-sdk/index.ts";
import { configureOmaModel } from "./model-configs.ts";
import { omaModelOptions } from "./model.ts";
import * as OmaSandboxProvider from "./oma-sandbox/index.ts";
import { childSystemPrompt,rootSystemPrompt } from "./prompt/assemble.ts";
import type { SessionRuntimeConfig } from "./runtime/contracts.ts";
import { WebClient } from "./web-tools/client.ts";
import * as WebTools from "./web-tools/index.ts";

export function omaRuntimeConfig(ctx: Context, api: OmaApi, assignment: Assignment,
  readContext: (thread?: string, signal?: AbortSignal) => Promise<Record<string, unknown>>): SessionRuntimeConfig {
  const fetcher = (thread?: string) => createDsrMcpFetch(api.base, { code_session_id: assignment.session_id, session_token: assignment.ingress_token }, api.fetcher, thread);
  const contextOf = readContext;
  const provider = `oma:${assignment.session_id}`;
  return {
    id: assignment.session_id,
    provider,
    configureModel: call => configureOmaModel(assignment, provider, call),
    model: assignment.launch.model,
    workDir: assignment.launch.work_dir,
    tools: assignment.launch.tools,
    externalMcpConfig: assignment.external_mcp_config,
    externalMcpFetch: fetcher(),
    threadMcpContext: async (threadId) => {
      const context = await omaToolContext(api, assignment.session_id, threadId, undefined, contextOf);
      return { config: context.mcp_config, fetcher: fetcher(threadId) };
    },
    refreshTools: async (signal) => {
      const context = await omaToolContext(api, assignment.session_id, undefined, signal, contextOf);
      return { tools: context.runtime_tools as string[], externalMcpConfig: context.mcp_config };
    },
    toolPermissions: { ...assignment.launch.tool_permissions },
    refreshSessionContext: async (signal, threadId) => {
      const context = await contextOf(threadId, signal);
      const models = context.models;
      validateModels(models);
      const permissions = context.runtime_tool_permissions;
      validateToolPermissions(permissions);
      assignment.launch.models = models;
      return { toolPermissions: permissions };
    },
    systemPrompt: rootSystemPrompt(assignment.launch),
    subagentSystemPrompt: (thread) => childSystemPrompt(assignment.launch, thread),
    setup: async (scope) => {
      await scope.plugin(LlmAiSdk, { providers: [provider],
        adapter: omaModelOptions(api, scope.get("attachments"), () => assignment.session_id) });
      await scope.plugin(OmaSandboxProvider, { api, sessionId: assignment.session_id });
    },
    setupTools: async (scope, bridge, tools, threadId, permissions) =>
      scope.plugin(WebTools, { client: new WebClient(api, assignment.session_id, scope.get("sandboxExecutor"), threadId), bridge, tools, permissions }),
    subagents: assignment.launch.subagents,
    modelMessageID: (id) => api.modelResponses.get(id),
    modelRequestID: (id) => api.modelRequestIDs.get(id),
    bindThread: (id) => {
      api.modelSessions.set(id, assignment.session_id);
      return () => { api.modelSessions.delete(id); api.modelResponses.delete(id); api.modelRequestIDs.delete(id); };
    },
  };
}

async function omaToolContext(api: OmaApi, sessionId: string, threadId: string | undefined, signal: AbortSignal | undefined, readContext: (thread?: string, signal?: AbortSignal) => Promise<Record<string, unknown>>) {
  const context = await readContext(threadId, signal);
  if (!context || typeof context !== "object" || Array.isArray(context)
    || !Array.isArray(context.runtime_tools) || context.runtime_tools.some((tool: unknown) => typeof tool !== "string")
    || !context.mcp_config || typeof context.mcp_config !== "object" || Array.isArray(context.mcp_config))
    throw new Error("Invalid current session tool context");
  validateModels(context.models);
  api.assignment(sessionId).launch.models = context.models;
  return context;
}
