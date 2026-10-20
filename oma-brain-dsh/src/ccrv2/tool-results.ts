import type { SessionEvent } from "@deepseek-ai/dsh-session";

export class CcrToolResults {
  private readonly names = new Map<string, Map<string, string>>();

  record(event: SessionEvent, owner: string): string | undefined {
    if (event.type === "turn/end") {
      this.names.delete(owner);
      return;
    }
    if (event.type === "tool/call") {
      let calls = this.names.get(owner);
      if (!calls) this.names.set(owner, (calls = new Map()));
      calls.set(event.data.callId, event.data.name);
    }
    if (event.type !== "tool/result") return;
    const calls = this.names.get(owner);
    const name = calls?.get(event.data.message.toolCallId);
    calls?.delete(event.data.message.toolCallId);
    return name;
  }
}
