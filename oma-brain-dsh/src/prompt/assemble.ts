import type { Assignment } from "../assignment.ts";
import type { SubagentThread } from "../runtime/contracts.ts";
import { childPlatformPrompt, rootPlatformPrompt } from "./platform.ts";

type Launch = Assignment["launch"];

export function rootSystemPrompt(launch: Launch): string {
  return layers([
    rootPlatformPrompt(launch.subagents !== undefined),
    launch.environment_prompt,
    skillsPrompt(launch.skills),
    `# Agent instructions\n\n${launch.system}`,
    sessionPrompt(launch),
  ]);
}

export function childSystemPrompt(launch: Launch, thread: SubagentThread): string {
  return layers([
    childPlatformPrompt(thread.name),
    launch.environment_prompt,
    skillsPrompt(launch.skills),
    `# Specialist instructions\n\n${thread.systemPrompt}`,
    sessionPrompt(launch),
  ]);
}

function skillsPrompt(skills: Launch["skills"]): string {
  if (!skills?.length) return "";
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
