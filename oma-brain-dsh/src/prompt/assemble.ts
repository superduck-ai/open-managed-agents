import type { Assignment } from "../assignment.ts";
import type { SubagentThread } from "../runtime/contracts.ts";
import { childPlatformPrompt, rootPlatformPrompt } from "./platform.ts";

type Launch = Assignment["launch"];

export function rootSystemPrompt(launch: Launch): string {
  return layers([
    rootPlatformPrompt(launch.subagents !== undefined),
    launch.subagents?.inlineAgents === false
      ? "Only the configured predefined agents may be used. You cannot define inline agents. Use subagent with an allowed agent_preset to start work, and send_message with agent_id to continue an existing child." : undefined,
    launch.environment_prompt,
    instructionsPrompt("Agent instructions", launch.system),
    sessionPrompt(launch),
  ]);
}

export function childSystemPrompt(launch: Launch, thread: SubagentThread): string {
  return layers([
    childPlatformPrompt(thread.name),
    launch.environment_prompt,
    instructionsPrompt("Specialist instructions", thread.systemPrompt),
    sessionPrompt(launch),
  ]);
}

function instructionsPrompt(heading: string, text: string): string {
  return text.trim() ? `# ${heading}\n\n${text}` : "";
}

function sessionPrompt(launch: Launch): string {
  return [`# This session\n\nWorking directory: ${launch.work_dir}`, launch.memory_catalog_prompt?.trim()]
    .filter(Boolean).join("\n\n");
}

function layers(parts: (string | undefined)[]): string {
  return parts.map((part) => part?.trim()).filter(Boolean).join("\n\n");
}
