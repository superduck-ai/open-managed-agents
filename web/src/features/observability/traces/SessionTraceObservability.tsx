import { useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { useWorkspace } from '../../../shared/workspaces/context';
import { ObservabilityFiltersBar } from '../filters';
import {
  defaultObservabilityFilters,
  panelVariablesFromFilters,
  refreshedTimeRange,
  type ObservabilityFilters,
} from '../model';
import { ObservabilityToolbar } from '../toolbar';
import { TraceDetailView } from './TraceDetailView';
import { TraceListView } from './TraceListView';

export function SessionTraceObservability({ sessionId }: { sessionId: string }) {
  const { orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const [filters, setFilters] = useState<ObservabilityFilters>(defaultObservabilityFilters);
  const [openTraceId, setOpenTraceId] = useState<string | null>(null);
  const variables = useMemo(() => panelVariablesFromFilters(filters, { sessionId }), [filters, sessionId]);
  if (!orgUuid) {
    return null;
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
