import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Copy, GripVertical } from 'lucide-react';
import { useMemo, useState, type ReactNode } from 'react';
import { toast } from 'sonner';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { copyText } from '../../../shared/lib/clipboard';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { PRESS_SCALE_CLASS } from '../chrome';
import { Input } from '../../../shared/ui/input';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { useWorkspace } from '../../../shared/workspaces/context';
import { getObservabilityTrace, listObservabilityAgents } from '../api';
import { formatDurationMs } from '../format';
import { ObservabilityStatus } from '../ObservabilityStatus';
import type { ObservabilitySpan, PanelQueryVariables } from '../types';
import { SpanDetailPanel } from './SpanDetailPanel';
import { TraceIOPanels } from './TraceTextPreview';
import { TraceWaterfall } from './TraceWaterfall';
import { resolveAgentName, serviceColorMap, spanAgentID, spanColorKey } from './traceLayout';
import { buildTraceTree, flattenTraceTree, tracePreview, traceStats } from './traceTree';
import { useColumnResize } from './useColumnResize';

const CALL_TREE_TAB = 'call-tree';

export function TraceDetailView({
  orgUuid,
  traceId,
  variables,
  onClose,
}: {
  orgUuid: string;
  traceId: string;
  variables: PanelQueryVariables;
  onClose?: () => void;
}) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const { width: leftWidth, onResizeStart } = useColumnResize();
  const detailQuery = useQuery({
    queryKey: ['observability', 'trace', orgUuid, activeWorkspaceId, traceId, variables],
    queryFn: () => getObservabilityTrace(orgUuid, activeWorkspaceId, traceId, variables),
    enabled: Boolean(activeWorkspaceId),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const agentsQuery = useQuery({
    queryKey: ['observability', 'agents', activeWorkspaceId],
    queryFn: () => listObservabilityAgents(activeWorkspaceId),
    enabled: Boolean(activeWorkspaceId),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const spans = useMemo(() => detailQuery.data?.spans ?? [], [detailQuery.data?.spans]);
  const preview = useMemo(() => tracePreview(spans), [spans]);
  const rows = useMemo(() => flattenTraceTree(buildTraceTree(spans)), [spans]);
  const selected = rows.find((row) => row.span_id === selectedSpanId) ?? null;
  const colors = useMemo(() => serviceColorMap(rows.map(spanColorKey)), [rows]);
  const agentNames = useMemo(
    () => new Map((agentsQuery.data ?? []).map((agent) => [agent.id, agent.label])),
    [agentsQuery.data],
  );
  const showTimeline = !selected;

  if (detailQuery.isPending) {
    return (
      <TraceDetailFrame onClose={onClose}>
        <Skeleton className="h-[calc(100vh-14rem)] min-h-[32rem] w-full" />
      </TraceDetailFrame>
    );
  }
  if (detailQuery.isError) {
    return (
      <TraceDetailFrame onClose={onClose}>
        <ObservabilityStatus
          tone="error"
          size="page"
          title={msg('observability.loadError', 'Couldn’t load observability')}
          actionLabel={msg('observability.retry', 'Retry')}
          onAction={() => void detailQuery.refetch()}
        />
      </TraceDetailFrame>
    );
  }
  if (!spans.length) {
    return (
      <TraceDetailFrame onClose={onClose}>
        <ObservabilityStatus title={msg('observability.trace.emptySpansTitle', 'No spans')} />
      </TraceDetailFrame>
    );
  }

  return (
    <Tabs value={CALL_TREE_TAB} className="flex min-h-0 flex-col gap-3">
      <div className="flex items-center gap-2">
        {onClose ? (
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            aria-label={msg('observability.trace.back', 'Back')}
            onClick={onClose}
          >
            <ArrowLeft className="size-3.5" aria-hidden />
          </Button>
        ) : null}
        <TabsList variant="line">
          <TabsTrigger value={CALL_TREE_TAB}>{msg('observability.trace.callTree', 'Call tree')}</TabsTrigger>
        </TabsList>
      </div>
      <TraceStatsRow spans={spans} traceId={traceId} />
      <TraceIOPanels
        input={preview.input}
        output={preview.output}
        className="grid-cols-1 md:grid-cols-2"
        contentClassName="max-h-36"
      />
      <Input
        value={query}
        onChange={(event) => setQuery(event.currentTarget.value)}
        placeholder={msg('observability.trace.searchPlaceholder', 'Search the call tree')}
        aria-label={msg('observability.trace.search', 'Search call tree')}
      />
      <TabsContent value={CALL_TREE_TAB} className="min-h-0">
        <div className="relative flex h-[calc(100vh-14rem)] min-h-[32rem] overflow-hidden rounded-lg border border-border">
          <div className="flex min-h-0 min-w-0 flex-col" style={{ width: showTimeline ? '100%' : leftWidth }}>
            <TraceWaterfall
              spans={spans}
              query={query}
              selectedSpanId={selected?.span_id ?? null}
              leftWidth={leftWidth}
              showTimeline={showTimeline}
              onSelectSpan={setSelectedSpanId}
            />
          </div>
          {selected ? (
            <div className="flex min-h-0 min-w-0 flex-1 border-l border-border">
              <SpanDetailPanel
                span={selected}
                color={colors.get(spanColorKey(selected)) ?? 'var(--chart-1)'}
                agentName={resolveAgentName(spanAgentID(selected), agentNames)}
                onClose={() => setSelectedSpanId(null)}
              />
            </div>
          ) : null}
          {selected ? (
            <>
              <div
                className="absolute top-0 bottom-0 z-20 w-px cursor-col-resize bg-border hover:bg-primary"
                style={{ left: leftWidth }}
                onPointerDown={onResizeStart}
              />
              <button
                type="button"
                className="absolute top-1.5 z-30 flex size-5 -translate-x-1/2 cursor-col-resize items-center justify-center rounded-full bg-primary text-primary-foreground"
                style={{ left: leftWidth }}
                aria-label={msg('observability.trace.resize', 'Resize')}
                onPointerDown={onResizeStart}
              >
                <GripVertical className="size-3" aria-hidden />
              </button>
            </>
          ) : null}
        </div>
      </TabsContent>
    </Tabs>
  );
}

type TraceStatItem = {
  label: string;
  value: string;
  destructive?: boolean;
};

function TraceStatsRow({ spans, traceId }: { spans: ObservabilitySpan[]; traceId: string }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const stats = useMemo(() => traceStats(spans), [spans]);
  const items: TraceStatItem[] = [
    { label: msg('observability.trace.duration', 'Duration'), value: formatDurationMs(stats.durationMs, formatters) },
    {
      label: msg('observability.trace.header.totalTokens', 'Total tokens'),
      value: formatters.number(stats.totalTokens),
    },
    { label: msg('observability.column.input_tokens', 'Input tokens'), value: formatters.number(stats.inputTokens) },
    { label: msg('observability.column.output_tokens', 'Output tokens'), value: formatters.number(stats.outputTokens) },
  ];
  if (stats.cacheHitRate !== null) {
    items.push({
      label: msg('observability.trace.header.cacheHit', 'Cache hit'),
      value: `${formatters.number(stats.cacheHitRate, { maximumFractionDigits: 1 })}%`,
    });
  }
  items.push(
    { label: msg('observability.trace.header.llmCalls', 'LLM calls'), value: formatters.number(stats.llmCallCount) },
    { label: msg('observability.trace.header.toolCalls', 'Tool calls'), value: formatters.number(stats.toolCallCount) },
    { label: msg('observability.trace.header.spans', 'Spans'), value: formatters.number(stats.spanCount) },
  );
  if (stats.errorCount > 0) {
    items.push({
      label: msg('observability.column.errors', 'Errors'),
      value: formatters.number(stats.errorCount),
      destructive: true,
    });
  }
  return (
    <dl className="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs">
      <div className="flex items-baseline gap-1.5">
        <dt className="text-muted-foreground">{msg('observability.trace.header.traceId', 'Trace ID')}</dt>
        <dd>
          <button
            type="button"
            className={cn(
              'inline-flex max-w-48 items-center gap-1 font-mono text-foreground hover:text-primary',
              PRESS_SCALE_CLASS,
            )}
            title={traceId}
            aria-label={msg('observability.trace.copyTraceId', 'Copy trace ID')}
            onClick={() => {
              void copyText(traceId).then(() => toast.success(msg('common.copied', 'Copied')));
            }}
          >
            <span className="truncate">{traceId}</span>
            <Copy className="size-3 shrink-0 opacity-60" aria-hidden />
          </button>
        </dd>
      </div>
      {items.map((item) => (
        <div key={item.label} className="flex items-baseline gap-1.5">
          <dt className="text-muted-foreground">{item.label}</dt>
          <dd className={cn('font-medium tabular-nums', item.destructive && 'text-destructive')}>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function TraceDetailFrame({ onClose, children }: { onClose?: () => void; children: ReactNode }) {
  const { msg } = useI18n();
  return (
    <div className="flex min-h-0 flex-col gap-3">
      {onClose ? (
        <Button
          type="button"
          size="icon-xs"
          variant="ghost"
          aria-label={msg('observability.trace.back', 'Back')}
          onClick={onClose}
        >
          <ArrowLeft className="size-3.5" aria-hidden />
        </Button>
      ) : null}
      {children}
    </div>
  );
}
