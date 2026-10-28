import { consoleApi } from '@/shared/api/client';

export type UsageMetrics = {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_input_tokens: number;
  cache_creation_input_tokens: number;
  cache_write_5m_tokens: number;
  cache_write_1h_tokens: number;
  web_search_requests: number;
  model_cost_cents: string;
  model_cost?: string;
  search_cost_cents: string;
  search_cost?: string;
  model_costs?: { name: string; cost: string }[];
  tool_costs?: { name: string; cost: string }[];
  subtotal_cents: string;
  unpriced_requests: number;
  incomplete_requests: number;
};
export type UsageBucket = { key: string; metrics: UsageMetrics };
export type UsageReport = {
  currency: string;
  cost_basis: string;
  billing_version: string;
  included_components: string[];
  excluded_components: string[];
  as_of: string;
  starting_at: string;
  ending_at: string;
  timezone: string;
  summary: UsageMetrics;
  daily: UsageBucket[];
  options: {
    models: string[];
    api_keys: string[];
    workspaces: { value: string; label: string }[];
    first_seen: string | null;
    sources: string[];
  };
};

export function getUsageReport(orgUuid: string, params: URLSearchParams, cost: boolean, signal: AbortSignal) {
  return consoleApi<UsageReport>(
    `/api/organizations/${encodeURIComponent(orgUuid)}/${cost ? 'usage_cost' : 'usage_activities'}?${params}`,
    {
      context: { organizationUuid: orgUuid },
      signal,
    },
  );
}

export function monthRange(month: string) {
  const [year, value] = month.split('-').map(Number);
  return {
    start: new Date(Date.UTC(year, value - 1, 1)).toISOString(),
    end: new Date(Date.UTC(year, value, 1)).toISOString(),
  };
}

export function monthOptions(firstSeen: string | null, locale: string, now = new Date()) {
  const current = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  const first = firstSeen ? new Date(firstSeen) : current;
  const earliest = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth(), 1));
  const options: { value: string; label: string }[] = [];
  for (const date = new Date(current); date >= earliest; date.setUTCMonth(date.getUTCMonth() - 1)) {
    options.push({
      value: date.toISOString().slice(0, 7),
      label: new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'long', timeZone: 'UTC' }).format(date),
    });
  }
  return options;
}

export function modelCostDollars(metrics: Pick<UsageMetrics, 'model_cost' | 'model_cost_cents'>) {
  if (metrics.model_cost) return metrics.model_cost;
  return centsToDollars(metrics.model_cost_cents);
}

export function toolCostDollars(metrics: Pick<UsageMetrics, 'search_cost' | 'search_cost_cents'>) {
  if (metrics.search_cost) return metrics.search_cost;
  return centsToDollars(metrics.search_cost_cents);
}

export function costTotalLabel(modelDollars: string, toolDollars: string, runtimeDollars: string, locale: string) {
  return formatMicros(
    dollarsToMicros(modelDollars) + dollarsToMicros(toolDollars) + dollarsToMicros(runtimeDollars),
    locale,
  );
}

export function formatCost(amount: string, locale: string) {
  return formatMicros(dollarsToMicros(amount), locale);
}

function centsToDollars(cents: string) {
  const amount = BigInt(cents || '0');
  const whole = amount / 100n;
  const fraction = (amount < 0n ? -amount : amount) % 100n;
  return `${amount < 0n ? '-' : ''}${whole}.${fraction.toString().padStart(2, '0')}`;
}

function centsToMicros(cents: string) {
  return BigInt(cents || '0') * 10000n;
}

function dollarsToMicros(amount: string) {
  const trimmed = amount.trim();
  const negative = trimmed.startsWith('-');
  const value = negative ? trimmed.slice(1) : trimmed;
  if (!/^\d+(\.\d+)?$/.test(value)) return 0n;
  const [whole, fraction = ''] = value.split('.');
  const digits = `${fraction}0000000`;
  const micros = BigInt(whole) * 1000000n + BigInt(digits.slice(0, 6)) + (digits[6]! >= '5' ? 1n : 0n);
  return negative ? -micros : micros;
}

function formatMicros(micros: bigint, locale: string) {
  const negative = micros < 0n;
  const absolute = negative ? -micros : micros;
  const whole = new Intl.NumberFormat(locale).format(absolute / 1000000n);
  const fraction = (absolute % 1000000n).toString().padStart(6, '0');
  return `${negative ? '-' : ''}$${whole}.${fraction}`;
}

export function money(cents: string, locale: string) {
  return formatMicros(centsToMicros(cents), locale);
}

export function reportRange(value: string, now = new Date()) {
  if (/^\d{4}-(0[1-9]|1[0-2])$/.test(value)) return monthRange(value);
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1));
  const start = new Date(end);
  if (value === 'month_to_date') start.setTime(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  else start.setUTCDate(start.getUTCDate() - (value === 'last_7_days' ? 7 : 30));
  return { start: start.toISOString(), end: end.toISOString() };
}
