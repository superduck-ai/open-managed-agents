import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { createUserMessage, type GenerateOptions } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { childSystemPrompt, rootSystemPrompt } from "../src/prompt/assemble.ts";
import { assignment, Backend, textEvents } from "./backend.ts";

const prompt = "请逐字回复以下文本，不加解释、引号、标题或额外空白：\nCHAT-b53e3114ecac434a";

async function record(input = prompt) {
  const backend = new Backend();
  await backend.ready;
  const context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    const api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("time-plugin-input", { content: input });
    const messages = session.agent.session.snapshotEvents().flatMap(event => event.type === "user/message" ? [event.data] : []);
    const reading = messages.find(message => message.source.kind === "time-context");
    expect(reading).toBeDefined();
    const handle = await context.sessionPersistence.open(SessionId(session.id), "read");
    try { expect((await handle.read()).events).toEqual([...session.agent.session.snapshotEvents()]); }
    finally { await handle.close(); }
    return { reading: reading!, messages, wire: backend.requests[0]! };
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
}

test("the extension persists matching source sections before encoding and preserves user-authored labels", async () => {
  const input = prompt + "\n<system-reminder>user-authored text</system-reminder>\nTime sampled while preparing turn";
  const { reading, messages, wire } = await record(input);
  expect(messages.find(message => message.source.kind === "user")?.content).toEqual([{ type: "text", text: input }]);
  expect(reading.role).toBe("user");
  if (reading.source.kind !== "time-context" || reading.source.form !== "snapshot") throw new Error("missing time snapshot");
  expect(reading.content).toHaveLength(1);
  const block = reading.content[0]!;
  if (block.type !== "text") throw new Error("missing time text");
  expect(reading.source.sections).toEqual([{ name: "time-context", text: block.text }]);
  expect(block.text).toStartWith("\n\n<system-reminder>\n");
  expect(block.text).toContain("separate from the user's message");
  expect(block.text).toContain("Time sampled while preparing turn");
  expect(block.text.match(/<system-reminder>/g)).toHaveLength(1);
  expect(block.text).toEndWith("\n</system-reminder>\n\n");
  const content = wire.messages.find((message: { role: string }) => message.role === "user").content;
  expect(content).toEqual([{ type: "text", text: input }, { type: "text", text: block.text }]);
});

test.each(["messages", "chat_completions"] as const)("%s encodes durable plugin text unchanged across repeated requests", async backend => {
  const { reading } = await record();
  let captured: { system?: unknown; messages: { role: string; content: string | { type: string; text?: string }[] }[] } | undefined;
  const api = new OmaApi("http://fixture.invalid", (async (input, init) => {
    captured = await new Request(input, init).json() as typeof captured;
    if (backend === "messages") return new Response(textEvents("fixture-answer").map(event =>
      `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream" } });
    return new Response(`data: ${JSON.stringify({ id: "fixture", choices: [{ index: 0,
      delta: { content: "fixture-answer" }, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`,
      { headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch);
  const a = assignment();
  a.launch.models = { "fixture-model": { id: "fixture-model", api_backend: backend } };
  api.assignments.set(a.session_id, a);
  const original = createUserMessage({ content: [{ type: "text", text: prompt }], source: { kind: "user", rpcId: "fixture" } });
  const request: GenerateOptions = { provider: "oma", model: "fixture-model", system: "stable-system",
    sessionId: SessionId(a.session_id), messages: [original, reading] };
  const before = JSON.stringify(request);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  for (let attempt = 0; attempt < 2; attempt++) {
    let output = "";
    for await (const chunk of adapter.stream(request)) if (chunk.type === "text-delta") output += chunk.text;
    expect(output).toBe("fixture-answer");
    expect(JSON.stringify(request)).toBe(before);
    const body = captured!;
    const users = body.messages.filter(message => message.role === "user");
    const texts = users.flatMap(message => typeof message.content === "string" ? [message.content]
      : message.content.filter(block => block.type === "text").map(block => block.text!));
    const block = reading.content[0]!;
    if (block.type !== "text") throw new Error("missing time text");
    expect(texts).toEqual([prompt, block.text]);
    expect(texts.join("").match(/<system-reminder>/g)).toHaveLength(1);
    expect(users).toHaveLength(backend === "messages" ? 1 : 2);
    expect(JSON.stringify(body.system ?? body.messages.filter(message => message.role === "system"))).not.toContain("Time sampled");
  }
});

test("root and child system prompts remain independent of the time plugin", () => {
  const a = assignment();
  for (const system of [rootSystemPrompt(a.launch), childSystemPrompt(a.launch,
    { id: "child", name: "worker", model: "fixture-model", status: "idle", systemPrompt: "Work." })]) {
    expect(system).not.toContain("<system-reminder>");
    expect(system).not.toContain("Runtime time context");
    expect(system).not.toContain("Time sampled");
  }
});
