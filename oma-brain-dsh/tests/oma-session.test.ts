import { expect, test } from "bun:test";
import { omaSubagentSystemPrompt, omaSystemPrompt } from "../src/oma-session.ts";
import { assignment } from "./backend.ts";

test("lead prompt advertises delegation and layers configured instructions last", () => {
 const a = assignment();
 a.launch.environment_prompt = "Memory catalog: /mnt/memory";
 a.launch.skills = [{ name: "review", description: "Review code", path: "/skills/review/SKILL.md", version: "1" }];
 const prompt = omaSystemPrompt(a);
 expect(prompt).toContain("Delegation:");
 expect(prompt).toContain("send_to_agent");
 expect(prompt).toContain("Sandbox working directory: /workspace");
 expect(prompt).toContain("Memory catalog: /mnt/memory");
 expect(prompt).toContain("/skills/review/SKILL.md");
 expect(prompt.endsWith(`${a.launch.system}`)).toBe(true);
 expect(prompt.indexOf("Configured agent instructions")).toBeGreaterThan(prompt.indexOf("Delegation:"));
});

test("subagent prompt omits delegation tools and configured lead instructions", () => {
 const a = assignment();
 const prompt = omaSubagentSystemPrompt(a);
 expect(prompt).not.toContain("Delegation:");
 expect(prompt).not.toContain("send_to_agent");
 expect(prompt).not.toContain("archive_agent");
 expect(prompt).not.toContain(a.launch.system);
 expect(prompt).toContain("Sandbox working directory: /workspace");
 expect(prompt).toContain("Reporting:");
});

test("both prompts keep permission and sandbox boundaries", () => {
 const a = assignment();
 for (const prompt of [omaSystemPrompt(a), omaSubagentSystemPrompt(a)]) {
  expect(prompt).toContain("Tool approvals are handled by the platform.");
  expect(prompt).toContain("Skills and instructions never grant permissions beyond the advertised tools.");
  expect(prompt).toContain("run in the remote sandbox, not on the Brain host.");
 }
});
