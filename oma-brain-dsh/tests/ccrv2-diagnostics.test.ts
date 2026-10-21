import { expect, spyOn, test } from "bun:test";
import { CcrClient, CcrHttpError } from "../src/ccrv2/client.ts";
import { failureDiagnostic } from "../src/ccrv2/diagnostics.ts";

test("registration failures identify the request and response without logging credentials or response bodies", async () => {
  const logs: string[] = [];
  const info = spyOn(console, "info").mockImplementation((line) => { logs.push(String(line)); });
  const errorLog = spyOn(console, "error").mockImplementation((line) => { logs.push(String(line)); });
  let attempts = 0;
  const client = new CcrClient({
    credentials: () => { throw new Error("Registration precedes assignment binding"); }, sessionIds: () => [],
    request: async (request) => {
      attempts++;
      expect(request.path).toBe("/v1/code/sessions/session%2Fa/worker/register");
      expect(request.body).toEqual({});
      return new Response("private-response-body", { status: 409, headers: { "request-id": "req_123" } });
    },
  });
  try {
    await expect(client.register("session/a", "private-token")).rejects.toBeInstanceOf(CcrHttpError);
    expect(attempts).toBe(1);
    expect(logs.map((line) => JSON.parse(line).event)).toEqual([
      "ccrv2_worker_register_started", "ccrv2_worker_register_failed",
    ]);
    expect(JSON.parse(logs[1]!)).toMatchObject({
      session_id: "session/a", method: "POST", path: "/worker/register", worker_id_present: false,
      stage: "http_response", http_status: 409, request_id: "req_123", failure_reason: "http_error",
    });
    expect(logs.join()).not.toContain("private-");
  } finally { info.mockRestore(); errorLog.mockRestore(); }
});

test("TypeError diagnostics keep source and bundle locations while excluding error messages and URLs", () => {
  const error = new TypeError("private-token");
  error.stack = "TypeError: private-token\n" +
    "    at fn (/workspace/src/assignment.ts:31:7)\n" +
    "    at fn (/workspace/dist/main.js:300:15)\n" +
    "    at fn (https://example.test/private-response:8:9)\n" +
    "    at fn (/workspace/src/main.ts?token=private-token:3:4)";
  expect(failureDiagnostic(error)).toEqual({ failure_reason: "type_error",
    error_stack: ["src/assignment.ts:31:7", "dist/main.js:300:15"] });
});

test("registration distinguishes transport errors from invalid JSON and epoch validation", async () => {
  const logs: string[] = [];
  const info = spyOn(console, "info").mockImplementation(() => {});
  const errorLog = spyOn(console, "error").mockImplementation((line) => { logs.push(String(line)); });
  const transportError = new TypeError("private-transport-message");
  const client = new CcrClient({
    credentials: () => ({ token: "private-token", workerEpoch: 1 }), sessionIds: () => [],
    request: async () => { throw transportError; },
  });
  try {
    await expect(client.register("session", "private-token")).rejects.toBe(transportError);
    expect(JSON.parse(logs.at(-1)!)).toMatchObject({ stage: "request", failure_reason: "type_error" });
    client.config.request = async () => new Response("private-invalid-json");
    await expect(client.register("session", "private-token")).rejects.toBeInstanceOf(SyntaxError);
    expect(JSON.parse(logs.at(-1)!)).toMatchObject({ stage: "decode_response", http_status: 200, failure_reason: "invalid_json" });
    client.config.request = async () => Response.json({ worker_epoch: "invalid" });
    await expect(client.register("session", "private-token")).rejects.toThrow("Invalid registered worker epoch");
    expect(JSON.parse(logs.at(-1)!)).toMatchObject({ stage: "validate_epoch", http_status: 200 });
    expect(logs.join()).not.toContain("private-");
  } finally { info.mockRestore(); errorLog.mockRestore(); }
});

test("logging failures cannot alter registration success or its original failure", async () => {
  const info = spyOn(console, "info").mockImplementation(() => { throw new Error("log unavailable"); });
  const errorLog = spyOn(console, "error").mockImplementation(() => { throw new Error("log unavailable"); });
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => [],
    request: async () => Response.json({ worker_epoch: "7" }),
  });
  try {
    expect(await client.register("session", "token")).toBe(7);
    const failure = new TypeError("transport failed");
    client.config.request = async () => { throw failure; };
    await expect(client.register("session", "token")).rejects.toBe(failure);
  } finally { info.mockRestore(); errorLog.mockRestore(); }
});
