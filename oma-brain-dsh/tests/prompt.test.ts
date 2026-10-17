import { expect, test } from "bun:test";
import { childPromptSections, joinPromptSections, rootPromptSections } from "../src/prompt/assemble.ts";
import { assignment } from "./backend.ts";

function launch(overrides: Partial<ReturnType<typeof assignment>["launch"]> = {}) {
  return { ...assignment().launch, ...overrides };
}

test("empty sections are dropped instead of leaving bare headings", () => {
  const sections = rootPromptSections(launch({ system: "  ", environment_prompt: "", skills: [], memory_prompt: "" }));
  expect(sections.map((section) => section.name)).toEqual(["platform", "session"]);
  const text = joinPromptSections(sections);
  expect(text).not.toContain("# Agent instructions");
  expect(text).not.toContain("# Skills");
});

test("subagent prompt drops delegation guidance and parent instructions", () => {
  const text = joinPromptSections(childPromptSections(
    launch({ system: "Parent-only instructions" }),
    { name: "alpha", systemPrompt: "Review the diff" },
  ));
  expect(text).toContain("You are subagent alpha.");
  expect(text).toContain("Delegation tools are unavailable");
  expect(text).toContain("# Specialist instructions\n\nReview the diff");
  expect(text).not.toContain("send_to_agent");
  expect(text).not.toContain("Parent-only instructions");
  expect(text).not.toContain("# Agent instructions");
});

test("root prompt orders sections from most to least stable", () => {
  const sections = rootPromptSections(launch({
    environment_prompt: "# Environment\n\nenv body",
    skills: [{ name: "pdf", description: "Read PDFs", path: "/root/.claude/skills/pdf/SKILL.md", version: "v7" }],
    memory_prompt: "\n\nUse the attached memory stores.",
  }));
  expect(sections.map((section) => section.name)).toEqual(["platform", "environment", "skills", "instructions", "session"]);
  const text = joinPromptSections(sections);
  expect(text).toContain("- pdf: Read PDFs (/root/.claude/skills/pdf/SKILL.md)");
  expect(text).not.toContain("v7");
  expect(text).toContain("send_to_agent");
  expect(text.endsWith("Sandbox working directory: /workspace\n\nUse the attached memory stores.")).toBe(true);
});

test("platform section is identical across sessions so it stays a cacheable prefix", () => {
  const first = rootPromptSections(launch({ work_dir: "/workspace/a", system: "A" }))[0]!;
  const second = rootPromptSections(launch({ work_dir: "/workspace/b", system: "B", memory_prompt: "memory" }))[0]!;
  expect(first.name).toBe("platform");
  expect(first.text).toBe(second.text);
  expect(first.text).not.toContain("/workspace");
});

test("agent instructions are inserted literally", () => {
  const text = joinPromptSections(rootPromptSections(launch({ system: "literal ${{ instructions }} $1" })));
  expect(text).toContain("# Agent instructions\n\nliteral ${{ instructions }} $1");
});
