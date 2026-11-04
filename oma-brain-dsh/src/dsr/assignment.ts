import type { OmaApi } from "../api.ts";
import { validateAssignment, type Assignment } from "../assignment.ts";
import type { Claim, Registration } from "./wire.ts";

export function dsrAssignment(api: OmaApi, claim: Claim, registration: Registration, context: Record<string, unknown>): Assignment {
  const assignment: Assignment = {
    session_id: claim.code_session_id, worker_epoch: claim.worker_epoch, ingress_token: claim.session_token, model_token: claim.model_token,
    observability_enabled: registration.observability_enabled, registered_at: Date.now(), trace_parent: claim.trace_parent, trace_export: registration.trace_export,
    external_mcp_config: context.mcp_config,
    launch: {
      version: 2, model: context.model as string, models: context.models as Assignment["launch"]["models"], work_dir: context.cwd as string,
      system: (context.system_prompt ?? context.custom_system_prompt ?? "") as string,
      environment_prompt: (context.append_system_prompt ?? "") as string, memory_catalog_prompt: (context.memory_catalog_prompt ?? "") as string,
      skills: (context.runtime_skills ?? []) as Assignment["launch"]["skills"], subagents: (context.subagents ?? undefined) as Assignment["launch"]["subagents"],
      tools: (context.runtime_tools ?? []) as string[], tool_permissions: (context.runtime_tool_permissions ?? undefined) as Assignment["launch"]["tool_permissions"],
    },
    mcp_config: { mcpServers: { sandbox: { type: "http", url: `${api.base}/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}/sandbox/mcp`,
      headers: { Authorization: `Bearer ${claim.session_token}`, "DSR-Protocol": "1" } } } },
  };
  validateAssignment(assignment);
  return assignment;
}
