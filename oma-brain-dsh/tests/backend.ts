import { expect } from "bun:test";
import { createServer } from "node:http";
import { setTimeout as delay } from "node:timers/promises";
import type { Assignment } from "../src/assignment.ts";
import type { Payload } from "../src/json.ts";

export function assignment(): Assignment {
  return {
    session_id: `test_${crypto.randomUUID().replaceAll("-", "")}`,
    worker_epoch: "1",
    ingress_token: "test-ingress",
    model_token: "test-model",
    mcp_config: {
      mcpServers: {
        sandbox: {
          type: "http",
          url: "http://sandbox.test/mcp",
          headers: { Authorization: "Bearer test-mcp" },
        },
      },
    },
    launch: {
      version: 2,
      subagents: {},
      model: "test-model",
      models: { "test-model": { id: "test-model", api_backend: "messages" } },
      work_dir: "/workspace",
      system: "You are a test agent.",
      tools: ["Bash", "Read", "Write", "Edit", "Grep", "Glob"],
    },
  };
}

/** Messages APIs accept both a string and structured system text blocks. */
export function modelSystemText(request: Payload): string {
  return typeof request.system === "string"
    ? request.system
    : (request.system ?? [])
        .map((block: Payload) => block.text ?? "")
        .join("\n\n");
}

export function textEvents(text: string): Payload[] {
  return [
    { type: "message_start", message: { usage: { input_tokens: 10 } } },
    {
      type: "content_block_start",
      index: 0,
      content_block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      delta: { stop_reason: "end_turn" },
      usage: { output_tokens: 5 },
    },
    { type: "message_stop" },
  ];
}

export function toolEvents(command = "original", callId = "call_1"): Payload[] {
  return [
    { type: "message_start", message: { usage: { input_tokens: 10 } } },
    {
      type: "content_block_start",
      index: 0,
      content_block: {
        type: "tool_use",
        id: callId,
        name: "Bash",
        input: {},
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: {
        type: "input_json_delta",
        partial_json: JSON.stringify({ command }),
      },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      delta: { stop_reason: "tool_use" },
      usage: { output_tokens: 5 },
    },
    { type: "message_stop" },
  ];
}

export class Backend {
  readonly a = assignment();
  readonly publicLog = new Map<string, Payload>();
  readonly requests: Payload[] = [];
  readonly imageObjects = new Map<
    string,
    { data: Uint8Array; mediaType: string }
  >();
  readonly messageImages = new Map<
    string,
    { data: Uint8Array; mediaType: string }
  >();
  imageStatus?: number;
  imageReads = 0;
  readonly timeline: Payload[] = [];
  readonly executions: Payload[] = [];
  readonly mcpMethods: string[] = [];
  readonly delivered = new Map<string, Payload>();
  readonly processed: string[] = [];
  readonly server;
  readonly ready: Promise<void>;
  readonly streams = new Set<ReadableStreamDefaultController<Uint8Array>>();
  readonly claimQueue: Payload[] = [];
  readonly claimRequests: { workerId: string; blockMs: number }[] = [];
  readonly releases: Payload[] = [];
  claimToken = "test-claim-token";
  telemetryRequests = 0;
  telemetryStatus = 503;
  telemetryRecords: Payload[] = [];
  traceSpans: Payload[] = [];
  registrations = 0;
  connections = 0;
  sandboxCount = 0;
  modelScript: Payload[][] = [textEvents("hello")];
  onModel?: (request: Payload) => Promise<Payload[] | Response>;
  onCancel?: (params: Payload) => Promise<void>;
  onTool?: (params: Payload) => Promise<Payload>;
  hangTool = false;
  rejectTool?: (params: Payload) => boolean;
  constructor() {
    this.server = createServer(async (incoming, outgoing) => {
      try {
        const chunks: Buffer[] = [];
        for await (const chunk of incoming) chunks.push(Buffer.from(chunk));
        const abort = new AbortController();
        outgoing.on("close", () => abort.abort());
        const request = new Request(
          `http://${incoming.headers.host}${incoming.url}`,
          {
            signal: abort.signal,
            method: incoming.method,
            headers: Object.fromEntries(
              Object.entries(incoming.headers)
                .filter(([, value]) => value !== undefined)
                .map(([key, value]) => [
                  key,
                  Array.isArray(value) ? value.join(",") : value!,
                ]),
            ),
            body:
              incoming.method === "GET" || incoming.method === "HEAD"
                ? undefined
                : Buffer.concat(chunks),
          },
        );
        const response = await this.route(request);
        outgoing.writeHead(
          response.status,
          Object.fromEntries(response.headers),
        );
        outgoing.flushHeaders();
        if (response.body)
          for await (const chunk of response.body) {
            if (outgoing.destroyed) break;
            outgoing.write(chunk);
          }
        outgoing.end();
      } catch (error) {
        outgoing.writeHead(500);
        outgoing.end("test backend failed");
        console.error(error);
      }
    });
    this.ready = new Promise((resolve) =>
      this.server.listen(0, "127.0.0.1", () => {
        this.a.mcp_config = {
          mcpServers: {
            sandbox: {
              type: "http",
              url: `${this.url}/sandbox/mcp`,
              headers: { Authorization: "Bearer test-mcp" },
            },
          },
        };
        resolve();
      }),
    );
  }
  get url() {
    const address = this.server.address();
    if (!address || typeof address === "string")
      throw new Error("Backend not ready");
    return `http://127.0.0.1:${address.port}`;
  }
  frame(id: string, payload: Payload): Uint8Array {
    return new TextEncoder().encode(
      `id: 42\nevent: input\ndata: ${JSON.stringify({ ...payload, id })}\n\n`,
    );
  }
  deliver(id: string, payload: Payload) {
    this.delivered.set(id, payload);
    for (const stream of this.streams) stream.enqueue(this.frame(id, payload));
  }
  disconnect() {
    for (const stream of this.streams) stream.close();
    this.streams.clear();
  }

  stop() {
    this.server.closeAllConnections();
    this.server.close();
  }
  async route(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;
    if (/^\/v1\/dsr\/sessions\/[^/]+\/image-attachments\//.test(path)) {
      if (
        request.headers.get("Authorization") !==
        `Bearer ${this.a.ingress_token}`
      )
        return new Response(null, { status: 401 });
      if (request.headers.get("DSR-Protocol") !== "1")
        return new Response(null, { status: 409 });
      if (this.imageStatus)
        return new Response(null, { status: this.imageStatus });
      const digest = path.split("/").at(-1)!;
      if (request.method === "PUT") {
        const data = new Uint8Array(await request.arrayBuffer());
        if (
          new Bun.CryptoHasher("sha256").update(data).digest("hex") !== digest
        )
          return new Response(null, { status: 400 });
        this.imageObjects.set(digest, {
          data,
          mediaType: request.headers.get("content-type")!,
        });
        return new Response(null, { status: 201 });
      }
      if (request.method === "GET") this.imageReads++;
      const object = this.imageObjects.get(digest);
      return object
        ? new Response(
            request.method === "HEAD"
              ? null
              : new Uint8Array(object.data).buffer,
            {
              headers: {
                "content-type": object.mediaType,
                "content-length": String(object.data.byteLength),
              },
            },
          )
        : new Response(null, { status: 404 });
    }
    if (
      path.startsWith(
        `/v1/dsr/sessions/${this.a.session_id}/message-attachments/`,
      )
    ) {
      if (this.imageStatus)
        return new Response(null, { status: this.imageStatus });
      const id = path.split("/").at(-2)!;
      const image = this.messageImages.get(id);
      return image
        ? new Response(new Uint8Array(image.data).buffer, {
            headers: { "content-type": image.mediaType },
          })
        : new Response(null, { status: 404 });
    }
    const body: Payload =
      request.method === "GET" || request.method === "HEAD"
        ? {}
        : await request.json();
    const json = (data: unknown, status = 200) =>
      Response.json(data, { status });
    if (path === `/v1/dsr/sessions/${this.a.session_id}/otlp/v1/logs`) {
      this.telemetryRequests++;
      for (const resource of body.resourceLogs ?? []) {
        for (const scope of resource.scopeLogs ?? [])
          this.telemetryRecords.push(...(scope.logRecords ?? []));
      }
      return json({}, this.telemetryStatus);
    }
    if (path === `/v1/dsr/sessions/${this.a.session_id}/otlp/v1/traces`) {
      expect(request.headers.get("Authorization")).toBe(
        `Bearer ${this.a.ingress_token}`,
      );
      for (const resource of body.resourceSpans ?? []) {
        for (const scope of resource.scopeSpans ?? [])
          this.traceSpans.push(...(scope.spans ?? []));
      }
      return json({});
    }
    if (path === `/v1/dsr/sessions/${this.a.session_id}/context`)
      return json({
        context: {
          runtime_tools: this.a.launch.tools,
          mcp_config: this.a.external_mcp_config ?? {},
          cwd: this.a.launch.work_dir,
          model: this.a.launch.model,
          system_prompt: this.a.launch.system,
          append_system_prompt: this.a.launch.environment_prompt,
          memory_catalog_prompt: this.a.launch.memory_catalog_prompt,
          runtime_skills: this.a.launch.skills,
          subagents: this.a.launch.subagents,
          runtime_tool_permissions: this.a.launch.tool_permissions,
          models: this.a.launch.models,
        },
      });
    if (path === "/v1/messages") {
      expect(request.headers.get("Authorization")).toBe(
        `Bearer ${this.a.model_token}`,
      );
      expect(request.headers.get("anthropic-version")).toBe("2023-06-01");
      this.requests.push(body);
      this.timeline.push({ type: "model.request" });
      const script = this.onModel
        ? await this.onModel(body)
        : this.modelScript.shift();
      if (!script) return json({ error: "model script exhausted" }, 500);
      if (script instanceof Response) return script;
      return new Response(
        script
          .map((e) => `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`)
          .join(""),
        { headers: { "Content-Type": "text/event-stream" } },
      );
    }
    if (
      path === "/sandbox/mcp" ||
      path === `/v1/dsr/sessions/${this.a.session_id}/sandbox/mcp`
    ) {
      if (request.method === "GET") return new Response(null, { status: 405 });
      expect(request.headers.get("Authorization")).toBe(
        path === "/sandbox/mcp"
          ? "Bearer test-mcp"
          : `Bearer ${this.a.ingress_token}`,
      );
      expect(request.headers.has("X-Brain-Epoch")).toBe(false);
      if (body.method === "server/discover")
        return json(
          {
            jsonrpc: "2.0",
            id: body.id,
            error: { code: -32601, message: "Method not found" },
          },
          404,
        );
      this.mcpMethods.push(body.method);
      if (body.method === "notifications/cancelled") {
        await this.onCancel?.(body.params);
        return new Response(null, { status: 202 });
      }
      if (body.method === "notifications/initialized")
        return new Response(null, { status: 202 });
      let result: Payload;
      if (body.method === "initialize") {
        this.sandboxCount++;
        this.timeline.push({ type: "sandbox.prepare" });
        result = {
          protocolVersion: "2024-11-05",
          capabilities: { tools: {} },
          serverInfo: { name: "test", version: "1" },
        };
      } else if (body.method === "tools/call") {
        if (this.rejectTool?.(body.params)) {
          this.timeline.push({ type: "tool.rejected", ...body.params });
          return Response.json(
            {
              type: "error",
              error: {
                type: "invalid_request_error",
                message: "Sandbox MCP request exceeds 1 MiB",
              },
            },
            { status: 400, headers: { "X-Oma-Sandbox-Not-Forwarded": "true" } },
          );
        }
        this.executions.push(body.params);
        this.timeline.push({ type: "tool.dispatch", ...body.params });

        if (this.hangTool) return await new Promise<Response>(() => {});
        result = this.onTool
          ? await this.onTool(body.params)
          : { content: [{ type: "text", text: "actual stdout\n" }] };
      } else
        return json({ jsonrpc: "2.0", id: body.id, error: { code: -32601 } });
      return json({ jsonrpc: "2.0", id: body.id, result });
    }
    return json({ error: "missing route" }, 404);
  }
}

export async function eventually(
  condition: () => boolean,
  timeout = 5000,
): Promise<void> {
  const deadline = Date.now() + timeout;
  while (!condition()) {
    if (Date.now() >= deadline)
      throw new Error("Condition did not become true");
    await delay(10);
  }
}
