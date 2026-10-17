import type { Assignment } from "../assignment.ts";
import type { SubagentThread } from "../runtime/contracts.ts";
import { childPlatformPrompt, rootPlatformPrompt } from "./platform.ts";

export interface PromptSection {
  name: "platform" | "environment" | "skills" | "instructions" | "session";
  text: string;
}

type Launch = Assignment["launch"];

export function rootPromptSections(launch: Launch): PromptSection[] {
  return compact([
    { name: "platform", text: rootPlatformPrompt() },
    { name: "environment", text: launch.environment_prompt ?? "" },
    { name: "skills", text: renderSkills(launch.skills ?? []) },
    { name: "instructions", text: heading("Agent instructions", launch.system) },
    { name: "session", text: renderSession(launch) },
  ]);
}

export function childPromptSections(launch: Launch, thread: Pick<SubagentThread, "name" | "systemPrompt">): PromptSection[] {
  return compact([
    { name: "platform", text: childPlatformPrompt(thread.name) },
    { name: "environment", text: launch.environment_prompt ?? "" },
    { name: "skills", text: renderSkills(launch.skills ?? []) },
    { name: "instructions", text: heading("Specialist instructions", thread.systemPrompt) },
    { name: "session", text: renderSession(launch) },
  ]);
}

export function joinPromptSections(sections: PromptSection[]): string {
  return sections.map((section) => section.text).join("\n\n");
}

function renderSkills(skills: NonNullable<Launch["skills"]>): string {
  if (!skills.length) return "";
  const lines = skills.map((skill) => `- ${skill.name}: ${skill.description} (${skill.path})`);
  return `# Skills\n\nSkills are instruction folders for specific kinds of work. When a task matches one, Read its SKILL.md before starting and follow it. Several skills can apply to one task.\n\n${lines.join("\n")}`;
}

function renderSession(launch: Launch): string {
  const memory = launch.memory_prompt?.trim();
  return `# This session\n\nSandbox working directory: ${launch.work_dir}${memory ? `\n\n${memory}` : ""}`;
}

function heading(title: string, body: string): string {
  return body.trim() ? `# ${title}\n\n${body}` : "";
}

function compact(sections: PromptSection[]): PromptSection[] {
  return sections.filter((section) => section.text.trim());
}
