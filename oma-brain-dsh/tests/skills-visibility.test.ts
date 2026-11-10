import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { runSubagent } from "./subagent-tools.ts";
import { Backend, textEvents } from "./backend.ts";

const document = (name: string) => ({ name, description: `Use ${name}`, version: "v1",
  path: `/root/.claude/skills/${name}/SKILL.md`, document: `---\nname: ${name}\ndescription: Use ${name}\n---\n${name} body` });

const catalogs = (session: ReturnType<typeof createOmaSession>) => session.agent.session.snapshotEvents()
  .filter(event => event.type === "user/message" && event.data.source.kind === "skill-catalog");

test("failed discovery publishes incomplete catalog and retries on the next turn", async () => {
  const backend = new Backend();
  await backend.ready;
  const original = backend.route.bind(backend);
  let attempts = 0;
  backend.route = async request => {
    if (new URL(request.url).pathname.endsWith("/skills")) {
      if (++attempts === 1) return new Response(null, { status: 503 });
      return Response.json({ skills: [document("pdf")] });
    }
    return original(request);
  };
  const context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    const api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("failed-discovery", { content: "Inspect skills" });
    expect(JSON.stringify(catalogs(session))).toContain('"incomplete":true');
    backend.modelScript = [textEvents("done")];
    await session.receive("recovered-discovery", { content: "Try again" });
    expect(attempts).toBe(2);
    const latest = catalogs(session).at(-1)!;
    expect(JSON.stringify(latest)).toContain("pdf");
    expect(JSON.stringify(latest)).not.toContain('"incomplete":true');
    expect(backend.sandboxCount).toBe(0);
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
});

test("specialist gets its own skill scope and resumed parent keeps one catalog", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "reviewer", description: "Review", systemPrompt: "Review only", tools: [],
  }] };
  const original = backend.route.bind(backend);
  const scopes: (string | null)[] = [];
  backend.route = async request => {
    if (new URL(request.url).pathname.endsWith("/skills")) {
      const thread = request.headers.get("X-OMA-Session-Thread-ID");
      scopes.push(thread);
      return Response.json({ skills: [document(thread ? "review" : "pdf")] });
    }
    return original(request);
  };
  let context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    let api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("parent-skills", { content: "Inspect skills" });
    backend.modelScript = [textEvents("child done")];
    await runSubagent(session, "subagent", { agent_preset: `${session.id}/reviewer`, description: "reviewer", prompt: "Inspect your skills" });
    const parentCatalog = JSON.stringify(catalogs(session));
    expect(parentCatalog).toContain('"name":"pdf"');
    expect(parentCatalog).not.toContain('"name":"review"');
    const childRequest = JSON.stringify(backend.requests[1]!.messages);
    expect(childRequest).toContain("Use review");
    expect(childRequest).not.toContain("Use pdf");
    expect(scopes).toContain(session.subagents.threads[0]!.id);
    await session.close();
    await context.fiber.dispose();
    context = new Context();
    api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    backend.modelScript = [textEvents("restored")];
    await session.receive("parent-resumed", { content: "Continue" });
    expect(catalogs(session)).toHaveLength(1);
    backend.modelScript = [textEvents("Skills remain available."), textEvents("after compaction")];
    const compacted = await context.get("compaction")!.compactNow(session.agent, new AbortController().signal);
    expect(compacted).not.toBeNull();
    await session.receive("parent-compacted", { content: "Continue after compaction" });
    expect(catalogs(session)).toHaveLength(2);
    expect(backend.sandboxCount).toBe(0);
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
});
