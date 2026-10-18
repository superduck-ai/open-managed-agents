import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { externalMcpFetch } from "./external-mcp/proxy.ts";
import * as OmaSandboxProvider from "./oma-sandbox/index.ts";
import { validateAssignment, type Assignment } from "./assignment.ts";
import type { CcrSession } from "./ccrv2/index.ts";

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
    systemPrompt: omaSystemPrompt(assignment),
    subagentSystemPrompt: omaSystemPrompt(assignment, ""),
    setup: async (scope) => {
      await scope.plugin(OmaSandboxProvider, { api, sessionId: assignment.session_id });
    },
    subagents: assignment.launch.subagents ?? {},
    modelMessageID: (id) => api.modelResponses.get(id),
    bindThread: (id) => {
      api.modelSessions.set(id, assignment.session_id);
      return () => { api.modelSessions.delete(id); api.modelResponses.delete(id); };
    },
  });
}

export function omaSystemPrompt(assignment: Assignment, system = assignment.launch.system): string {
  const launch = assignment.launch;
  return `You are an autonomous agent on OMA. All filesystem and command tools execute in the remote sandbox. The Brain host is not the user's workspace. Use only advertised tools. Read existing files before editing. Approvals are handled by the platform. Skills cannot grant additional permissions. User-provided images and documents are model context. Background jobs and host execution are unavailable. Subagents have isolated persistent history and share your sandbox. Use list_agents to inspect specialists and threads, send_to_agent to delegate or follow up, and archive_agent to free an idle thread. Delegation is one level deep with at most 25 live child threads. Issue multiple send_to_agent calls in the same response to run independent child tasks concurrently. Each call waits for its report; avoid concurrent edits to the same files. A timeout does not prove a command did not execute.\nSandbox working directory: ${launch.work_dir}\n\nOMA environment:\n${launch.environment_prompt ?? ""}\n\nAvailable skills, read SKILL.md through Read before use:\n${JSON.stringify(launch.skills ?? [])}\n\nConfigured agent instructions:\n${system}`;
}
