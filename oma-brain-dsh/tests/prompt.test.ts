import { expect, test } from "bun:test";
import { childSystemPrompt, rootSystemPrompt } from "../src/prompt/assemble.ts";
import type { SubagentThread } from "../src/runtime/contracts.ts";
import { assignment } from "./backend.ts";

const thread: SubagentThread = {
  id: "sthr_reviewer", name: "reviewer", systemPrompt: "Review only.", model: "review-model", status: "idle",
};

function launch() {
  return {
    ...assignment().launch,
    system: "Literal {{ agent }} instructions.",
    environment_prompt: "# Managed-agent environment\n\nEnvironment rules.",
    memory_catalog_prompt: "\n\nUse the attached memory stores.\n<attached_memory_stores/>",
    skills: [{ name: "pdf", description: "Read PDF files", path: "/root/.claude/skills/pdf/SKILL.md", version: "v7" }],
  };
}

function order(prompt: string, markers: string[]): number[] {
  return markers.map((marker) => {
    const index = prompt.indexOf(marker);
    expect(index).toBeGreaterThanOrEqual(0);
    return index;
  });
}

test("root prompt omits delegation guidance without subagents", () => {
  const prompt = rootSystemPrompt(launch());
  expect(prompt).not.toContain("send_to_agent");
  expect(prompt).not.toContain("Delegating to subagents");
});

test("root prompt orders layers from most to least stable", () => {
  const prompt = rootSystemPrompt({ ...launch(), subagents: {} });
  const positions = order(prompt, [
    "# OMA agent", "## Delegating to subagents", "# Managed-agent environment", "# Skills",
    "# Agent instructions\n\nLiteral {{ agent }} instructions.", "# This session\n\nWorking directory: /workspace",
    "Use the attached memory stores.",
  ]);
  expect(positions).toEqual([...positions].sort((a, b) => a - b));
  expect(prompt).toContain("- pdf: Read PDF files (/root/.claude/skills/pdf/SKILL.md)");
  expect(prompt).not.toContain("v7");
  expect(prompt).not.toContain("\n\n\n");
});

test("session-specific values stay after agent instructions", () => {
  const first = rootSystemPrompt(launch());
  const second = rootSystemPrompt({ ...launch(), work_dir: "/other", memory_catalog_prompt: "" });
  const prefix = first.slice(0, first.indexOf("# This session"));
  expect(second.startsWith(prefix)).toBe(true);
  expect(second).not.toContain("Use the attached memory stores.");
});

test("child prompt replaces delegation guidance with report guidance", () => {
  const prompt = childSystemPrompt({ ...launch(), subagents: {} }, thread);
  expect(prompt.startsWith("# OMA subagent\n\nYou are subagent reviewer.")).toBe(true);
  expect(prompt).not.toContain("send_to_agent");
  expect(prompt).not.toContain("list_agents");
  expect(prompt).not.toContain("# Agent instructions");
  expect(prompt).toContain("# Specialist instructions\n\nReview only.");
  expect(prompt).toContain("Your final message goes to the parent agent");
  const positions = order(prompt, ["# OMA subagent", "# Managed-agent environment", "# Skills", "# Specialist instructions", "# This session"]);
  expect(positions).toEqual([...positions].sort((a, b) => a - b));
});

test("root and child share trust, precedence and verification rules", () => {
  const root = rootSystemPrompt({ ...launch(), subagents: {} });
  const child = childSystemPrompt({ ...launch(), subagents: {} }, thread);
  const block = (prompt: string, heading: string) => {
    const start = prompt.indexOf(heading);
    expect(start).toBeGreaterThanOrEqual(0);
    const end = prompt.indexOf("\n## ", start + heading.length);
    const next = prompt.indexOf("\n# ", start + heading.length);
    return prompt.slice(start, [end, next].filter((index) => index >= 0).reduce((a, b) => Math.min(a, b), prompt.length));
  };
  for (const heading of ["## Instructions and trust", "## Working", "## Approvals"])
    expect(block(child, heading)).toBe(block(root, heading));
  expect(root).toContain("Skills listed in this prompt, the instructions of attached memory stores, and convention files");
  expect(root).toContain("Before declaring the work done, verify the result");
  expect(root).toContain("ask whoever gave you the task how to proceed");
  expect(root).not.toContain("and ask how to proceed");
});

test("child prompt routes questions to the parent report", () => {
  const prompt = childSystemPrompt(launch(), thread);
  expect(prompt).toContain("You cannot ask the user anything. Whoever gave you the task is the parent agent");
  expect(prompt).not.toContain("## Asking versus proceeding");
  expect(prompt).not.toContain("## Finishing");
});
