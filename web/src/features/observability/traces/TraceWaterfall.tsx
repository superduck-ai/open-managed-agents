import { AlertCircle } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { PRESS_SCALE_CLASS } from '../chrome';
import { formatDurationMs, formatWaterfallTick } from '../format';
import type { ObservabilitySpan } from '../types';
import {
  connectorHeight,
  connectorOffset,
  durationLabelPlacement,
  serviceColorMap,
  showTreeConnector,
  spanColorKey,
  spanIdentity,
  spanTokenTotal,
  TRACE_BAR_HEIGHT,
  TRACE_COLLAPSE_WIDTH,
  TRACE_ROW_HEIGHT,
  TRACE_TREE_GAP,
  treeIndentLeft,
} from './traceLayout';
import {
  buildTraceTree,
  filterCallTreeRows,
  flattenTraceTree,
  spanDisplayName,
  waterfallTickMs,
  waterfallTotalMs,
  type TraceTreeRow,
} from './traceTree';

export function TraceWaterfall({
  spans,
  query = '',
  selectedSpanId,
  leftWidth,
  showTimeline,
  onSelectSpan,
}: {
  spans: ObservabilitySpan[];
  query?: string;
  selectedSpanId: string | null;
  leftWidth: number;
  showTimeline: boolean;
  onSelectSpan: (spanId: string) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const timelineRef = useRef<HTMLDivElement>(null);
  const [timelineWidth, setTimelineWidth] = useState(0);
  const tree = useMemo(() => buildTraceTree(spans), [spans]);
  const searching = Boolean(query.trim());
  const rows = useMemo(() => {
    const flat = flattenTraceTree(tree, 0, searching ? new Set() : collapsed);
    return filterCallTreeRows(flat, query);
  }, [collapsed, query, searching, tree]);
  const totalMs = waterfallTotalMs(rows);
  const ticks = waterfallTickMs(totalMs);
  // 配色按完整树分配（与 TraceDetailView 一致），折叠或搜索过滤时颜色不漂移。
  const colors = useMemo(() => serviceColorMap(flattenTraceTree(tree).map(spanColorKey)), [tree]);

  useEffect(() => {
    const node = timelineRef.current;
    if (!node || !showTimeline) {
      return;
    }
    const observer = new ResizeObserver(() => setTimelineWidth(node.clientWidth));
    observer.observe(node);
    setTimelineWidth(node.clientWidth);
    return () => observer.disconnect();
  }, [showTimeline]);

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="sticky top-0 z-10 flex h-[30px] shrink-0 items-center border-b border-border bg-muted/40 text-xs text-muted-foreground">
        <div className="flex h-full items-center px-2" style={{ width: showTimeline ? leftWidth : '100%' }}>
          {msg('observability.trace.operation', 'Operation')}
        </div>
        {showTimeline ? (
          <div ref={timelineRef} className="relative h-full min-w-0 flex-1">
            {ticks.map((tick, index) => (
              <span
                key={tick}
                className={cn(
                  'absolute top-1/2 -translate-y-1/2 text-[11px] tabular-nums',
                  index === 0 ? 'left-3' : '',
                  index === ticks.length - 1 ? 'right-1' : '',
                  index > 0 && index < ticks.length - 1 ? '-translate-x-1/2' : '',
                )}
                style={
                  index === 0 || index === ticks.length - 1
                    ? undefined
                    : { left: `${(index / (ticks.length - 1)) * 100}%` }
                }
              >
                {formatWaterfallTick(tick, formatters)}
              </span>
            ))}
          </div>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {rows.map((row) => (
          <WaterfallRow
            key={row.span_id}
            row={row}
            color={colors.get(spanColorKey(row)) ?? 'var(--chart-1)'}
            selected={selectedSpanId === row.span_id}
            dimmed={Boolean(selectedSpanId && selectedSpanId !== row.span_id)}
            collapsed={collapsed.has(row.span_id)}
            leftWidth={leftWidth}
            showTimeline={showTimeline}
            timelineWidth={timelineWidth}
            onSelect={() => onSelectSpan(row.span_id)}
            onToggle={() => setCollapsed((current) => toggleCollapsed(current, row.span_id))}
          />
        ))}
      </div>
    </div>
  );
}

function WaterfallRow({
  row,
  color,
  selected,
  dimmed,
  collapsed,
  leftWidth,
  showTimeline,
  timelineWidth,
  onSelect,
  onToggle,
}: {
  row: TraceTreeRow;
  color: string;
  selected: boolean;
  dimmed: boolean;
  collapsed: boolean;
  leftWidth: number;
  showTimeline: boolean;
  timelineWidth: number;
  onSelect: () => void;
  onToggle: () => void;
}) {
  return (
    <div
      className={cn(
        'relative flex min-h-[30px] transition-colors duration-150',
        selected ? 'bg-accent' : 'hover:bg-muted',
      )}
      style={{ height: TRACE_ROW_HEIGHT }}
    >
      <div className="relative shrink-0" style={{ width: showTimeline ? leftWidth : '100%' }}>
        <TreeConnectors row={row} />
        <OperationCell row={row} color={color} collapsed={collapsed} onSelect={onSelect} onToggle={onToggle} />
      </div>
      {showTimeline ? (
        <TimelineCell row={row} color={color} dimmed={dimmed} timelineWidth={timelineWidth} onSelect={onSelect} />
      ) : null}
    </div>
  );
}

function OperationCell({
  row,
  color,
  collapsed,
  onSelect,
  onToggle,
}: {
  row: TraceTreeRow;
  color: string;
  collapsed: boolean;
  onSelect: () => void;
  onToggle: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const hasChildren = row.childCount > 0;
  const indent = treeIndentLeft(row.depth);
  const tokens = row.kind === 'llm' ? spanTokenTotal(row) : 0;
  return (
    <div
      className="flex h-full min-w-0 items-center overflow-hidden pl-1.5"
      style={{ marginLeft: hasChildren ? indent : indent + TRACE_COLLAPSE_WIDTH }}
    >
      {hasChildren ? (
        <button
          type="button"
          className={cn(
            'relative mr-1 flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full border border-border px-1 text-[10px] font-semibold hover:bg-muted',
            PRESS_SCALE_CLASS,
          )}
          style={{ color }}
          aria-expanded={!collapsed}
          aria-label={spanDisplayName(row)}
          onClick={onToggle}
        >
          {row.childCount}
          {collapsed ? <span className="absolute -bottom-1.5 left-2 h-1.5 w-px bg-border" aria-hidden /> : null}
        </button>
      ) : (
        <span
          className="mr-1 size-1.5 shrink-0 self-center rounded-full"
          style={{ backgroundColor: color }}
          aria-hidden
        />
      )}
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden text-left"
        onClick={onSelect}
      >
        {row.status === 'error' ? (
          <AlertCircle
            className="size-3.5 shrink-0 text-destructive"
            aria-label={msg('observability.trace.error', 'Error')}
          />
        ) : null}
        <span className="truncate text-sm font-medium text-foreground">{spanIdentity(row)}</span>
        <span className="min-w-0 truncate text-sm text-muted-foreground">{spanDisplayName(row)}</span>
        {tokens > 0 ? (
          <span className="ml-auto shrink-0 pr-2 text-[11px] tabular-nums text-muted-foreground">
            {formatters.number(tokens, { notation: 'compact', maximumFractionDigits: 1 })}
          </span>
        ) : null}
      </button>
    </div>
  );
}

function TimelineCell({
  row,
  color,
  dimmed,
  timelineWidth,
  onSelect,
}: {
  row: TraceTreeRow;
  color: string;
  dimmed: boolean;
  timelineWidth: number;
  onSelect: () => void;
}) {
  const formatters = useFormatters();
  const label = durationLabelPlacement(row.offsetPct, row.widthPct, timelineWidth);
  return (
    <button
      type="button"
      className={cn('relative min-w-0 flex-1', dimmed ? 'opacity-30' : '')}
      aria-label={spanDisplayName(row)}
      onClick={onSelect}
    >
      <span
        className="absolute flex items-center"
        style={{
          left: `${row.offsetPct}%`,
          width: `${row.widthPct}%`,
          height: TRACE_BAR_HEIGHT,
          bottom: 6,
        }}
      >
        <span className="h-full min-w-0.5 w-[calc(100%-0.375rem)] rounded-sm" style={{ backgroundColor: color }} />
      </span>
      <span
        className="absolute text-[11px] leading-none tabular-nums text-muted-foreground"
        style={{ top: label.top, left: label.left, right: label.right }}
      >
        {formatDurationMs(row.duration_ms, formatters)}
      </span>
    </button>
  );
}

function TreeConnectors({ row }: { row: TraceTreeRow }) {
  if (row.depth <= 0) {
    return null;
  }
  return (
    <span className="pointer-events-none absolute inset-0" aria-hidden>
      {Array.from({ length: row.depth }, (_, index) => {
        const visualDepth = index + 1;
        if (!showTreeConnector(row.ancestorLast, row.depth, visualDepth)) {
          return null;
        }
        return (
          <span
            key={visualDepth}
            className="absolute top-0 border-l border-border"
            style={{
              left: connectorOffset(row.depth, visualDepth),
              height: connectorHeight(row.isLast, visualDepth),
            }}
          />
        );
      })}
      <span
        className="absolute top-1/2 border-t border-border"
        style={{
          left: treeIndentLeft(row.depth),
          width: row.childCount > 0 ? TRACE_TREE_GAP / 2 : TRACE_TREE_GAP + 5,
        }}
      />
    </span>
  );
}

function toggleCollapsed(current: ReadonlySet<string>, spanId: string) {
  const next = new Set(current);
  if (next.has(spanId)) {
    next.delete(spanId);
  } else {
    next.add(spanId);
  }
  return next;
}
