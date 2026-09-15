import { CircleHelp, Maximize2, Minimize2, RefreshCw } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '../../shared/ui/card';
import { Skeleton } from '../../shared/ui/skeleton';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../shared/ui/tooltip';
import { ICON_CROSSFADE_CLASS } from './chrome';
import { isPanelDataEmpty, pickDeclaredVariables } from './model';
import { ObservabilityStatus } from './ObservabilityStatus';
import { CategoricalPanel } from './renderers/CategoricalPanel';
import { MultiStatPanel } from './renderers/MultiStatPanel';
import { StatPanel } from './renderers/StatPanel';
import { TablePanel } from './renderers/TablePanel';
import { TimeseriesPanel } from './renderers/TimeseriesPanel';
import type {
  CategoricalPanelData,
  MultistatPanelData,
  ObservabilityPanel,
  ObservabilityVariableSpec,
  PanelQueryVariables,
  StatPanelData,
  TablePanelData,
  TimeseriesPanelData,
} from './types';
import { usePanelQuery } from './usePanelQuery';

export const VIEWED_PANEL_FRAME = 'h-[calc(100dvh-16rem)] min-h-[28rem]';

export function PanelCard({
  orgUuid,
  panel,
  variables,
  variableSpecs,
  viewed = false,
  onToggleView,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  orgUuid: string;
  panel: ObservabilityPanel;
  variables: PanelQueryVariables;
  variableSpecs?: ObservabilityVariableSpec[];
  viewed?: boolean;
  onToggleView?: () => void;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const { msg } = useI18n();
  const bound = pickDeclaredVariables(variableSpecs, variables);
  const query = usePanelQuery(orgUuid, panel.query_ref, bound);
  const empty = isPanelDataEmpty(query.data, panel.render_type);
  const title = msg(panel.title_key, panel.id);
  const help = msg(`${panel.title_key}.help`, '');
  const viewLabel = viewed ? msg('observability.panel.exitView', 'Exit view') : msg('observability.panel.view', 'View');

  const actionsVisible = viewed || query.isError || query.isFetching;

  return (
    <Card size="sm" className={cn('relative h-full min-h-0', viewed && VIEWED_PANEL_FRAME)}>
      <CardHeader className="gap-1 has-data-[slot=card-action]:grid-cols-1">
        <CardTitle className="flex min-w-0 items-center gap-1">
          <span className="truncate">{title}</span>
          {help ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    className="text-muted-foreground"
                    aria-label={msg('observability.panel.info', 'About this panel')}
                  >
                    <CircleHelp className="size-3.5" aria-hidden />
                  </Button>
                }
              />
              <TooltipContent className="max-w-xs text-left">{help}</TooltipContent>
            </Tooltip>
          ) : null}
        </CardTitle>
        <CardAction
          className={cn(
            'absolute top-(--card-spacing) right-(--card-spacing) z-10 flex items-center gap-0.5 rounded-md bg-card/90',
            'transition-[opacity] duration-150 ease-out',
            actionsVisible
              ? 'opacity-100'
              : 'pointer-events-none opacity-0 group-hover/card:pointer-events-auto group-hover/card:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100',
          )}
        >
          {onToggleView ? (
            <Button type="button" variant="ghost" size="icon-xs" aria-label={viewLabel} onClick={onToggleView}>
              <ViewModeIcon viewed={viewed} />
            </Button>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={msg('observability.panel.refresh', 'Refresh panel')}
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className={cn('size-3.5', query.isFetching && 'animate-spin')} aria-hidden />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col">
        <PanelCardContent
          panel={panel}
          variables={variables}
          viewed={viewed}
          pending={query.isPending}
          error={query.isError}
          empty={empty}
          data={query.data?.data}
          onRetry={() => void query.refetch()}
          onTimeRangeChange={onTimeRangeChange}
          onTimeRangeZoomOut={onTimeRangeZoomOut}
        />
      </CardContent>
    </Card>
  );
}

function ViewModeIcon({ viewed }: { viewed: boolean }) {
  return (
    <span className="relative inline-flex size-3.5 items-center justify-center">
      <Minimize2
        className={cn(
          'absolute size-3.5',
          ICON_CROSSFADE_CLASS,
          viewed ? 'scale-100 opacity-100 blur-0' : 'scale-[0.25] opacity-0 blur-[4px]',
        )}
        aria-hidden
      />
      <Maximize2
        className={cn(
          'size-3.5',
          ICON_CROSSFADE_CLASS,
          viewed ? 'scale-[0.25] opacity-0 blur-[4px]' : 'scale-100 opacity-100 blur-0',
        )}
        aria-hidden
      />
    </span>
  );
}

function PanelCardContent({
  panel,
  variables,
  viewed,
  pending,
  error,
  empty,
  data,
  onRetry,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  panel: ObservabilityPanel;
  variables: PanelQueryVariables;
  viewed: boolean;
  pending: boolean;
  error: boolean;
  empty: boolean;
  data: unknown;
  onRetry: () => void;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const { msg } = useI18n();
  if (pending) {
    return <Skeleton className="min-h-16 w-full flex-1" />;
  }
  if (error) {
    return (
      <ObservabilityStatus
        tone="error"
        title={msg('observability.panel.loadError', 'Couldn’t load this panel')}
        actionLabel={msg('observability.retry', 'Retry')}
        onAction={onRetry}
      />
    );
  }
  if (empty || data == null) {
    return <ObservabilityStatus title={msg('observability.empty', 'No data')} />;
  }
  return (
    <PanelBody
      panel={panel}
      data={data}
      viewed={viewed}
      start={typeof variables.start_time === 'string' ? variables.start_time : undefined}
      end={typeof variables.end_time === 'string' ? variables.end_time : undefined}
      onTimeRangeChange={onTimeRangeChange}
      onTimeRangeZoomOut={onTimeRangeZoomOut}
    />
  );
}

function PanelBody({
  panel,
  data,
  viewed,
  start,
  end,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  panel: ObservabilityPanel;
  data: unknown;
  viewed: boolean;
  start?: string;
  end?: string;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  if (panel.render_type === 'stat') {
    return (
      <div className={viewed ? 'flex h-full items-center' : undefined}>
        <StatPanel unit={panel.unit} data={data as StatPanelData} />
      </div>
    );
  }
  if (panel.render_type === 'timeseries') {
    return (
      <div className={viewed ? 'flex min-h-0 flex-1 flex-col' : undefined}>
        <TimeseriesPanel
          unit={panel.unit}
          options={panel.options}
          data={data as TimeseriesPanelData}
          fillHeight={viewed}
          start={start}
          end={end}
          onTimeRangeChange={onTimeRangeChange}
          onTimeRangeZoomOut={onTimeRangeZoomOut}
        />
      </div>
    );
  }
  if (panel.render_type === 'categorical') {
    return (
      <div className={viewed ? 'flex min-h-0 flex-1 flex-col' : undefined}>
        <CategoricalPanel
          unit={panel.unit}
          data={data as CategoricalPanelData}
          fillHeight={viewed}
          chart={panel.options?.chart}
        />
      </div>
    );
  }
  if (panel.render_type === 'multistat') {
    return (
      <div className={viewed ? 'flex h-full items-center' : undefined}>
        <MultiStatPanel
          unit={panel.unit}
          options={panel.options}
          data={data as MultistatPanelData}
          start={start}
          end={end}
        />
      </div>
    );
  }
  return (
    <div className={viewed ? 'min-h-0 flex-1 overflow-auto' : undefined}>
      <TablePanel options={panel.options} data={data as TablePanelData} />
    </div>
  );
}
