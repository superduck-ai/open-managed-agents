import { foldSubagentDescriptor } from "@deepseek-ai/dsh-subagent";
import { SessionId, interruptedTurnClosers } from "@deepseek-ai/dsh-session";
import type { SubagentBridge, SubagentEvent, SubagentThread, ToolExecutionBridge } from "../runtime/contracts.ts";
import { CcrHttpError } from "./client.ts";
import { assertPersistenceFormat } from "./recovery.ts";
import type { CcrSession } from "./session.ts";
import type { Payload } from "../json.ts";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";

export class CcrSubagentBridge implements SubagentBridge {
  private readonly retained: SubagentThread[] = [];
  private readonly recoveredCompletions = new Map<string, boolean>();
  constructor(readonly session: CcrSession) {}
  async restoreThreads(): Promise<void> {
    const ids = new Set((await this.session.ctx.sessionPersistence.list())
      .filter(({ header }) => header.origin === "subagent" && header.parentSession === this.session.id)
      .map(({ header }) => header.id));
    const payloads = await this.session.backend.privateEvents(this.session.id, { subagents: true, signal: this.session.abort.signal });
    for (const payload of payloads) {
      assertPersistenceFormat(payload, this.session.id);
      if (payload.type === "dsh_log" && payload.data?.origin === "subagent" && payload.data.parentSession === this.session.id)
        ids.add(SessionId(payload.data.id));
    }
    for (const id of ids) {
      this.bindPersistence(id);
      await this.session.ctx.ccrReplication.ensure(id);
      const handle = await this.session.ctx.sessionPersistence.open(SessionId(id), "read");
      let descriptor;
      let interrupted: boolean;
      let hasTurns: boolean;
      try {
        const { events } = await handle.read();
        hasTurns = events.some((event) => event.type === "turn/start");
        descriptor = foldSubagentDescriptor(events);
        interrupted = interruptedTurnClosers(events).length > 0;
        const lastEnd = events.findLast(event => event.type === "turn/end");
        this.recoveredCompletions.set(id, !interrupted && lastEnd?.type === "turn/end" && lastEnd.data.reason.kind === "completed");
      }
      finally { await handle.close(); }
      if (!this.session.backend.getSubagentThread) throw new Error("Authoritative subagent thread state is unavailable");
      let state;
      try { state = await this.session.backend.getSubagentThread(this.session.id, id, this.session.abort.signal); }
      catch (error) {
        if (!hasTurns && error instanceof CcrHttpError && error.status === 404) continue;
        throw error;
      }
      if (state.id !== id) throw new Error("Invalid authoritative subagent thread identity");
      if (!descriptor || descriptor.mode !== "continuable") throw new Error("Subagent is missing its native descriptor");
      this.retained.push({
        id, name: descriptor.label, inline: descriptor.provider === "oma-inline",
        systemPrompt: descriptor.persona ?? "", model: descriptor.agentModel ?? this.session.config.model,
        tools: descriptor.toolFilter?.allow ? [...descriptor.toolFilter.allow] : undefined,
        status: state.archived || state.status === "terminated" ? "terminated" : state.status === "idle" && !interrupted ? "idle" : "running",
      });
    }
  }
  async validateThread(threadId: string, signal: AbortSignal): Promise<void> {
    if (!this.session.backend.getSubagentThread) throw new Error("Authoritative subagent thread state is unavailable");
    const state = await this.session.backend.getSubagentThread(this.session.id, threadId, signal);
    if (state.id !== threadId) throw new Error("Invalid authoritative subagent thread identity");
    if (state.archived || state.status === "terminated") {
      const thread = this.threads().find((entry) => entry.id === threadId);
      if (thread) thread.status = "terminated";
      throw new Error("Subagent thread is archived");
    }
    if (state.status !== "idle") throw new Error("Subagent thread is running or archived");
  }
  async archivedThreads(threadIds: string[], signal: AbortSignal): Promise<string[]> {
    if (!this.session.backend.getSubagentThread) throw new Error("Authoritative subagent thread state is unavailable");
    const archived: string[] = [];
    for (const id of threadIds) {
      const state = await this.session.backend.getSubagentThread(this.session.id, id, signal);
      if (state.id !== id) throw new Error("Invalid authoritative subagent thread identity");
      if (state.archived || state.status === "terminated") archived.push(id);
    }
    return archived;
  }
  threads(): SubagentThread[] { return this.retained; }
  bindPersistence(threadId: string): void {
    this.session.ctx.ccrReplication.bindThread(threadId, this.session.id);
  }
  async emit(event: SubagentEvent): Promise<void> {
    const { thread } = event;
    let fields: Payload;
    switch (event.kind) {
      case "created":
        fields = { type: "session.thread_created", parent_thread_id: this.session.id };
        if (thread.inline) fields.agent = { type: "inline", name: thread.name, system: thread.systemPrompt };
        else if (thread.agentSnapshot) fields.agent = thread.agentSnapshot;
        break;
      case "running":
        this.recoveredCompletions.delete(thread.id);
        fields = { type: "session.thread_status_running" }; break;
      case "terminated": fields = { type: "session.thread_status_terminated" }; break;
      case "idle": {
        const completed = this.recoveredCompletions.get(thread.id) ?? event.completed;
        this.recoveredCompletions.delete(thread.id);
        fields = { type: "session.thread_status_idle", stop_reason: { type: this.session.consumeInterruptedApproval(thread.id) || completed ? "end_turn" : "interrupted" } }; break;
      }
      case "message-sent":
        fields = { type: "agent.thread_message_sent", to_session_thread_id: thread.id,
          to_agent_name: thread.name, content: [{ type: "text", text: event.text }] }; break;
      case "report-received":
        this.session.wakeParentFromChildReport();
        fields = { type: "agent.thread_message_received", from_session_thread_id: thread.id,
          from_agent_name: thread.name, content: [{ type: "text", text: event.text }] }; break;
    }
    this.session.pendingEvents.push({ ...fields, uuid: event.kind === "created" ? `${thread.id}_created` : crypto.randomUUID(), session_thread_id: thread.id, agent_name: thread.name });
    await this.session.flushEvents();
    await this.session.updateStatus();
  }
  onContextCompacted(threadId: string, tokens?: number): Promise<void> {
    return this.session.contextCompacted(tokens, threadId);
  }
  beforeRequest(): Promise<void> { return this.session.flushEvents(); }
  onEvent(threadId: string, event: SessionEvent): void { this.session.record(event, threadId); }
  onStream(threadId: string, frame: AssistantStreamFrame): void { this.session.stream(frame, threadId); }
  toolExecution(threadId: string): ToolExecutionBridge {
    return {
      approve: (request, signal) => this.session.approve(request, signal, threadId),
      runTool: (callId, execute, readOnly) => this.session.runTool(threadId, callId, execute, readOnly),
      approvalChanged: (name, callId, effective) => this.session.approvalChanged(threadId, name, callId, effective),
    };
  }
}
