import { createServer } from "node:http";
import { setTimeout as delay } from "node:timers/promises";
import { expect } from "bun:test";
import type { Assignment } from "../src/assignment.ts";
import type { Payload } from "../src/json.ts";

export function assignment(): Assignment {
  return {
    session_id: `test_${crypto.randomUUID().replaceAll("-", "")}`,
    worker_epoch: 1,
    ingress_token: "test-ingress",
    model_token: "test-model",
    mcp_config: { mcpServers: { sandbox: { type: "http", url: "http://sandbox.test/mcp", headers: { Authorization: "Bearer test-mcp" } } } },
    launch: {
      version: 2,
      model: "test-model",
      work_dir: "/workspace",
      system: "You are a test agent.",
    },
  };
}

/** Messages APIs accept both a string and structured system text blocks. */
export function modelSystemText(request: Payload): string {
  return typeof request.system === "string" ? request.system
    : (request.system ?? []).map((block: Payload) => block.text ?? "").join("\n\n");
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
  readonly privateLog = new Map<string, Payload>();
  readonly privateAgentIds = new Map<string, string>();
  readonly publicLog = new Map<string, Payload>();
  readonly requests: Payload[] = [];
  readonly timeline: Payload[] = [];
  readonly executions: Payload[] = [];
  readonly mcpMethods: string[] = [];
  readonly delivered = new Map<string, Payload>();
  readonly statuses: string[] = [];
  readonly workerMetadata: Record<string, unknown> = {};
  readonly processed: string[] = [];
  readonly server;
  readonly ready: Promise<void>;
  readonly streams = new Set<ReadableStreamDefaultController<Uint8Array>>();
  registrations = 0;
  connections = 0;
  sandboxCount = 0;
  modelScript: Payload[][] = [textEvents("hello")];
  onModel?: (request: Payload) => Promise<Payload[]>;
  onCancel?: (params: Payload) => Promise<void>;
  onTool?: (params: Payload) => Promise<Payload>;
  onApproval?: (payload: Payload) => Promise<void>;
  hangTool = false;
  rejectTool?: (params: Payload) => boolean;
  failWrites = false;
  failPublicEvents = false;
  failPublicType?: string;
  failedCcrWrites = 0;
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
            body: incoming.method === "GET" ? undefined : Buffer.concat(chunks),
          },
        );
        const response = await this.route(request);
        outgoing.writeHead(
          response.status,
          Object.fromEntries(response.headers),
        );
        outgoing.flushHeaders();
        if (response.body) for await (const chunk of response.body) {
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
        this.a.mcp_config = { mcpServers: { sandbox: { type: "http", url: `${this.url}/sandbox/mcp`, headers: { Authorization: "Bearer test-mcp" } } } };
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
    return new TextEncoder().encode(`id: 42\nevent: client_event\ndata: ${JSON.stringify({
      session_id: this.a.session_id, event_id: id, sequence_num: 42, payload,
    })}\n\n`);
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
    const body: Payload = request.method === "GET" ? {} : await request.json();
    const json = (data: unknown, status = 200) =>
      Response.json(data, { status });
    if (path === `/v1/code/sessions/${this.a.session_id}/worker/register`) {
      expect(request.headers.get("Authorization")).toBe(`Bearer ${this.a.ingress_token}`);
      this.registrations++;
      this.timeline.push({ type: "worker.register" });
      return json({ worker_epoch: String(this.a.worker_epoch) });
    }
    if (path === `/v2/sessions/${this.a.session_id}`) return json({session_context: {
      cwd: this.a.launch.work_dir, model: this.a.launch.model, system_prompt: this.a.launch.system,
      append_system_prompt: this.a.launch.environment_prompt, memory_catalog_prompt: this.a.launch.memory_catalog_prompt, runtime_skills: this.a.launch.skills, subagents: this.a.launch.subagents, runtime_tools: this.a.launch.tools,
      runtime_tool_permissions: this.a.launch.tool_permissions,
    }});
    if (path === "/v1/messages") {
      expect(request.headers.get("Authorization")).toBe(
        `Bearer ${this.a.model_token}`,
      );
      expect(request.headers.get("anthropic-version")).toBe("2023-06-01");
      this.requests.push(body);
      this.timeline.push({ type: "model.request" });
      const script = this.onModel ? await this.onModel(body) : this.modelScript.shift();
      if (!script) return json({ error: "model script exhausted" }, 500);
      return new Response(
        script
          .map((e) => `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`)
          .join(""),
        { headers: { "Content-Type": "text/event-stream" } },
      );
    }
    if (path === "/sandbox/mcp" || path === `/v2/ccr-sessions/${this.a.session_id}/sandbox/mcp`) {
      if (request.method === "GET") return new Response(null, { status: 405 });
      expect(request.headers.get("Authorization")).toBe(path === "/sandbox/mcp" ? "Bearer test-mcp" : `Bearer ${this.a.ingress_token}`);
      expect(request.headers.has("X-Brain-Epoch")).toBe(false);
      if (body.method === "server/discover") return json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, 404);
        this.mcpMethods.push(body.method);
        if (body.method === "notifications/cancelled") { await this.onCancel?.(body.params); return new Response(null, { status: 202 }); }
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
            return Response.json({ type: "error", error: { type: "invalid_request_error", message: "Sandbox MCP request exceeds 1 MiB" } },
              { status: 400, headers: { "X-Oma-Sandbox-Not-Forwarded": "true" } });
          }
          this.executions.push(body.params);
          this.timeline.push({ type: "tool.dispatch", ...body.params });
          const states = [...this.privateLog.values()].filter(
            (p) => p.subtype === "dsh_brain_state",
          );
          if (!["read", "glob", "grep"].includes(body.params.name))
            expect(states.at(-1)!.data.phase).toBe("dispatch");
          if (this.hangTool) return await new Promise<Response>(() => {});
          result = this.onTool ? await this.onTool(body.params) : { content: [{ type: "text", text: "actual stdout\n" }] };
        } else
          return json({ jsonrpc: "2.0", id: body.id, error: { code: -32601 } });
        return json({ jsonrpc: "2.0", id: body.id, result });
    }
    if (path.includes("/worker")) {
      expect(request.headers.get("Authorization")).toBe(
        `Bearer ${this.a.ingress_token}`,
      );
      if (path.endsWith("/register")) {
        this.registrations++;
        this.timeline.push({ type: "worker.register" });
        expect(body).toEqual({});
        return json({ worker_epoch: String(this.a.worker_epoch) });
      }
      if (path.endsWith("/events/stream")) {
        expect(url.searchParams.get("worker_epoch")).toBe(String(this.a.worker_epoch));
        this.connections++;
        let controller: ReadableStreamDefaultController<Uint8Array>;
        const close = () => { this.streams.delete(controller); try { controller.close(); } catch {} };
        const stream = new ReadableStream<Uint8Array>({
          start: (value) => {
            controller = value;
            this.streams.add(value);
            value.enqueue(new TextEncoder().encode(": connected\n\n"));
            for (const [id, payload] of this.delivered) value.enqueue(this.frame(id, payload));
            request.signal.addEventListener("abort", close, { once: true });
          },
          cancel: () => { this.streams.delete(controller); request.signal.removeEventListener("abort", close); },
        });
        return new Response(stream, { headers: { "Content-Type": "text/event-stream" } });
      }
      if (request.method !== "GET") {
        if (body.worker_epoch !== this.a.worker_epoch) return json({}, 409);
      }
      if (path.endsWith("/internal-events")) {
        if (request.method === "GET")
          return json({
            data: [...this.privateLog.values()]
              .filter((payload) => url.searchParams.get("subagents") === "true"
                ? this.privateAgentIds.has(payload.uuid) : !this.privateAgentIds.has(payload.uuid))
              .map((payload) => ({ payload, agent_id: this.privateAgentIds.get(payload.uuid) })),
            next_cursor: null,
          });
        if (this.failWrites) { this.failedCcrWrites++; return json({}, 503); }
        for (const event of body.events) {
          const old = this.privateLog.get(event.payload.uuid);
          if (old) expect(event.payload).toEqual(old);
          this.privateLog.set(event.payload.uuid, event.payload);
          if (event.agent_id) this.privateAgentIds.set(event.payload.uuid, event.agent_id);
          this.timeline.push({ type: "private.persist", payload: structuredClone(event.payload) });
        }
        return json({});
      }
      if (path.endsWith("/events/delivery")) {
        for (const update of body.updates) {
          this.timeline.push({ type: "input.delivery", inputId: update.event_id, status: update.status });
          if (update.status === "processed") {
            this.processed.push(update.event_id);
            this.delivered.delete(update.event_id);
            this.timeline.push({ type: "input.ack", inputId: update.event_id });
          }
        }
        return json({ ok: true, applied: body.updates.length, ignored: 0 });
      }
      if (path.endsWith("/events")) {
        if (this.failPublicEvents || body.events.some(
          (event: Payload) => event.payload.type === this.failPublicType,
        )) { this.failedCcrWrites++; return json({}, 503); }
        for (const event of body.events) {
          this.timeline.push(structuredClone(event.payload));
          if (!event.ephemeral)
            this.publicLog.set(event.payload.uuid, event.payload);
          if (event.payload.type === "control_request")
            await this.onApproval?.(event.payload);
        }
        return json({});
      }
      if (path.endsWith("/heartbeat")) return json({});
      if (path.endsWith("/worker") && request.method === "GET")
        return json({ worker: { external_metadata: this.workerMetadata } });
      if (request.method === "PUT") {
        if (typeof body.worker_status === "string") this.statuses.push(body.worker_status);
        for (const [key, value] of Object.entries(body.external_metadata ?? {})) {
          if (value === null) delete this.workerMetadata[key];
          else this.workerMetadata[key] = value;
        }
        this.timeline.push({ type: "worker.status", status: body.worker_status, ...body });
        return json({});
      }
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
