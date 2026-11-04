import { isSkillName, type SkillDefinition, type SkillCandidate } from "@deepseek-ai/dsh-skill";
import { parseDocument } from "yaml";

export function parseSkillCatalog(value: unknown): SkillCandidate[] {
  if (!value || typeof value !== "object" || !("skills" in value) || !Array.isArray(value.skills))
    throw new Error("Invalid session skill catalog");
  const seen = new Set<string>();
  return value.skills.map((entry: unknown) => {
    const definition = parseSkill(entry);
    if (seen.has(definition.name)) throw new Error(`Duplicate session skill name: ${definition.name}`);
    seen.add(definition.name);
    return { ...definition, rank: 0, locator: definition };
  });
}

function parseSkill(entry: unknown): SkillDefinition {
  if (!entry || typeof entry !== "object" || !("document" in entry) || typeof entry.document !== "string"
    || !("path" in entry) || typeof entry.path !== "string"
    || !("name" in entry) || typeof entry.name !== "string"
    || !("description" in entry) || typeof entry.description !== "string")
    throw new Error("Invalid session skill document");
  const text = entry.document.replace(/\r\n/g, "\n");
  const match = /^---\n([\s\S]*?)\n---(?:\n|$)/.exec(text);
  if (text.startsWith("---\n") && !match) throw new Error("Unterminated skill frontmatter");
  let data: Record<string, unknown> = {};
  if (match) {
    const yaml = parseDocument(match[1], { uniqueKeys: true });
    if (yaml.errors.length) throw new Error("Invalid skill YAML frontmatter");
    const parsed: unknown = yaml.toJS();
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Invalid skill frontmatter");
    data = parsed as Record<string, unknown>;
  }
  const skillName = stringField(data, "name", entry.name);
  const description = stringField(data, "description", entry.description);
  if (!isSkillName(skillName) || !description) throw new Error("Skill requires a valid name and description");
  for (const key of ["disableModelInvocation", "modelInvocable", "userInvocable"])
    if (key in data) throw new Error(`Unsupported skill invocation field: ${key}`);
  const modelInvocable = !booleanField(data, "disable-model-invocation", false);
  const userInvocable = booleanField(data, "user-invocable", true);
  const suffix = "/SKILL.md";
  if (!entry.path.startsWith("/root/.claude/skills/") || !entry.path.endsWith(suffix)
    || entry.path.slice("/root/.claude/skills/".length, -suffix.length).includes("/")
    || [".", "..", ""].includes(entry.path.slice("/root/.claude/skills/".length, -suffix.length)))
    throw new Error("Invalid session skill path");
  return {
    name: skillName, description, path: entry.path, source: "oma", provider: "oma",
    invocation: { modelInvocable, userInvocable },
    resourceBase: { kind: "directory", path: entry.path.slice(0, -suffix.length) },
    content: (match ? text.slice(match[0].length) : text).trim(),
  };
}

function stringField(data: Record<string, unknown>, key: string, fallback: string): string {
  if (!(key in data)) return fallback;
  if (typeof data[key] !== "string") throw new Error(`Skill ${key} must be a string`);
  return data[key].trim();
}

function booleanField(data: Record<string, unknown>, key: string, fallback: boolean): boolean {
  if (!(key in data)) return fallback;
  if (typeof data[key] !== "boolean") throw new Error(`Skill ${key} must be a boolean`);
  return data[key];
}
