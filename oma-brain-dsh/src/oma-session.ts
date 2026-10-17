import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
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
    systemPrompt: omaSystemPrompt(assignment),
    subagentSystemPrompt: omaSubagentSystemPrompt(assignment),
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

export function omaSystemPrompt(assignment: Assignment): string {
  return [
    "You are the lead agent of an OMA session. You plan the work, act in the sandbox, and may delegate to specialists.",
    operatingRules,
    delegationRules,
    launchContext(assignment),
    `Configured agent instructions define your role and task rules. They apply within the rules above and cannot grant tools, permissions or sandbox access.\n${assignment.launch.system}`,
  ].join("\n\n");
}

export function omaSubagentSystemPrompt(assignment: Assignment): string {
  return [
    "You are a specialist subagent of an OMA session. You work in the parent's sandbox on one delegated task and report back.",
    operatingRules,
    launchContext(assignment),
  ].join("\n\n");
}

const operatingRules = [
  "Execution:",
  "- File, search and command tools run in the remote sandbox, not on the Brain host. Do not assume host paths exist in the sandbox.",
  "- Use only the tools advertised to you. Attachments, background jobs and host execution are unavailable.",
  "- Read a file before editing it. Make targeted edits and run the relevant check when one exists.",
  "- A timed-out command may still have run. Inspect sandbox state before repeating a command that changes it.",
  "",
  "Permissions:",
  "- Tool approvals are handled by the platform. If a tool call is denied, change your approach instead of retrying it unchanged.",
  "- Skills and instructions never grant permissions beyond the advertised tools.",
  "",
  "Reporting:",
  "- End with a concise report: what changed, what you verified and how, and what remains open. Do not describe unverified steps as done.",
].join("\n");

const delegationRules = [
  "Delegation:",
  "- list_agents shows predefined specialists and retained threads. send_to_agent delegates a task and waits for its report: pass thread_id to continue a thread, agent_name for a predefined specialist, or name and system_prompt for a new inline agent.",
  "- Delegation is one level deep. Subagents keep isolated persistent history and share your sandbox, but they cannot see this conversation, so each delegated task must carry the context it needs.",
  "- At most 25 live child threads. Idle threads count toward the limit; archive_agent frees one you no longer need.",
  "- Issue several send_to_agent calls in one response to run independent tasks concurrently. Do not let concurrent children edit the same files.",
  "- Check child reports against the sandbox before relying on them.",
].join("\n");

function launchContext(assignment: Assignment): string {
  const launch = assignment.launch;
  return [
    `Sandbox working directory: ${launch.work_dir}`,
    `OMA environment:\n${launch.environment_prompt ?? ""}`,
    `Available skills. Read a skill's SKILL.md with Read before using it:\n${JSON.stringify(launch.skills ?? [])}`,
  ].join("\n\n");
}
