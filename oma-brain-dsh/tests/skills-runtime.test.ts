import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function skillCall(name: string): Payload[] {
  return toolEvents().map(event => event.type === "content_block_start"
    ? { ...event, content_block: { ...event.content_block, name: "skill" } }
    : event.type === "content_block_delta"
      ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify({ name }) } } : event);
}

async function runSkillScenario(policy: string, input: string, modelLoads: boolean,
  inspect: (session: ReturnType<typeof createOmaSession>, backend: Backend) => void) {
  const backend = new Backend();
  await backend.ready;
  backend.skillDocuments = [{ name: "pdf", description: "Read PDF documents", version: "v1",
    path: "/root/.claude/skills/pdf/SKILL.md",
    document: `---\nname: pdf\ndescription: Read PDF documents\n${policy}\n---\nPRIVATE_SKILL_BODY\nRead references/forms.md.` }];
  backend.modelScript = modelLoads ? [skillCall("pdf"), textEvents("done")] : [textEvents("done")];
  const context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    const api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("skill-input", { content: input });
    inspect(session, backend);
    expect(backend.sandboxCount).toBe(0);
    expect(backend.executions).toHaveLength(0);
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
}

const eventText = (session: ReturnType<typeof createOmaSession>) => JSON.stringify(session.agent.session.snapshotEvents());

test("model-disabled skill is absent from catalog and rejects model loading", async () => {
  await runSkillScenario("disable-model-invocation: true", "load pdf", true, session => {
    const events = eventText(session);
    expect(events).toContain("not available for model invocation");
    expect(events).not.toContain("PRIVATE_SKILL_BODY");
  });
});

test("user-disabled skill remains ordinary prose on explicit invocation", async () => {
  await runSkillScenario("user-invocable: false", "/pdf help", false, session => {
    expect(eventText(session)).not.toContain("PRIVATE_SKILL_BODY");
    expect(session.agent.session.snapshotEvents().some(event => event.type === "user/message"
      && event.data.source.kind === "skill-invocation")).toBe(false);
  });
});

test("model loads body through native skill tool and records the catalog once", async () => {
  await runSkillScenario("", "load pdf", true, (session, backend) => {
    const events = session.agent.session.snapshotEvents();
    const catalogs = events.filter(event => event.type === "user/message" && event.data.source.kind === "skill-catalog");
    expect(catalogs).toHaveLength(1);
    expect(JSON.stringify(catalogs)).not.toContain("PRIVATE_SKILL_BODY");
    expect(eventText(session)).toContain("PRIVATE_SKILL_BODY");
    expect(eventText(session)).toContain("skill_resources");
    expect(backend.requests[0]!.tools.some((tool: Payload) => tool.name === "skill")).toBe(true);
  });
});

test("user explicitly loads model-disabled skill without a model tool call", async () => {
  await runSkillScenario("disable-model-invocation: true", "/pdf help", false, session => {
    const invocations = session.agent.session.snapshotEvents().filter(event => event.type === "user/message"
      && event.data.source.kind === "skill-invocation");
    expect(invocations).toHaveLength(1);
    expect(JSON.stringify(invocations)).toContain("PRIVATE_SKILL_BODY");
  });
});
