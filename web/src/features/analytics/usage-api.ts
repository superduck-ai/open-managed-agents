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
  search_cost_cents: string;
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

export function money(cents: string, locale: string) {
  const amount = BigInt(cents);
  const fraction = (amount % 100n).toString().padStart(2, '0');
  return `USD ${new Intl.NumberFormat(locale).format(amount / 100n)}.${fraction}`;
}

export function reportRange(value: string, now = new Date()) {
  if (/^\d{4}-(0[1-9]|1[0-2])$/.test(value)) return monthRange(value);
  const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1));
  const start = new Date(end);
  if (value === 'month_to_date') start.setTime(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  else start.setUTCDate(start.getUTCDate() - (value === 'last_7_days' ? 7 : 30));
  return { start: start.toISOString(), end: end.toISOString() };
}
