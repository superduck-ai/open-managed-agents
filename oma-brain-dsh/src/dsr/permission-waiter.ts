import type { Decision, Permission } from "./wire.ts";

export class DsrPermissionWaiter {
  private readonly decisions = new Map<string, Decision>();
  private readonly listeners = new Map<string, Set<() => void>>();
  private version = 0;

  resolve(id: string, decision: Decision): void {
    this.decisions.set(id, decision);
    for (const wake of this.listeners.get(id) ?? []) wake();
  }

  reconnect(): void {
    this.version++;
    for (const listeners of this.listeners.values())
      for (const wake of listeners) wake();
  }

  private changed(
    id: string,
    version: number,
    ms: number,
    signal: AbortSignal,
  ): Promise<void> {
    signal.throwIfAborted();
    if (this.decisions.has(id) || version !== this.version)
      return Promise.resolve();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        signal.removeEventListener("abort", cancel);
        const listeners = this.listeners.get(id);
        listeners?.delete(wake);
        if (!listeners?.size) this.listeners.delete(id);
      };
      const wake = () => {
        cleanup();
        resolve();
      };
      const cancel = () => {
        cleanup();
        reject(signal.reason);
      };
      const timer = setTimeout(wake, ms);
      const listeners = this.listeners.get(id) ?? new Set<() => void>();
      listeners.add(wake);
      this.listeners.set(id, listeners);
      signal.addEventListener("abort", cancel, { once: true });
    });
  }

  async wait(
    query: (signal: AbortSignal) => Promise<Permission>,
    signal: AbortSignal,
    budgetMs: number,
  ): Promise<Permission | undefined> {
    const deadline = Date.now() + budgetMs;
    let id: string | undefined;
    for (;;) {
      signal.throwIfAborted();
      const cached = id ? this.decisions.get(id) : undefined;
      if (id && cached)
        return { request_id: id, state: "resolved", decision: cached };
      const remaining = deadline - Date.now();
      if (id && remaining <= 0) return undefined;
      const version = this.version;
      const operation = new AbortController();
      const lifetime = AbortSignal.any([signal, operation.signal]);
      let response: Permission | undefined;
      try {
        response = id
          ? await Promise.race([
              query(lifetime),
              this.changed(id, version, Math.max(0, remaining), lifetime).then(
                () => undefined,
              ),
            ])
          : await query(lifetime);
      } finally {
        operation.abort();
      }
      if (!response) continue;
      if (response.state !== "pending") return response;
      id = response.request_id;
      const wait = deadline - Date.now();
      if (wait <= 0) return undefined;
      await this.changed(id, version, Math.min(10000, wait), signal);
    }
  }
}
