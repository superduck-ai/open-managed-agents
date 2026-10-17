import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import * as OmaSandboxProvider from "./oma-sandbox/index.ts";
import { validateAssignment, type Assignment } from "./assignment.ts";
import type { CcrSession } from "./ccrv2/index.ts";
import { childPromptSections, joinPromptSections, rootPromptSections } from "./prompt/assemble.ts";

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
    systemPrompt: joinPromptSections(rootPromptSections(assignment.launch)),
    subagentSystemPrompt: (thread) => joinPromptSections(childPromptSections(assignment.launch, thread)),
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
