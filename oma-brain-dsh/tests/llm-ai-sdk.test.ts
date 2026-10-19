import { expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

test("AI SDK adapter module depends only on DSH and AI SDK packages", () => {
  const directory = join(import.meta.dir, "../src/llm-ai-sdk");
  const imports = readdirSync(directory).flatMap((file) =>
    [...readFileSync(join(directory, file), "utf8").matchAll(/from "([^"]+)"/g)].map((match) => match[1]!));
  expect(imports.filter((source) => !/^(\.\/|@ai-sdk\/|@deepseek-ai\/)/.test(source))).toEqual([]);
});
