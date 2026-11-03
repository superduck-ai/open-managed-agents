export type CaseResultMessage = {
  role: 'user' | 'agent';
  text: string;
};

export type CaseResultTrace = {
  elapsedSeconds?: number;
  rounds: number;
  toolCalls: number;
  inputTokens?: number;
  outputTokens?: number;
  output: string;
  messages: CaseResultMessage[];
};

export function checkpointLines(value: unknown) {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) {
      return [];
    }
    if (trimmed.startsWith('[')) {
      try {
        return checkpointLines(JSON.parse(trimmed));
      } catch {
        return [trimmed];
      }
    }
    return [trimmed];
  }
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => (typeof item === 'string' && item.trim() ? [item.trim()] : []));
}

export function scoreHundredLabel(value: number | undefined) {
  if (typeof value !== 'number' || Number.isNaN(value)) {
    return '';
  }
  if (value >= 0 && value <= 1) {
    return (Math.round(value * 1000) / 10).toFixed(1);
  }
  return String(Math.round(value * 100) / 100);
}

export function detailsSnippet(details: unknown, limit = 180) {
  if (details === undefined || details === null || details === '') {
    return '';
  }
  const text = typeof details === 'string' ? details : JSON.stringify(details);
  if (!text) {
    return '';
  }
  if (text.length <= limit) {
    return text;
  }
  return `${text.slice(0, limit)}…`;
}

export function formatElapsed(seconds: number) {
  const rounded = Math.round(seconds * 100) / 100;
  const text = Number.isInteger(rounded) ? rounded.toFixed(1) : String(rounded);
  return `${text}s`;
}

export function caseResultTrace(session: unknown, events: unknown): CaseResultTrace {
  const messages = traceMessages(events);
  const agentMessages = messages.filter((message) => message.role === 'agent');
  const usage = tokenUsage(session);
  return {
    elapsedSeconds: elapsedSeconds(session),
    rounds: agentMessages.length,
    toolCalls: toolCallCount(events),
    inputTokens: usage?.input,
    outputTokens: usage?.output,
    output: agentMessages[agentMessages.length - 1]?.text ?? '',
    messages,
  };
}

function traceMessages(events: unknown): CaseResultMessage[] {
  if (!Array.isArray(events)) {
    return [];
  }
  return events.flatMap((event) => {
    const role = messageRole(event);
    const text = eventText(event);
    if (!role || !text) {
      return [];
    }
    return [{ role, text }];
  });
}

function messageRole(event: unknown): CaseResultMessage['role'] | '' {
  const type = eventType(event);
  if (type === 'user.message' || type === 'user') {
    return 'user';
  }
  if (type === 'agent.message' || type === 'assistant.message' || type === 'agent') {
    return 'agent';
  }
  return '';
}

function toolCallCount(events: unknown) {
  if (!Array.isArray(events)) {
    return 0;
  }
  return events.filter((event) => {
    const type = eventType(event);
    return type.includes('tool') && !type.includes('result');
  }).length;
}

function eventType(event: unknown) {
  if (!event || typeof event !== 'object' || !('type' in event) || typeof event.type !== 'string') {
    return '';
  }
  return event.type;
}

function eventText(event: unknown) {
  if (!event || typeof event !== 'object') {
    return '';
  }
  const record = event as Record<string, unknown>;
  return textValue(record.content) || textValue(record.text);
}

function textValue(value: unknown): string {
  if (typeof value === 'string') {
    return value.trim();
  }
  if (!Array.isArray(value)) {
    return '';
  }
  return value
    .map((part) => {
      if (typeof part === 'string') {
        return part;
      }
      if (part && typeof part === 'object' && 'text' in part && typeof part.text === 'string') {
        return part.text;
      }
      return '';
    })
    .filter((part) => part.trim().length > 0)
    .join('\n')
    .trim();
}

function elapsedSeconds(session: unknown) {
  const created = timeValue(session, 'created_at');
  const updated = timeValue(session, 'updated_at');
  if (created === undefined || updated === undefined || updated < created) {
    return undefined;
  }
  return (updated - created) / 1000;
}

function tokenUsage(session: unknown) {
  if (!session || typeof session !== 'object' || !('usage' in session)) {
    return undefined;
  }
  const usage = session.usage;
  if (!usage || typeof usage !== 'object') {
    return undefined;
  }
  const record = usage as Record<string, unknown>;
  const input = numeric(record.input_tokens);
  const output = numeric(record.output_tokens);
  if (input === undefined && output === undefined) {
    return undefined;
  }
  return { input, output };
}

function timeValue(session: unknown, key: 'created_at' | 'updated_at') {
  if (!session || typeof session !== 'object' || !(key in session)) {
    return undefined;
  }
  const value = (session as Record<string, unknown>)[key];
  if (typeof value !== 'string') {
    return undefined;
  }
  const time = Date.parse(value);
  return Number.isNaN(time) ? undefined : time;
}

function numeric(value: unknown) {
  return typeof value === 'number' && !Number.isNaN(value) ? value : undefined;
}
