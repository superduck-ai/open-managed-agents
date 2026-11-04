import { expect,test } from "bun:test";







import { childSystemPrompt,rootSystemPrompt } from "../src/prompt/assemble.ts";

import { assignment } from "./backend.ts";

const prompt = "请逐字回复以下文本，不加解释、引号、标题或额外空白：\nCHAT-b53e3114ecac434a";

test("root and child system prompts remain independent of the time plugin", () => {
  const a = assignment();
  for (const system of [rootSystemPrompt(a.launch), childSystemPrompt(a.launch,
    { id: "child", name: "worker", model: "fixture-model", status: "idle", systemPrompt: "Work." })]) {
    expect(system).not.toContain("<system-reminder>");
    expect(system).not.toContain("Runtime time context");
    expect(system).not.toContain("Time sampled");
  }
});
