import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import { toRecord } from '../utils';
import { sessionEventType } from './sessionTraceModel';

export type ProviderCreditsUsage = {
  totalCredits: number;
  modelCredits: number;
  sandboxRuntimeCredits: number;
};

function numericField(record: Record<string, unknown> | null | undefined, key: string): number | null {
  if (!record) return null;
  const raw = record[key];
  if (typeof raw === 'number' && Number.isFinite(raw)) {
    return raw;
  }
  if (typeof raw === 'string' && raw.trim()) {
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function readCreditsRecord(record: Record<string, unknown> | null | undefined): ProviderCreditsUsage | null {
  if (!record) return null;
  const total = numericField(record, 'total_credits');
  const model = numericField(record, 'model_credits');
  const sandbox = numericField(record, 'sandbox_runtime_credits');
  if (total === null && model === null && sandbox === null) {
    return null;
  }
  const modelCredits = model ?? 0;
  const sandboxRuntimeCredits = sandbox ?? 0;
  return {
    totalCredits: total ?? modelCredits + sandboxRuntimeCredits,
    modelCredits,
    sandboxRuntimeCredits,
  };
}

export function extractProviderCreditsUsage(
  session: SessionApiResponse,
  events: QuickstartSessionEvent[],
): ProviderCreditsUsage | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (sessionEventType(event) !== 'session.usage') {
      continue;
    }
    const eventUsage = readCreditsRecord(toRecord(event.usage)) ?? readCreditsRecord(toRecord(event));
    if (eventUsage) {
      return eventUsage;
    }
  }
  return readCreditsRecord(toRecord(session.usage)) ?? readCreditsRecord(toRecord(session.stats));
}

export function extractSessionCreditsBudget(session: SessionApiResponse): string | null {
  const budget = toRecord(session.budget);
  if (!budget) return null;
  const raw = budget.max_credit_cost;
  if (typeof raw === 'string' && raw.trim()) {
    return raw.trim();
  }
  if (typeof raw === 'number' && Number.isFinite(raw) && raw > 0) {
    return raw.toFixed(2);
  }
  return null;
}
