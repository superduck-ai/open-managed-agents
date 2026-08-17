import { useQuery } from '@tanstack/react-query';
import { ArrowDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Pagination, PaginationContent, PaginationItem } from '../../../shared/ui/pagination';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { useWorkspace } from '../../../shared/workspaces/context';
import { listObservabilityAgents, listObservabilityTraces } from '../api';
import { formatDurationMs, formatStatValue, formatTraceTimestamp } from '../format';
import type { ObservabilityFilters } from '../model';
import { ObservabilityStatus } from '../ObservabilityStatus';
import { PanelCard, VIEWED_PANEL_FRAME } from '../PanelCard';
import type { ObservabilityPanel, ObservabilityQuery, ObservabilityTraceListItem, PanelQueryVariables } from '../types';
import { TracePreviewTableCell } from './TraceTextPreview';

const TRACE_PAGE_SIZE = 50;

const TRACE_TREND_PANELS: ObservabilityPanel[] = [
  {
    id: 'trace.trend.count',
    title_key: 'observability.panel.trace.trend.count',
    render_type: 'timeseries',
    unit: 'count',
    query_ref: 'trace.trend.count',
    grid: { x: 0, y: 0, w: 4, h: 2 },
  },
  {
    id: 'trace.trend.interaction_duration',
    title_key: 'observability.panel.trace.trend.interaction_duration',
    render_type: 'timeseries',
    unit: 'duration_ms',
    query_ref: 'trace.trend.interaction_duration',
    grid: { x: 4, y: 0, w: 4, h: 2 },
  },
  {
    id: 'trace.trend.errors',
    title_key: 'observability.panel.trace.trend.errors',
    render_type: 'timeseries',
    unit: 'count',
    query_ref: 'trace.trend.errors',
    grid: { x: 8, y: 0, w: 4, h: 2 },
  },
];

export function TraceListView({
  orgUuid,
  filters,
  variables,
  queries,
  showTrends = true,
  showAgentColumn = false,
  showSessionColumn = true,
  viewedPanelId,
  onToggleView,
  onOpenTrace,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  orgUuid: string;
  filters: ObservabilityFilters;
  variables: PanelQueryVariables;
  queries: ObservabilityQuery[];
  showTrends?: boolean;
  showAgentColumn?: boolean;
  showSessionColumn?: boolean;
  viewedPanelId?: string | null;
  onToggleView?: (panelId: string) => void;
  onOpenTrace: (traceId: string) => void;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  // queryKey 用 trim 后的值，避免只有首尾空白差异的输入打出重复请求。
  const traceIdFilter = filters.traceId.trim();
  const listScopeKey = [
    activeWorkspaceId,
    traceIdFilter,
    filters.status,
    variables.start_time,
    variables.end_time,
    variables.agent_id ?? '',
    variables.session_id ?? '',
    (variables.agent_version ?? []).join(','),
  ].join('|');
  const [offsetState, setOffsetState] = useState({ key: listScopeKey, offset: 0 });
  const offset = offsetState.key === listScopeKey ? offsetState.offset : 0;
  const listQuery = useQuery({
    queryKey: ['observability', 'traces', orgUuid, activeWorkspaceId, variables, traceIdFilter, filters.status, offset],
    queryFn: () =>
      listObservabilityTraces(orgUuid, activeWorkspaceId, {
        ...variables,
        trace_id: traceIdFilter || undefined,
        status: filters.status || undefined,
        offset,
      }),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 30_000,
    // 翻页时保留上一页数据，避免整表闪烁回骨架屏。
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[3] === activeWorkspaceId ? previousData : undefined,
  });
  const agentsQuery = useQuery({
    queryKey: ['observability', 'agents', activeWorkspaceId],
    queryFn: () => listObservabilityAgents(activeWorkspaceId),
    enabled: showAgentColumn && Boolean(activeWorkspaceId),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data?.items]);
  const agentNames = useMemo(
    () => new Map((agentsQuery.data ?? []).map((agent) => [agent.id, agent.label])),
    [agentsQuery.data],
  );
  const specsByRef = useMemo(() => new Map(queries.map((query) => [query.query_ref, query.variables])), [queries]);

  const hasMore = Boolean(listQuery.data?.has_more);
  const foundCount = items.length;
  const foundLabel = hasMore
    ? msg('observability.traces.foundMore', '{count}+', { count: foundCount })
    : msg('observability.traces.found', '{count} traces', { count: foundCount });
  const viewingTrend = Boolean(viewedPanelId && TRACE_TREND_PANELS.some((panel) => panel.id === viewedPanelId));

  return (
    <div className="flex flex-col gap-4">
      {showTrends ? (
        <TraceTrendGrid
          orgUuid={orgUuid}
          variables={variables}
          specsByRef={specsByRef}
          viewedPanelId={viewedPanelId}
          onToggleView={onToggleView}
          onTimeRangeChange={onTimeRangeChange}
          onTimeRangeZoomOut={onTimeRangeZoomOut}
        />
      ) : null}
      {viewingTrend ? null : listQuery.isError ? (
        <ObservabilityStatus
          tone="error"
          size="page"
          title={msg('observability.loadError', 'Couldn’t load observability')}
          actionLabel={msg('observability.retry', 'Retry')}
          onAction={() => void listQuery.refetch()}
        />
      ) : (
        <TraceListResults
          pending={listQuery.isPending}
          items={items}
          foundLabel={foundLabel}
          offset={offset}
          hasMore={hasMore}
          showAgentColumn={showAgentColumn}
          showSessionColumn={showSessionColumn}
          agentNames={agentNames}
          onOffset={(next) => setOffsetState({ key: listScopeKey, offset: next })}
          onOpenTrace={onOpenTrace}
        />
      )}
    </div>
  );
}

function TraceTrendGrid({
  orgUuid,
  variables,
  specsByRef,
  viewedPanelId,
  onToggleView,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  orgUuid: string;
  variables: PanelQueryVariables;
  specsByRef: Map<string, ObservabilityQuery['variables']>;
  viewedPanelId?: string | null;
  onToggleView?: (panelId: string) => void;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const panels = viewedPanelId ? TRACE_TREND_PANELS.filter((panel) => panel.id === viewedPanelId) : TRACE_TREND_PANELS;
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-12">
      {panels.map((panel) => {
        const viewed = viewedPanelId === panel.id;
        return (
          <div
            key={panel.id}
            className={viewed ? `${VIEWED_PANEL_FRAME} md:col-span-12` : 'min-h-[8.5rem] md:col-span-4'}
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

function TraceListResults({
  pending,
  items,
  foundLabel,
  offset,
  hasMore,
  showAgentColumn,
  showSessionColumn,
  agentNames,
  onOffset,
  onOpenTrace,
}: {
  pending: boolean;
  items: ObservabilityTraceListItem[];
  foundLabel: string;
  offset: number;
  hasMore: boolean;
  showAgentColumn: boolean;
  showSessionColumn: boolean;
  agentNames: Map<string, string>;
  onOffset: (offset: number) => void;
  onOpenTrace: (traceId: string) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  if (pending && items.length === 0) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (!pending && items.length === 0) {
    return (
      <div className="flex flex-col gap-2">
        {/* 深翻页翻到空页时保留分页控件，让用户能退回上一页而不是卡死。 */}
        {offset > 0 ? (
          <div className="flex justify-end">
            <TraceListPagination offset={offset} hasMore={hasMore} onOffset={onOffset} />
          </div>
        ) : null}
        <ObservabilityStatus title={msg('observability.traces.emptyTitle', 'No traces')} />
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-foreground">{pending ? '' : foundLabel}</p>
        <TraceListPagination offset={offset} hasMore={hasMore} onOffset={onOffset} />
      </div>
      <Table className="w-full table-fixed text-left text-xs" aria-label={msg('observability.tab.traces', 'Traces')}>
        <TableHeader className="bg-muted/50">
          <TableRow className="hover:bg-transparent">
            <TableHead className="h-8 w-36">{msg('observability.column.trace_id', 'Trace')}</TableHead>
            {showAgentColumn ? (
              <TableHead className="h-8 w-28">{msg('observability.column.agent', 'Agent')}</TableHead>
            ) : null}
            {showSessionColumn ? (
              <TableHead className="h-8 w-32">{msg('observability.column.session_id', 'Session')}</TableHead>
            ) : null}
            <TableHead className="h-8">{msg('observability.column.input', 'Input')}</TableHead>
            <TableHead className="h-8">{msg('observability.column.output', 'Output')}</TableHead>
            <TableHead className="h-8 w-44 whitespace-nowrap">
              <span className="inline-flex items-center gap-1">
                {msg('observability.column.start_time', 'Started')}
                <ArrowDown className="size-3 text-muted-foreground" aria-hidden />
              </span>
            </TableHead>
            <TableHead className="h-8 w-20 whitespace-nowrap">
              {msg('observability.column.duration', 'Duration')}
            </TableHead>
            <TableHead className="h-8 w-20 whitespace-nowrap">{msg('observability.column.tokens', 'Tokens')}</TableHead>
            <TableHead className="h-8 w-20 whitespace-nowrap">{msg('observability.column.status', 'Status')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => (
            <TraceListRow
              key={item.trace_id}
              item={item}
              agentLabel={item.agent_id ? (agentNames.get(item.agent_id) ?? item.agent_id) : '—'}
              showAgentColumn={showAgentColumn}
              showSessionColumn={showSessionColumn}
              timestamp={formatTraceTimestamp(item.start_time, formatters)}
              duration={formatDurationMs(item.duration_ms, formatters)}
              tokens={formatStatValue(item.tokens, 'tokens', formatters)}
              onOpen={() => onOpenTrace(item.trace_id)}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function TraceListRow({
  item,
  agentLabel,
  showAgentColumn,
  showSessionColumn,
  timestamp,
  duration,
  tokens,
  onOpen,
}: {
  item: ObservabilityTraceListItem;
  agentLabel: string;
  showAgentColumn: boolean;
  showSessionColumn: boolean;
  timestamp: string;
  duration: string;
  tokens: string;
  onOpen: () => void;
}) {
  return (
    <TableRow
      className="h-9 cursor-pointer active:bg-accent/25"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onOpen();
        }
      }}
    >
      <TableCell className="max-w-36">
        <span className="block truncate font-mono text-[11px] text-primary">{item.trace_id}</span>
      </TableCell>
      {showAgentColumn ? (
        <TableCell className="max-w-28">
          <span className="block truncate">{agentLabel}</span>
        </TableCell>
      ) : null}
      {showSessionColumn ? (
        <TableCell className="max-w-32">
          <span className="block truncate font-mono text-[11px]">{item.session_id || '—'}</span>
        </TableCell>
      ) : null}
      <TracePreviewTableCell value={item.input} />
      <TracePreviewTableCell value={item.output} />
      <TableCell>
        <span className="block truncate font-mono text-[11px] tabular-nums">{timestamp}</span>
      </TableCell>
      <TableCell>
        <span className="block truncate tabular-nums">{duration}</span>
      </TableCell>
      <TableCell>
        <span className="block truncate tabular-nums">{tokens}</span>
      </TableCell>
      <TableCell>
        <TraceStatusBadge status={item.status} />
      </TableCell>
    </TableRow>
  );
}

function TraceStatusBadge({ status }: { status: ObservabilityTraceListItem['status'] }) {
  const { msg } = useI18n();
  const isError = status === 'error';
  return (
    <Badge
      variant="secondary"
      className={
        isError
          ? 'bg-destructive/10 text-destructive dark:bg-destructive/20'
          : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
      }
    >
      <span
        className={isError ? 'size-1.5 rounded-full bg-destructive' : 'size-1.5 rounded-full bg-emerald-500'}
        aria-hidden
      />
      {isError ? msg('observability.filter.status.error', 'Error') : msg('observability.filter.status.ok', 'OK')}
    </Badge>
  );
}

function TraceListPagination({
  offset,
  hasMore,
  onOffset,
}: {
  offset: number;
  hasMore: boolean;
  onOffset: (offset: number) => void;
}) {
  const { msg } = useI18n();
  if (offset === 0 && !hasMore) {
    return null;
  }
  return (
    <Pagination className="mx-0 w-auto justify-end">
      <PaginationContent>
        <PaginationItem>
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            disabled={offset === 0}
            aria-label={msg('pagination.previousPage', 'Previous page')}
            onClick={() => onOffset(Math.max(0, offset - TRACE_PAGE_SIZE))}
          >
            <ChevronLeft className="size-3.5" aria-hidden />
          </Button>
        </PaginationItem>
        <PaginationItem>
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            disabled={!hasMore}
            aria-label={msg('pagination.nextPage', 'Next page')}
            onClick={() => onOffset(offset + TRACE_PAGE_SIZE)}
          >
            <ChevronRight className="size-3.5" aria-hidden />
          </Button>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  );
}
