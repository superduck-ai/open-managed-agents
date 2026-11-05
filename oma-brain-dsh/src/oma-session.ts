import { OmaApi } from "./api.ts";
import { type Assignment } from "./assignment.ts";
import { createDsrMcpFetch } from "./dsr/mcp-fetch.ts";
import * as LlmAiSdk from "./llm-ai-sdk/index.ts";
import { configureOmaModel } from "./model-configs.ts";
import { omaModelOptions } from "./model.ts";
import * as OmaSandboxProvider from "./oma-sandbox/index.ts";
import { childSystemPrompt, rootSystemPrompt } from "./prompt/assemble.ts";
import type { SessionRuntimeConfig } from "./runtime/contracts.ts";
import { WebClient } from "./web-tools/client.ts";
import * as WebTools from "./web-tools/index.ts";

export function omaRuntimeConfig(
  api: OmaApi,
  assignment: Assignment,
): SessionRuntimeConfig {
  const fetcher = (thread?: string) =>
    createDsrMcpFetch(
      api.base,
      {
        code_session_id: assignment.session_id,
        session_token: assignment.ingress_token,
      },
      api.fetcher,
      thread,
    );
  const provider = `oma:${assignment.session_id}`;
  return {
    id: assignment.session_id,
    provider,
    configureModel: (call) => configureOmaModel(assignment, provider, call),
    model: assignment.launch.model,
    workDir: assignment.launch.work_dir,
    tools: assignment.launch.tools,
    externalMcpConfig: assignment.external_mcp_config,
    externalMcpFetch: fetcher(),
    threadMcpContext: async (thread) => {
      const snapshot = thread.inline
        ? undefined
        : assignment.subagent_contexts[thread.name];
      if (!thread.inline && !snapshot)
        throw new Error("Missing frozen subagent context");
      return {
        config: snapshot?.mcp_config ?? assignment.external_mcp_config,
        fetcher: fetcher(thread.id),
        toolPermissions: snapshot
          ? snapshot.runtime_tool_permissions
          : assignment.launch.tool_permissions,
      };
    },
    toolPermissions: { ...assignment.launch.tool_permissions },
    systemPrompt: rootSystemPrompt(assignment.launch),
    subagentSystemPrompt: (thread) =>
      childSystemPrompt(assignment.launch, thread),
    setup: async (scope) => {
      await scope.plugin(LlmAiSdk, {
        providers: [provider],
        adapter: omaModelOptions(
          api,
          scope.get("attachments"),
          () => assignment.session_id,
        ),
      });
      await scope.plugin(OmaSandboxProvider, {
        api,
        sessionId: assignment.session_id,
      });
    },
    setupTools: async (scope, bridge, tools, threadId, permissions) =>
      scope.plugin(WebTools, {
        client: new WebClient(
          api,
          assignment.session_id,
          scope.get("sandboxExecutor"),
          threadId,
        ),
        bridge,
        tools,
        permissions,
      }),
    subagents: assignment.launch.subagents,
    modelMessageID: (id) => api.modelResponses.get(id),
    modelRequestID: (id) => api.modelRequestIDs.get(id),
    bindThread: (id) => {
      api.modelSessions.set(id, assignment.session_id);
      return () => {
        api.modelSessions.delete(id);
        api.modelResponses.delete(id);
        api.modelRequestIDs.delete(id);
      };
    },
  };
}
