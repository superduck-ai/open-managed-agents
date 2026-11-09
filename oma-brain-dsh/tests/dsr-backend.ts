import { transcriptResponse } from "./transcript-response.ts";
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
  permissionPending = false;
  cancelRequested = false;
  failDsrPublication = false;

  private frozenContext?: Payload;
  private async snapshotContext(): Promise<Payload> {
    if (!this.frozenContext) {
      this.frozenContext = structuredClone({
        ...this.runtimeContext(),
        subagent_contexts: {},
      });
    }
    return structuredClone(this.frozenContext!);
  }
  queueClaim() {
    this.claimQueue.push({
      code_session_id: this.a.session_id,
      worker_epoch: this.a.worker_epoch,
      session_token: this.a.ingress_token,
      model_token: this.a.model_token,
      claim_expires_at: new Date(Date.now() + 30000).toISOString(),
    });
  }
  override deliver(id: string, payload: Payload) {
    if (payload.lane === "task") this.delivered.set(id, payload);
    else super.deliver(id, payload);
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
    const body: Payload =
      request.method === "GET" || path.endsWith("/records")
        ? {}
        : await request.json();
    if (path === "/register")
      return Response.json({
        epoch: String(this.a.worker_epoch),
        protocol: "1",
        lease: { ttl_ms: 20000, grace_ms: 10000, heartbeat_interval_ms: 6000 },
        observability_enabled: false,
        context: await this.snapshotContext(),
      });
    if (path === "/heartbeat")
      return Response.json({
        lease_expires_at: new Date(Date.now() + 20000).toISOString(),
      });
    if (path === "/context") return Response.json({}, { status: 404 });
    if (path === "/transcripts")
      return Response.json({
        data: [...this.transcripts]
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([id, value]) => ({
            dsh_session_id: id,
            parent_dsh_session_id: value.header.parentSession ?? null,
            session_thread_id: id === this.a.session_id ? null : id,
            next_offset: String(value.events.length + 1),
          })),
        next_after: null,
      });
    const transcript = /^\/transcripts\/([^/]+)\/records$/.exec(path);
    if (transcript) {
      const id = decodeURIComponent(transcript[1]!);
      const from = Number(url.searchParams.get("from_offset"));
      if (request.method === "POST") {
        expect(request.headers.get("Content-Type")).toBe(
          "application/x-ndjson",
        );
        const raw = await request.text();
        expect(raw.endsWith("\n")).toBe(true);
        for (const [index, line] of raw.slice(0, -1).split("\n").entries()) {
          const record = JSON.parse(line);
          if (from + index === 0) {
            if (!this.transcripts.has(id))
              this.transcripts.set(id, { header: record, events: [] });
            else expect(this.transcripts.get(id)!.header).toEqual(record);
          } else {
            const stored = this.transcripts.get(id)!;
            expect(record.seq + 1).toBe(from + index);
            expect(record.seq <= stored.events.length).toBe(true);
            if (record.seq === stored.events.length) stored.events.push(record);
            else expect(stored.events[record.seq]).toEqual(record);
          }
        }
        return Response.json({
          next_offset: String(this.transcripts.get(id)!.events.length + 1),
        });
      }
      const stored = this.transcripts.get(id)!;
      const all = [stored.header, ...stored.events];
      return transcriptResponse(
        all.slice(from, from + 200),
        String(all.length),
        from + 200 < all.length ? String(from + 200) : null,
      );
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
            if (input.lane !== "task") value.enqueue(this.frame(id, input));
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
    if (path === "/inputs/claim") {
      for (const [id, input] of this.delivered) {
        if (input.lane !== "task" || this.finishes.has(id)) continue;
        return Response.json({
          input: { ...input, id },
          cancel_requested_at: this.cancelRequested
            ? "2026-11-04T00:00:00Z"
            : undefined,
        });
      }
      return Response.json({ input: null });
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
