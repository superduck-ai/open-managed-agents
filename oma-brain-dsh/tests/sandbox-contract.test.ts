import { expect, test } from "bun:test";
import { OmaApi } from "../src/api.ts";
import { SandboxCalls } from "../src/oma-sandbox/client.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import { assignment } from "./backend.ts";

const endpoint = process.env.OMA_TEST_TOOL_CALL_API_BASE_URL;
const sessionId = process.env.OMA_TEST_TOOL_CALL_SESSION_ID;
const token = process.env.OMA_TEST_TOOL_CALL_WORKER_TOKEN;

test.skipIf(!endpoint || !sessionId || !token)("built-in tools reject missing required arguments through the claim protocol", async () => {
  const api = new OmaApi(endpoint!);
  api.assignments.set(sessionId!, { ...assignment(), session_id: sessionId!, ingress_token: token! });
  const executor = new SandboxCalls(api, sessionId!);
  try {
    for (const tool of sandboxToolDefinitions()) {
      const result = await executor.call(tool, {}, AbortSignal.timeout(30_000), "root", `contract-${crypto.randomUUID()}`);
      expect(result.isError, `${tool.name} accepted missing required arguments`).toBe(true);
      expect(result.content).toBeArray();
    }
  } finally { await executor.close(); }
}, 180_000);
