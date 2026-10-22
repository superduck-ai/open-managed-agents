import { validateAssignment, type Assignment } from "./assignment.ts";
import type { OmaApi } from "./api.ts";
import { PermanentAdmissionError } from "./claim.ts";

export interface BrainTask {
  session_id: string;
  worker_epoch: number;
  ingress_token: string;
  model_token: string;
}

export function validateBrainTask(value: unknown): asserts value is BrainTask {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new PermanentAdmissionError("Invalid task envelope");
  const task = value as Partial<BrainTask>;
  if (typeof task.session_id !== "string" || !task.session_id
    || typeof task.ingress_token !== "string" || !task.ingress_token
    || typeof task.model_token !== "string" || !task.model_token
    || typeof task.worker_epoch !== "number" || !Number.isSafeInteger(task.worker_epoch) || task.worker_epoch <= 0)
    throw new PermanentAdmissionError("Invalid task identity or credentials");
}

export async function registerWorkerTask(api: OmaApi, task: BrainTask, signal?: AbortSignal): Promise<Assignment> {
  const id = encodeURIComponent(task.session_id);
  const registered = await api.request(`/v1/code/sessions/${id}/worker/register`,
    task.ingress_token, "POST", {}, undefined, signal);
  if (Number(registered.worker_epoch) !== task.worker_epoch)
    throw new Error("Registered epoch does not match dispatched task");
  const response = await api.request(`/v2/sessions/${id}`, task.ingress_token, "GET", undefined, undefined, signal);
  const context = response.session_context;
  if (!context || typeof context !== "object" || Array.isArray(context))
    throw new PermanentAdmissionError("Invalid session_context");
  const assignment: Assignment = {
    ...task,
    external_mcp_config: context.mcp_config,
    launch: {
      billing: context.billing,
      version: 2, models: context.models, model: context.model, work_dir: context.cwd,
      system: context.system_prompt || context.custom_system_prompt || "",
      environment_prompt: context.append_system_prompt ?? "",
      memory_catalog_prompt: context.memory_catalog_prompt ?? "",
      skills: context.runtime_skills ?? [], subagents: context.subagents ?? undefined,
      tools: context.runtime_tools ?? [],
      tool_permissions: context.runtime_tool_permissions ?? undefined,
    },
    mcp_config: { mcpServers: { sandbox: {
      type: "http", url: `${api.base.replace(/\/$/, "")}/v2/ccr-sessions/${id}/sandbox/mcp`,
      headers: { Authorization: `Bearer ${task.ingress_token}` },
    } } },
  };
  try {
    validateAssignment(assignment);
  } catch (error) {
    throw new PermanentAdmissionError(error instanceof Error ? error.message : "Invalid assignment");
  }
  return assignment;
}
