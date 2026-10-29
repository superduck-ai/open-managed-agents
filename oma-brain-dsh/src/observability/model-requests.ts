import { assistantStreamFirstTokenTime, type AssistantStreamRecord } from "@deepseek-ai/dsh-llm";

interface SessionClock {
  started?: number;
  stepStarted?: number;
  lastSettled?: number;
  model?: string;
}

type Attributes = Record<string, string | number>;

function text(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

export class ModelRequestClock {
  private readonly sessions = new Map<string, SessionClock>();

  private clock(sessionId: string): SessionClock {
    let clock = this.sessions.get(sessionId);
    if (!clock) {
      clock = {};
      this.sessions.set(sessionId, clock);
    }
    return clock;
  }

  started(sessionId: string, time: number): void {
    this.clock(sessionId).started = time;
  }

  forget(sessionId: string): void {
    this.sessions.delete(sessionId);
  }

  observe(sessionId: string, type: string, time: number, body: unknown): Attributes {
    const data = record(body);
    switch (type) {
      case "step/start": {
        const clock = this.clock(sessionId);
        clock.stepStarted = time;
        clock.lastSettled = undefined;
        return {};
      }
      case "step/end": {
        const clock = this.clock(sessionId);
        clock.started = clock.stepStarted = clock.lastSettled = undefined;
        return {};
      }
      case "request/context": {
        const model = text(data.model);
        if (model) this.clock(sessionId).model = model;
        return {};
      }
      case "assistant/message":
      case "assistant/attempt":
        return this.settle(sessionId, time, data);
      default:
        return {};
    }
  }

  private settle(sessionId: string, time: number, data: Record<string, unknown>): Attributes {
    const clock = this.clock(sessionId);
    const startedAt = clock.started ?? clock.lastSettled ?? clock.stepStarted;
    clock.started = undefined;
    clock.lastSettled = time;
    const attributes: Attributes = {};
    if (startedAt !== undefined && startedAt <= time) attributes["oma.model_request.started_at"] = startedAt;
    const stream = Array.isArray(data.stream) ? data.stream as AssistantStreamRecord[] : [];
    const firstToken = assistantStreamFirstTokenTime(stream);
    if (firstToken !== undefined) attributes["oma.model_request.first_token_at"] = firstToken;
    const model = text(record(record(data.message).source).model) ?? clock.model;
    if (model) attributes["gen_ai.request.model"] = model;
    return attributes;
  }
}
