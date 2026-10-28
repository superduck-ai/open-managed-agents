import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { questionAnswers } from "../src/user-questions/index.ts";
import { Backend, eventually, textEvents, toolEvents } from "./backend.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

async function fixture(enabled = true) {
  const b = new Backend();
  await b.ready;
  if (enabled) b.a.launch.tools.push("ask_user_question");
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  cleanups.push(async () => { await session.close(); await ctx.fiber.dispose(); b.stop(); });
  await session.open();
  return { b, session };
}

const questions: Parameters<typeof questionAnswers>[0] = [
  { id: "mode", question: "Which mode?", options: [{ label: "Small" }, { label: "Full" }] },
  { id: "colors", question: "Which colors?", multi_select: true, options: [{ label: "Blue" }, { label: "Red" }] },
  { id: "details", question: "Which mode?" },
];

function askEvents(id = "ask_1") {
  const events = toolEvents("unused", id);
  events[1]!.content_block.name = "AskUserQuestion";
  events[2]!.delta.partial_json = JSON.stringify({ questions });
  return events;
}

test("invalid or missing question answers fail instead of inventing user input", () => {
  for (const input of [undefined, {}, { answers: null }, { answers: [] }, { answers: { "Which mode?": 7 } }, { answers: { "Which mode?": ["Small"] } }])
    expect(() => questionAnswers([questions[0]!], input)).toThrow();
});

test("answer ids and selections are validated independently of question text", () => {
  for (const answers of [
    [{ id: "other", selected: [] }], [{ id: "mode", selected: ["Unknown"] }],
    [{ id: "mode", selected: ["Small", "Small"] }], [{ id: "mode", selected: ["Small"], custom: "Other" }],
    [{ id: "mode", selected: [] }, { id: "mode", selected: [] }],
  ]) expect(() => questionAnswers([questions[0]!], { answers })).toThrow();
});

test("denied questions return tool errors without provisioning a sandbox", async () => {
  const { b, session } = await fixture();
  b.modelScript = [askEvents(), textEvents("denied")];
  b.onApproval = async payload => { await session.control({ type: "control_response", response: {
    subtype: "success", request_id: payload.request_id, response: { behavior: "deny", message: "Question disabled" },
  } }); };
  await session.receive("denied", { content: "ask" });
  const result = [...b.publicLog.values()].find(event => event.type === "user")!.message.content[0];
  expect(result.is_error).toBe(true);
  expect(JSON.stringify(b.requests[1])).toContain("Question disabled");
  expect(b.sandboxCount).toBe(0);
  expect(session.approvals.size).toBe(0);
});

test.each(["", "User declined to answer."])("cancelled questions preserve error text or use the cancellation message (%s)", async text => {
  const { b, session } = await fixture();
  b.modelScript = [askEvents(), textEvents("cancelled")];
  b.onApproval = async call => { await session.control({ type: "control_response", response: {
    subtype: "success", request_id: call.request_id,
    response: { behavior: "allow", externalResult: { isError: true, content: [{ type: "text", text }] } },
  } }); };
  await session.receive("cancelled", { content: "ask" });
  const result = [...b.publicLog.values()].find(event => event.type === "user")!.message.content[0];
  expect(result.is_error).toBe(true);
  expect(JSON.stringify(b.requests[1])).toContain(text || "User question was cancelled");
  expect(b.sandboxCount).toBe(0);
  expect(session.approvals.size).toBe(0);
});

test("custom ask tool waits through CCR and returns DeepSeek answers unchanged", async () => {
  const { b, session } = await fixture();
  b.a.launch.tool_permissions = { ask_user_question: "allow" };
  b.modelScript = [askEvents(), textEvents("answered")];
  const turn = session.receive("question", { content: "ask" });
  await eventually(() => b.statuses.includes("requires_action"));
  expect(b.requests).toHaveLength(1);
  const call = [...b.publicLog.values()].find(event => event.type === "control_request")!;
  expect(call.request.tool_name).toBe("AskUserQuestion");
  expect(call.request.input.questions[1].multi_select).toBe(true);
  expect(call.request.tool_use_id).toBe("ask_1");
  await session.control({ type: "control_response", response: { subtype: "success", request_id: call.request_id,
    response: { behavior: "allow", externalResult: { isError: false, content: [{ type: "text", text: JSON.stringify({ answers: [
      { id: "mode", selected: ["Small"] },
      { id: "colors", selected: ["Blue"], custom: "Green" },
      { id: "details", selected: [], custom: "Keep it simple" },
    ] }) }] } },
  } });
  await turn;
  const result = [...b.publicLog.values()].find(event => event.type === "user")!.message.content[0];
  expect(result.is_error).toBe(false);
  expect(JSON.parse(result.content[0].text)).toEqual({ answers: [
    { id: "mode", selected: ["Small"] },
    { id: "colors", selected: ["Blue"], custom: "Green" },
    { id: "details", selected: [], custom: "Keep it simple" },
  ] });
  expect(JSON.stringify(b.requests[1])).toContain("Keep it simple");
  expect(b.sandboxCount).toBe(0);
  expect(b.statuses.at(-1)).toBe("idle");
  expect(session.approvals.size).toBe(0);
});

test("interrupt cancels the pending questionnaire and rejects late answers", async () => {
  const { b, session } = await fixture();
  b.modelScript = [askEvents()];
  const turn = session.receive("question", { content: "ask" });
  await eventually(() => b.statuses.includes("requires_action"));
  const call = [...b.publicLog.values()].find(event => event.type === "control_request")!;
  await session.control({ type: "user.interrupt" });
  await turn;
  expect(session.approvals.size).toBe(0);
  await session.control({ type: "control_response", response: { subtype: "success", request_id: call.request_id,
    response: { behavior: "allow", externalResult: { content: [{ type: "text", text: JSON.stringify({ answers: [] }) }], isError: false } },
  } });
  expect(b.requests).toHaveLength(1);
  expect(b.statuses.at(-1)).toBe("idle");
});

test("batched questionnaires keep call identities and answers isolated", async () => {
  const { b, session } = await fixture();
  const first = askEvents("first");
  const second = askEvents("second").slice(1, 4).map(event => ({ ...event, index: 1 }));
  b.modelScript = [[...first.slice(0, 4), ...second, ...first.slice(4)], textEvents("done")];
  b.onApproval = async call => {
    await session.control({ type: "control_response", response: { subtype: "success", request_id: call.request_id,
      response: { behavior: "allow", externalResult: { isError: false, content: [{ type: "text", text: JSON.stringify({ answers: [
        { id: "mode", selected: ["Full"] }, { id: "colors", selected: ["Red"] },
        { id: "details", selected: [], custom: call.request.tool_use_id },
      ] }) }] } },
    } });
  };
  await session.receive("batch", { content: "ask twice" });
  const results = [...b.publicLog.values()].filter(event => event.type === "user").flatMap(event => event.message.content);
  expect(results).toHaveLength(2);
  for (const result of results) {
    expect(result.is_error).toBe(false);
    expect(JSON.parse(result.content[0].text).answers[2].custom).toBe(result.tool_use_id);
  }
  expect(session.approvals.size).toBe(0);
});

test("enabling and disabling questions refreshes the same session's model tools", async () => {
  const { b, session } = await fixture(false);
  b.modelScript = [textEvents("disabled"), textEvents("enabled"), textEvents("disabled again")];
  await session.receive("before", { content: "tools" });
  b.a.launch.tools = [...b.a.launch.tools, "ask_user_question"];
  await session.receive("enabled", { content: "tools", oma_refresh_tools: true });
  b.a.launch.tools = b.a.launch.tools.filter(name => name !== "ask_user_question");
  await session.receive("after", { content: "tools", oma_refresh_tools: true });
  expect(b.requests.map(request => request.tools.some((tool: { name: string }) => tool.name === "AskUserQuestion")))
    .toEqual([false, true, false]);
});
