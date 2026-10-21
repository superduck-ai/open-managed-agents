import { useQuery } from '@tanstack/react-query';
import { consoleApi, type ApiError } from '../../shared/api/client';

export type RuntimeCostGroup = { id: string; label: string; amount: string };
export type RuntimeCostDay = { date: string; amount: string; groups: RuntimeCostGroup[] };
export type RuntimeCostReport = {
  configured: boolean;
  currency: string;
  total: string;
  daily: RuntimeCostDay[];
  groups: RuntimeCostGroup[];
  models: string[];
  service_accounts: Array<{ id: string; label: string }>;
};

export function runtimeCostLabel(
  isPending: boolean,
  isError: boolean,
  report: RuntimeCostReport | undefined,
  labels: { loading: string; error: string; unconfigured: string },
) {
  if (isError) {
    return labels.error;
  }
  if (isPending || !report) {
    return labels.loading;
  }
  if (report.configured === false) {
    return labels.unconfigured;
  }
  return formatDollars(report.total);
}

export function formatDollars(amount: string) {
  return `$${amount}`;
}

export function runtimeCostRange(range: string, now = new Date()) {
  const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  const endingBefore = addUTCDays(today, 1);
  if (range === 'last-7-days') {
    return { startingOn: formatUTCDate(addUTCDays(today, -6)), endingBefore: formatUTCDate(endingBefore) };
  }
  if (range === 'month-to-date') {
    return {
      startingOn: formatUTCDate(new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), 1))),
      endingBefore: formatUTCDate(endingBefore),
    };
  }
  return { startingOn: formatUTCDate(addUTCDays(today, -29)), endingBefore: formatUTCDate(endingBefore) };
}

export function useRuntimeCost(input: {
  orgUuid: string;
  workspaceId: string;
  model: string;
  groupBy: string;
  range: string;
}) {
  return useQuery({
    queryKey: ['console', 'runtime-cost', input.orgUuid, input.workspaceId, input.model, input.groupBy, input.range],
    enabled: Boolean(input.orgUuid),
    retry: false,
    queryFn: () => {
      const bounds = runtimeCostRange(input.range);
      const params = new URLSearchParams({
        starting_on: bounds.startingOn,
        ending_before: bounds.endingBefore,
        group_by: input.groupBy,
      });
      if (input.workspaceId && input.workspaceId !== 'all-workspaces') {
        params.set('workspace_id', input.workspaceId);
      }
      if (input.model && input.model !== 'all') {
        params.set('model', input.model);
      }
      return consoleApi<RuntimeCostReport>(
        `/api/organizations/${encodeURIComponent(input.orgUuid)}/runtime_cost?${params.toString()}`,
      );
    },
  });
}

export function apiErrorStatus(error: unknown) {
  if (error && typeof error === 'object' && 'status' in error) {
    return (error as ApiError).status;
  }
  return 0;
}

function addUTCDays(date: Date, days: number) {
  return new Date(date.getTime() + days * 24 * 60 * 60 * 1000);
}

function formatUTCDate(date: Date) {
  return date.toISOString().slice(0, 10);
}
