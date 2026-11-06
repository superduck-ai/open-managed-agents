import SessionStore, { type SessionId } from "@deepseek-ai/dsh-session";

export default class TranscriptSessions extends SessionStore {
  override prepare(
    id?: SessionId,
    options?: Parameters<SessionStore["prepare"]>[1],
  ) {
    if (id !== undefined && !this.get(id)) {
      const prepared = this.ctx
        .get("transcriptStore")
        ?.takePrepared(id, options, this.messageProjections);
      if (prepared) return prepared;
    }
    return super.prepare(id, options);
  }
}
