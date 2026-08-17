import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Skeleton } from '../../../shared/ui/skeleton';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { useWorkspace } from '../../../shared/workspaces/context';
import { getObservabilityDashboard } from '../api';
import { ObservabilityFiltersBar } from '../filters';
import {
  defaultObservabilityFilters,
  isApiError,
  panelVariablesFromFilters,
  refreshedTimeRange,
  traceIdFromSearch,
  type ObservabilityFilters,
} from '../model';
import { ObservabilityStatus } from '../ObservabilityStatus';
import { ObservabilityToolbar } from '../toolbar';
import { TraceDetailView } from './TraceDetailView';
import { TraceListView } from './TraceListView';

export function SessionTraceObservability({ sessionId }: { sessionId: string }) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const [filters, setFilters] = useState<ObservabilityFilters>(defaultObservabilityFilters);
  const [openTraceId, setOpenTraceId] = useState<string | null>(() =>
    typeof window === 'undefined'
      ? null
      : traceIdFromSearch(new URLSearchParams(window.location.search).get('trace_id')),
  );
  const variables = useMemo(() => panelVariablesFromFilters(filters, { sessionId }), [filters, sessionId]);
  const dashboardQuery = useQuery({
    queryKey: ['observability', 'dashboard', orgUuid, activeWorkspaceId],
    queryFn: () => getObservabilityDashboard(orgUuid ?? '', activeWorkspaceId),
    enabled: Boolean(orgUuid) && Boolean(activeWorkspaceId),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 5 * 60 * 1000,
  });

  useEffect(() => {
    const url = new URL(window.location.href);
    if (openTraceId) {
      url.searchParams.set('trace_id', openTraceId);
    } else {
      url.searchParams.delete('trace_id');
    }
    window.history.replaceState(window.history.state, '', url);
  }, [openTraceId]);

  if (!orgUuid) {
    return null;
  }
  if (dashboardQuery.isPending) {
    return (
      <div className="flex flex-col gap-2 py-3">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (dashboardQuery.isError) {
    const disabled = isApiError(dashboardQuery.error) && dashboardQuery.error.status === 404;
    return (
      <ObservabilityStatus
        tone={disabled ? 'empty' : 'error'}
        size="page"
        icon={disabled ? Ban : undefined}
        title={
          disabled
            ? msg('observability.disabled.title', 'Observability is not enabled')
            : msg('observability.loadError', 'Couldn’t load observability')
        }
        description={
          disabled
            ? msg(
                'observability.disabled.body',
                'Turn on observability in server configuration to query traces and metrics.',
              )
            : undefined
        }
        actionLabel={disabled ? undefined : msg('observability.retry', 'Retry')}
        onAction={disabled ? undefined : () => void dashboardQuery.refetch()}
      />
    );
  }
  return (
    <TooltipProvider>
      <div className="flex flex-col gap-5 py-3">
        {openTraceId ? (
          <TraceDetailView
            orgUuid={orgUuid}
            traceId={openTraceId}
            variables={variables}
            onClose={() => setOpenTraceId(null)}
          />
        ) : (
          <>
            <ObservabilityToolbar
              filters={filters}
              onChange={setFilters}
              onRefresh={() => {
                setFilters((current) => refreshedTimeRange(current));
                void queryClient.invalidateQueries({ queryKey: ['observability'] });
              }}
            >
              <ObservabilityFiltersBar
                filters={filters}
                onChange={setFilters}
                scope={{ kind: 'session', sessionId }}
                tab="traces"
              />
            </ObservabilityToolbar>
            <TraceListView
              orgUuid={orgUuid}
              filters={filters}
              variables={variables}
              queries={[]}
              showTrends={false}
              showAgentColumn={false}
              showSessionColumn={false}
              onOpenTrace={setOpenTraceId}
            />
          </>
        )}
      </div>
    </TooltipProvider>
  );
}
