import { expect, test } from "bun:test";
import { OmaApi } from "../src/api.ts";
import { omaRuntimeConfig } from "../src/oma-session.ts";
import { assignment } from "./backend.ts";
import type { SubagentThread } from "../src/runtime/contracts.ts";

const child = (parent: string): SubagentThread => ({
  id: "child",
  parentSessionId: parent,
  name: "worker",
  model: "test",
  systemPrompt: "",
  status: "idle",
});

test("new scoped catalog refuses missing, nested and unversioned child authority", () => {
  const task = assignment();
  task.launch.skillCatalogVersion = 1;
  const runtime = omaRuntimeConfig(new OmaApi("https://api.example"), task);
  expect(() => runtime.subagentSystemPrompt!(child("foreign"))).toThrow(
    "authoritative root parent",
  );
  expect(() =>
    runtime.subagentSystemPrompt!({
      ...child(task.session_id),
      parentSessionId: undefined,
      inline: true,
    }),
  ).toThrow("authoritative root parent");
  expect(() => runtime.subagentSystemPrompt!(child(task.session_id))).toThrow(
    "Missing frozen subagent context",
  );
  task.subagent_contexts.worker = { mcp_config: {} };
  expect(() => runtime.subagentSystemPrompt!(child(task.session_id))).toThrow(
    "Missing frozen subagent Skill catalog",
  );
});

test("each child receives only frozen pinned catalogue; root inline inherits authoritative root", () => {
  const task = assignment();
  task.launch.skillCatalogVersion = 1;
  task.launch.skills = [
    {
      name: "demo",
      description: "root-version-two",
      version: "2",
      path: "/skills/two/SKILL.md",
    },
  ];
  task.subagent_contexts.worker = {
    mcp_config: {},
    skill_catalog_version: 1,
    runtime_skills: [
      {
        name: "demo",
        description: "child-version-one",
        version: "1",
        path: "/skills/one/SKILL.md",
      },
    ],
  };
  const runtime = omaRuntimeConfig(new OmaApi("https://api.example"), task);
  const prompt = runtime.subagentSystemPrompt!(child(task.session_id));
  expect(prompt).toContain("child-version-one");
  expect(prompt).not.toContain("root-version-two");
  expect(runtime.systemPrompt).toContain("root-version-two");
  expect(runtime.systemPrompt).not.toContain("child-version-one");
  const inline = runtime.subagentSystemPrompt!({
    ...child(task.session_id),
    inline: true,
  });
  expect(inline).toContain("root-version-two");
  expect(inline).not.toContain("child-version-one");
  task.subagent_contexts.worker.runtime_skills = [];
  const empty = runtime.subagentSystemPrompt!(child(task.session_id));
  expect(empty).toContain("Available skills: []");
  expect(empty).not.toContain("root-version-two");
});
