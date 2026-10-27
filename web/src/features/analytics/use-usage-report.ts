import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { useI18n } from '@/shared/i18n';
import { localizedWorkspaceName } from '@/shared/workspaces/display-name';
import { useWorkspace } from '@/shared/workspaces/context';
import { getUsageReport, reportRange, type UsageReport } from './usage-api';

export function useUsageReport(cost: boolean) {
  const scope = useWorkspace();
  const { msg } = useI18n();
  const routeWorkspace = window.location.pathname.match(/^\/workspaces\/([^/]+)\/cost/)?.[1];
  const [filters, setFilters] = useState(() => {
    const params = new URLSearchParams(window.location.search);
    return {
      range: params.get('range') || (params.has('month') ? validMonth(params.get('month')) : 'last_30_days'),
      workspace: params.get('workspace_id') ?? routeWorkspace ?? '',
      model: params.get('model') || '',
      source: params.get('source') || '',
      session: params.get('session_id') || '',
      apiKey: params.get('api_key_id') || '',
    };
  });
  const scopeKey = `${scope.orgUuid}:${scope.activeWorkspaceId}:${routeWorkspace}`;
  const previousScope = useRef(scopeKey);
  useEffect(() => {
    if (previousScope.current === scopeKey) return;
    previousScope.current = scopeKey;
    setFilters((current) => ({
      ...current,
      workspace: routeWorkspace || '',
      model: '',
      apiKey: '',
      session: '',
    }));
  }, [scopeKey, routeWorkspace]);
  const range = reportRange(filters.range);
  const params = new URLSearchParams({
    starting_at: range.start,
    ending_at: range.end,
    timezone: 'UTC',
  });
  params.set('workspace_id', filters.workspace);
  if (filters.model) params.set('model', filters.model);
  if (filters.source) params.set('source', filters.source);
  if (filters.session) params.set('session_id', filters.session);
  if (filters.apiKey) params.set('api_key_id', filters.apiKey);
  useEffect(() => {
    const url = new URL(window.location.href);
    const next = new URLSearchParams(params);
    next.set('range', filters.range);
    url.search = next.toString();
    window.history.replaceState(window.history.state, '', url);
  }, [params.toString(), filters.range]);
  const query = useQuery<UsageReport>({
    queryKey: ['analytics-usage', scope.orgUuid, cost, params.toString()],
    enabled: Boolean(scope.orgUuid),
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[1] === scope.orgUuid ? previous : undefined),
    queryFn: ({ signal }) => getUsageReport(scope.orgUuid!, params, cost, signal),
  });
  function change(key: keyof typeof filters, value: string) {
    setFilters((current) => ({
      ...current,
      [key]: value,
      ...(key === 'workspace' ? { model: '', apiKey: '', session: '' } : {}),
    }));
  }
  return {
    scope,
    workspaces:
      query.data?.options.workspaces.map((workspace) => ({
        ...workspace,
        label: localizedWorkspaceName(
          scope.workspaces.find((item) => item.id === workspace.value)?.name ?? workspace.label,
          msg,
        ),
      })) ??
      scope.workspaces.map((workspace) => ({
        value: workspace.id,
        label: localizedWorkspaceName(workspace.name, msg),
      })),
    filters,
    change,
    query,
  };
}

function validMonth(value: string | null) {
  return value && /^\d{4}-(0[1-9]|1[0-2])$/.test(value) ? value : new Date().toISOString().slice(0, 7);
}
