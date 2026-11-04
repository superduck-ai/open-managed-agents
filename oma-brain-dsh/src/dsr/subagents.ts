import type { Context } from "@deepseek-ai/cordis";
import { dsrExecutor } from "./executor.ts";
import { traceHooks } from "../observability/trace-context.ts";
import { SessionId, type Session } from "@deepseek-ai/dsh-session";
import { foldSubagentDescriptor } from "@deepseek-ai/dsh-subagent";
import { dshThreadConfig } from "../runtime/dsh-subagents.ts";
import type { SubagentBridge, SubagentEvent, SubagentThread } from "../runtime/contracts.ts";
import type { DsrSession } from "./session.ts";
import { openTurn } from "./session.ts";
import { DsrError, object } from "./http.ts";
import type { PublicEvent } from "./wire.ts";

export class DsrSubagentBridge implements SubagentBridge {
  private readonly retained: SubagentThread[] = [];
  constructor(readonly owner: DsrSession) {}
  setupTools(scope: Context): void { scope.tools.setExecutor(dsrExecutor(scope, this.owner)); }
  threads(): SubagentThread[] { return this.retained; }
  bindPersistence(id: string): void { this.owner.ctx.sessionReplication.bindThread(id, this.owner.id); }
  async restoreThreads(): Promise<void> {
    for (const transcript of await this.owner.client.listTranscripts(this.owner.abort.signal)) {
      if (transcript.dsh_session_id === this.owner.id) continue;
      if (transcript.parent_dsh_session_id !== this.owner.id) throw new Error("DSRv1 supports one subagent level");
      this.bindPersistence(transcript.dsh_session_id);
      await this.owner.ctx.sessionReplication.ensure(transcript.dsh_session_id);
      const handle = await this.owner.ctx.sessionPersistence.open(SessionId(transcript.dsh_session_id), "read");
      let thread: SubagentThread | undefined;
      try {
        const { events } = await handle.read();
        const descriptor = foldSubagentDescriptor(events);
        const config = dshThreadConfig(events);
        if (!descriptor || !config) { if (events.some(event => event.type === "turn/start")) throw new Error("Subagent has incomplete configuration"); continue; }
        thread = { id: transcript.dsh_session_id, name: config.name ?? descriptor.label ?? transcript.dsh_session_id, inline: config.inline,
          systemPrompt: config.systemPrompt, model: config.model ?? this.owner.config.model, tools: config.tools,
          status: openTurn(events) === undefined ? "idle" : "running" };
      } finally { await handle.close(); }
      if (thread) {
        const state = await this.threadState(thread.id, this.owner.abort.signal);
        if (state.archived || state.status === "terminated") thread.status = "terminated";
        this.retained.push(thread);
      }
    }
  }
  private async threadState(id: string, signal: AbortSignal) {
    const response = await this.owner.client.transport.request({ path: `${this.owner.client.root}/threads/${encodeURIComponent(id)}`, method: "GET", token: this.owner.client.claim.session_token, headers: { "DSR-Protocol": "1" }, signal });
    if (!response.ok) throw new DsrError(response.status, "thread_unavailable");
    const body = object(await response.json());
    const thread = object(body.thread ?? body);
    if (thread.id !== id || !["idle", "running", "terminated"].includes(String(thread.status))) throw new DsrError(0, "invalid_response");
    return thread;
  }
  async validateThread(id: string, signal: AbortSignal, allowRunning = false): Promise<void> {
    const state = await this.threadState(id, signal);
    if (state.archived || state.status === "terminated" || state.status === "running" && !allowRunning) throw new Error("Subagent thread is unavailable");
  }
  async archivedThreads(ids: string[], signal: AbortSignal): Promise<string[]> {
    const result: string[] = [];
    for (const id of ids) { const state = await this.threadState(id, signal); if (state.archived || state.status === "terminated") result.push(id); }
    return result;
  }
  async emit(event: SubagentEvent): Promise<void> {
    const { thread } = event;
    const base = { id: event.kind === "created" ? `${thread.id}_created` : crypto.randomUUID(), session_thread_id: thread.id };
    let output: PublicEvent;
    switch (event.kind) {
      case "created": traceHooks.subagentCreated(thread.id, { name: thread.name, inline: thread.inline === true }); output = { ...base, type: "session.thread_created", dsh_session_id: thread.id, parent_thread_id: this.owner.id,
        agent_snapshot: !thread.inline ? this.owner.config.subagents?.agents?.find(agent => agent.name === thread.name)?.agentSnapshot : { type: "inline", name: thread.name, system: thread.systemPrompt } }; break;
      case "running": output = { ...base, type: "session.thread_status_running" }; break;
      case "idle": output = { ...base, type: "session.thread_status_idle", stop_reason: { type: event.completed ? "end_turn" : "interrupted" } }; break;
      case "terminated": output = { ...base, type: "session.thread_status_terminated" }; break;
      case "message-sent": output = { id: base.id, type: "agent.thread_message_sent", to_session_thread_id: thread.id, content: [{ type: "text", text: event.text }] }; break;
      case "report-received": output = { id: base.id, type: "agent.thread_message_received", from_session_thread_id: thread.id, content: [{ type: "text", text: event.text }] }; break;
    }
    await this.owner.publish(output);
  }
  async onOpen(session: Session): Promise<void> {
    for (const event of session.snapshotEvents()) this.owner.record(event, session);
    await this.owner.checkpoint();
  }
  async afterFlushLog(_session: Session): Promise<void> { await this.owner.checkpoint(); }
  async beforeRequest(): Promise<void> { await this.owner.checkpoint(); }
  onEvent(_id: string, event: Parameters<SubagentBridge["onEvent"]>[1], session: Session): void { this.owner.record(event, session); }
  onStream(id: string, frame: Parameters<SubagentBridge["onStream"]>[1]): void { this.owner.stream(frame, id); }
  toolExecution(id: string) { return this.owner.execution(id); }
}
