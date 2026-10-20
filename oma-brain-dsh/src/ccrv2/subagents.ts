import type { SubagentBridge, SubagentEvent, SubagentThread, ToolExecutionBridge } from "../runtime/contracts.ts";
import { CcrPersistence } from "./persistence.ts";
import type { CcrSession } from "./session.ts";
import type { Payload } from "../json.ts";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";

/** Protocol mapping and durable per-thread execution adapters. */
export class CcrSubagentBridge implements SubagentBridge {
  constructor(readonly session: CcrSession) {}
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
  threads(): SubagentThread[] { return this.session.state.threads ??= []; }
  bindPersistence(threadId: string): void {
    (this.session.ctx.sessionPersistence as CcrPersistence).bindThread(threadId, this.session.id);
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
        this.session.beginThread(thread.id);
        fields = { type: "session.thread_status_running" }; break;
      case "terminated": fields = { type: "session.thread_status_terminated" }; break;
      case "idle":
        this.session.endThread(thread.id, event.completed);
        fields = { type: "session.thread_status_idle", stop_reason: { type: this.session.consumeInterruptedApproval(thread.id) || event.completed ? "end_turn" : "interrupted" } }; break;
      case "message-sent":
        fields = { type: "agent.thread_message_sent", to_session_thread_id: thread.id,
          to_agent_name: thread.name, content: [{ type: "text", text: event.text }] }; break;
      case "report-received":
        this.session.wakeParentFromChildReport();
        fields = { type: "agent.thread_message_received", from_session_thread_id: thread.id,
          from_agent_name: thread.name, content: [{ type: "text", text: event.text }] }; break;
    }
    this.session.state.outbox.push({ ...fields, uuid: crypto.randomUUID(), session_thread_id: thread.id, agent_name: thread.name });
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
      blocked: () => this.session.blocked(),
      approve: (request, signal) => this.session.approve(request, signal, threadId),
      recordApproved: (call) => this.session.recordApproved(call, threadId),
      flushLog: () => this.session.runtime.flushLog(),
      recordDispatch: (callId) => this.session.recordDispatch(callId, threadId),
      recordUnknown: (callId) => this.session.recordUnknown(callId, threadId),
      recordResult: (outcome) => this.session.recordResult(outcome, threadId),
      stop: () => this.session.stop(),
    };
  }
}
