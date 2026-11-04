import { expect,test } from "bun:test";



import { validateModels } from "../src/assignment.ts";



for (const value of [null, [], { model: null }, ...[
  { max_output_tokens: 0 }, { context_window: -1 }, { api_backend: "invalid" },
  { max_output_tokens: 1.5 }, { max_output_tokens: Number.MAX_SAFE_INTEGER + 1 }, { input_modalities: [] },
].map(config => ({ model: { id: "model", api_backend: "messages", ...config } }))]) {
  test(`reject invalid model configs ${JSON.stringify(value)}`, () => expect(() => validateModels(value)).toThrow());
}
