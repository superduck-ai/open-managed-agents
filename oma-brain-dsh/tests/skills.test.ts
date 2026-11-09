import { expect, test } from "bun:test";
import { parseSkillCatalog } from "../src/skills/catalog.ts";

const entry = (document: string, name = "pdf") => ({
  name, description: "Read PDF", path: `/root/.claude/skills/${name}/SKILL.md`, version: "v1", document,
});

for (const document of [
  "---\nname: pdf\ndisable-model-invocation: yes\n---\nbody",
  "---\nname: pdf\nuser-invocable: 1\n---\nbody",
  "---\nname: pdf\nname: other\n---\nbody",
  "---\nname: Bad Name\n---\nbody",
  "---\nname: pdf\nmodelInvocable: false\n---\nbody",
  "---\nname: pdf\n",
]) test(`rejects invalid skill metadata ${JSON.stringify(document)}`, () => {
  expect(() => parseSkillCatalog({ skills: [entry(document)] })).toThrow();
});

test("rejects duplicate names and paths outside the skill mount", () => {
  expect(() => parseSkillCatalog({ skills: [entry("body"), entry("other")] })).toThrow("Duplicate");
  expect(() => parseSkillCatalog({ skills: [{ ...entry("body"), path: "/etc/passwd" }] })).toThrow("path");
});

test("parses multiline YAML and removes frontmatter from loaded instructions", () => {
  const [skill] = parseSkillCatalog({ skills: [entry("---\nname: pdf\ndescription: >-\n  Read PDF\n  documents\ndisable-model-invocation: true\nuser-invocable: false\n---\n\nUse references/forms.md.\n")] });
  expect(skill!.description).toBe("Read PDF documents");
  expect(skill!.invocation).toEqual({ modelInvocable: false, userInvocable: false });
  expect(skill!.resourceBase).toEqual({ kind: "directory", path: "/root/.claude/skills/pdf" });
  expect(skill!.locator).toMatchObject({ content: "Use references/forms.md." });
});

test("keeps legacy catalog metadata when a skill has no frontmatter", () => {
  const [skill] = parseSkillCatalog({ skills: [entry("# PDF\n\nRead instructions")] });
  expect(skill!.invocation).toEqual({ modelInvocable: true, userInvocable: true });
  expect(skill!.locator).toMatchObject({ content: "# PDF\n\nRead instructions" });
});
