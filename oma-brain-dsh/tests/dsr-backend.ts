import { expect } from "bun:test";
import type { Payload } from "../src/json.ts";
import { Backend } from "./backend.ts";

export class DsrBackend extends Backend {
  readonly transcripts = new Map<
    string,
    { header: Payload; events: Payload[] }
  >();
  readonly finishes = new Map<string, Payload>();
  readonly permissions = new Map<string, Payload>();
  readonly routes = new Set<string>();
  readonly state: Payload = {
    model_control_sequence: null,
    model: null,
    trace_turns: null,
  };
  permissionPending = false;
  cancelRequested = false;
  failDsrPublication = false;

  queueClaim() {
    this.claimQueue.push({
      code_session_id: this.a.session_id,
      worker_epoch: this.a.worker_epoch,
      session_token: this.a.ingress_token,
      model_token: this.a.model_token,
      claim_expires_at: new Date(Date.now() + 30000).toISOString(),
    });
  }
  override frame(id: string, payload: Payload): Uint8Array {
    return new TextEncoder().encode(
      `id: 42\nevent: input\ndata: ${JSON.stringify({ ...payload, id })}\n\n`,
    );
  }
  override async route(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const prefix = `/v1/dsr/sessions/${this.a.session_id}`;
    if (url.pathname === "/v1/dsr/claims") {
      expect(request.headers.get("DSR-Protocol")).toBe("1");
      if (request.headers.get("Authorization") !== `Bearer ${this.claimToken}`)
        return Response.json({}, { status: 401 });
      this.claimRequests.push({
        workerId: request.headers.get("X-Worker-ID")!,
        blockMs: Number(url.searchParams.get("block_ms")),
      });
      const claim = this.claimQueue.shift();
      if (!claim) {
        await Bun.sleep(20);
        return new Response(null, { status: 204 });
      }
      return Response.json(claim);
    }
    if (
      url.pathname.startsWith("/v1/dsr/claims/") &&
      url.pathname.endsWith("/release")
    ) {
      this.releases.push(await request.json());
      return Response.json({ ok: true });
    }
    if (!url.pathname.startsWith(prefix)) return super.route(request);
    const path = url.pathname.slice(prefix.length);
    this.routes.add(`${request.method} ${path}`);
    expect(request.headers.get("DSR-Protocol")).toBe("1");
    expect(request.headers.get("Authorization")).toBe(
      `Bearer ${this.a.ingress_token}`,
    );
    if (
      path === "/sandbox/mcp" ||
      path.startsWith("/image-attachments/") ||
      path.startsWith("/message-attachments/") ||
      path.startsWith("/otlp/")
    )
      return super.route(request);
    const body: Payload = request.method === "GET" ? {} : await request.json();
    if (path === "/register")
      return Response.json({
        epoch: String(this.a.worker_epoch),
        protocol: "1",
        lease: { ttl_ms: 20000, grace_ms: 10000, heartbeat_interval_ms: 6000 },
        observability_enabled: false,
      });
    if (path === "/heartbeat")
      return Response.json({
        lease_expires_at: new Date(Date.now() + 20000).toISOString(),
      });
    if (path === "/context") return super.route(request);
    if (path === "/worker-state") {
      if (Object.hasOwn(body, "status"))
        throw new Error("DSR no longer accepts worker status");
      if (request.method === "PUT") {
        Object.assign(this.state, body);
        if (body.control_sequence)
          this.state.model_control_sequence = body.control_sequence;
      }
      return Response.json(this.state);
    }
    if (path === "/transcripts")
      return Response.json({
        data: [...this.transcripts]
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([id, value]) => ({
            dsh_session_id: id,
            parent_dsh_session_id: value.header.parentSession ?? null,
            session_thread_id: id === this.a.session_id ? null : id,
            header_present: true,
            next_seq: String(value.events.length),
          })),
        next_after: null,
      });
    const transcript = /^\/transcripts\/([^/]+)(?:\/(header|events))?$/.exec(
      path,
    );
    if (transcript) {
      const id = decodeURIComponent(transcript[1]!);
      if (transcript[2] === "header") {
        if (!this.transcripts.has(id))
          this.transcripts.set(id, { header: body.header, events: [] });
        return Response.json({
          dsh_session_id: id,
          next_seq: String(this.transcripts.get(id)!.events.length),
        });
      }
      const stored = this.transcripts.get(id)!;
      if (transcript[2] === "events") {
        for (const event of body.events) {
          expect(event.seq <= stored.events.length).toBe(true);
          if (event.seq === stored.events.length)
            stored.events.push(structuredClone(event));
        }
        return Response.json({ next_seq: String(stored.events.length) });
      }
      const from = Number(url.searchParams.get("from_seq") ?? "0");
      return Response.json({
        header: from === 0 ? stored.header : null,
        events: stored.events.slice(from),
        next_from_seq: null,
      });
    }
    if (path === "/inputs/stream") {
      this.connections++;
      let controller: ReadableStreamDefaultController<Uint8Array>;
      const close = () => {
        this.streams.delete(controller);
        try {
          controller.close();
        } catch {}
      };
      const stream = new ReadableStream<Uint8Array>({
        start: (value) => {
          controller = value;
          this.streams.add(value);
          value.enqueue(new TextEncoder().encode(": connected\n\n"));
          for (const [id, input] of this.delivered)
            value.enqueue(this.frame(id, input));
          request.signal.addEventListener("abort", close, { once: true });
        },
        cancel: () => {
          this.streams.delete(controller);
          request.signal.removeEventListener("abort", close);
        },
      });
      return new Response(stream, {
        headers: { "Content-Type": "text/event-stream" },
      });
    }
    if (path === "/inputs/delivery") {
      for (const update of body.updates)
        if (update.status === "processed") {
          this.processed.push(update.id);
          this.delivered.delete(update.id);
        }
      return Response.json({
        scheduled: body.updates.filter(
          (u: Payload) => u.status === "processed" && this.finishes.has(u.id),
        ).length,
        applied: body.updates.filter(
          (u: Payload) => u.status !== "processed" || !this.finishes.has(u.id),
        ).length,
        ignored: 0,
        results: body.updates.map((update: Payload) => ({
          ...update,
          outcome:
            update.status === "processed" && this.finishes.has(update.id)
              ? "scheduled"
              : "applied",
          cancel_requested_at: this.cancelRequested
            ? "2026-11-04T00:00:00Z"
            : undefined,
          execution_status: this.finishes.has(update.id)
            ? "completed"
            : "running",
        })),
      });
    }
    const finish = /^\/inputs\/([^/]+)\/finish$/.exec(path);
    if (finish) {
      this.finishes.set(finish[1]!, body);
      return Response.json(body);
    }
    if (path === "/permission-requests") {
      const id = `approval_${body.dsh_session_id}_${body.tool_use_id}`;
      this.permissions.set(id, body);
      return Response.json(
        this.permissionPending
          ? { request_id: id, state: "pending" }
          : {
              request_id: id,
              state: "resolved",
              decision: { behavior: "allow" },
            },
      );
    }
    if (path === "/public-events") {
      if (this.failDsrPublication) return Response.json({}, { status: 503 });
      let accepted = 0,
        duplicates = 0;
      for (const event of body.events) {
        if (event.type.startsWith("preview.")) continue;
        if (this.publicLog.has(event.id)) duplicates++;
        else {
          accepted++;
          this.publicLog.set(event.id, event);
        }
      }
      return Response.json({ accepted, duplicates });
    }
    return Response.json({}, { status: 404 });
  }
}
