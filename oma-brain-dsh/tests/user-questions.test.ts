import { afterEach,expect,test } from "bun:test";



import { questionAnswers } from "../src/user-questions/index.ts";

import { toolEvents } from "./backend.ts";

const cleanups: (() => Promise<void>)[] = [];

afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

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
