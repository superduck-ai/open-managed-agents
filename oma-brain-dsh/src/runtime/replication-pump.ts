export class ReplicationPump {
  private readonly pending = new Map<string, () => Promise<boolean>>();
  private running?: Promise<void>;

  request(id: string, send: () => Promise<boolean>): void {
    this.pending.set(id, send);
    this.running ??= Promise.resolve()
      .then(() => this.drain())
      .finally(() => {
        this.running = undefined;
        const next = this.pending.entries().next().value;
        if (next) this.request(...next);
      });
  }

  remove(id: string): void {
    this.pending.delete(id);
  }

  idle(): Promise<void> {
    return this.running ?? Promise.resolve();
  }

  private async drain(): Promise<void> {
    while (this.pending.size) {
      const [id, send] = this.pending.entries().next().value!;
      this.pending.delete(id);
      if (await send()) {
        this.pending.delete(id);
        this.pending.set(id, send);
      }
    }
  }
}
