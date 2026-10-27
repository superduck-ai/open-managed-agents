import type { Assignment } from "../assignment.ts";
import type { SubagentThread } from "../runtime/contracts.ts";
import { childPlatformPrompt, rootPlatformPrompt } from "./platform.ts";

type Launch = Assignment["launch"];

export function rootSystemPrompt(launch: Launch): string {
  return layers([
    rootPlatformPrompt(launch.subagents !== undefined),
    launch.subagents?.inlineAgents === false
      ? "Only the configured predefined agents may be used. You cannot define inline agents. Use agent_name for a new specialist or thread_id to follow up on an existing thread." : undefined,
    launch.environment_prompt,
    skillsPrompt(launch.skills),
    instructionsPrompt("Agent instructions", launch.system),
    sessionPrompt(launch),
  ]);
}

export function childSystemPrompt(launch: Launch, thread: SubagentThread): string {
  return layers([
    childPlatformPrompt(thread.name),
    launch.environment_prompt,
    skillsPrompt(launch.skills),
    instructionsPrompt("Specialist instructions", thread.systemPrompt),
    sessionPrompt(launch),
  ]);
}

function instructionsPrompt(heading: string, text: string): string {
  return text.trim() ? `# ${heading}\n\n${text}` : "";
}

function skillsPrompt(skills: Launch["skills"]): string {
  if (!skills) return "";
  if (skills.length === 0) return "# Skills\n\nAvailable skills: []";
  return [
    "# Skills",
    "Skills are instruction folders for specific kinds of work. When a task matches one, Read its SKILL.md before starting and follow it. Several skills can apply to one task.",
    skills.map((skill) => `- ${skill.name}: ${skill.description} (${skill.path})`).join("\n"),
  ].join("\n\n");
}

function sessionPrompt(launch: Launch): string {
  return [`# This session\n\nWorking directory: ${launch.work_dir}`, launch.memory_catalog_prompt?.trim()]
    .filter(Boolean).join("\n\n");
}

function layers(parts: (string | undefined)[]): string {
  return parts.map((part) => part?.trim()).filter(Boolean).join("\n\n");
}
