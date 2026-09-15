import { Skeleton } from '../../shared/ui/skeleton';
import { PanelCard, VIEWED_PANEL_FRAME } from './PanelCard';
import type { ObservabilityPanel, ObservabilityQuery, PanelQueryVariables } from './types';

export function PanelGridSkeleton({ count = 6 }: { count?: number }) {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-12">
      {Array.from({ length: count }, (_, index) => (
        <Skeleton key={index} className="min-h-[8.5rem] md:col-span-4" />
      ))}
    </div>
  );
}

export function PanelGrid({
  orgUuid,
  panels,
  queries,
  variables,
  viewedPanelId,
  onToggleView,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  orgUuid: string;
  panels: ObservabilityPanel[];
  queries: ObservabilityQuery[];
  variables: PanelQueryVariables;
  viewedPanelId?: string | null;
  onToggleView?: (panelId: string) => void;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const specsByRef = new Map(queries.map((query) => [query.query_ref, query.variables]));
  const visible = viewedPanelId ? panels.filter((panel) => panel.id === viewedPanelId) : panels;
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-12 md:grid-flow-dense">
      {visible.map((panel) => {
        const viewed = viewedPanelId === panel.id;
        return (
          <div
            key={panel.id}
            className={viewed ? VIEWED_PANEL_FRAME : 'min-h-[8.5rem]'}
            style={{
              gridColumn: `span ${viewed ? 12 : Math.min(12, Math.max(1, panel.grid.w))}`,
              gridRow: viewed ? 'span 1' : `span ${Math.max(1, panel.grid.h)}`,
            }}
          >
            <PanelCard
              orgUuid={orgUuid}
              panel={panel}
              variables={variables}
              variableSpecs={specsByRef.get(panel.query_ref)}
              viewed={viewed}
              onToggleView={onToggleView ? () => onToggleView(panel.id) : undefined}
              onTimeRangeChange={onTimeRangeChange}
              onTimeRangeZoomOut={onTimeRangeZoomOut}
            />
          </div>
        );
      })}
    </div>
  );
}
