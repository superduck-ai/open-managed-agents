import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { Area, AreaChart, CartesianGrid, Line, LineChart, ReferenceArea, XAxis, YAxis } from 'recharts';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { ChartContainer, ChartHoverCard, type ChartConfig } from '../../../shared/ui/chart';
import { chartColorAt } from '../chartColors';
import { PRESS_SCALE_CLASS } from '../chrome';
import { formatChartHoverTimestamp, formatSeriesParts, formatStatValue, formatTimeseriesYTick } from '../format';
import {
  fillTimeseriesRows,
  interpolateSeriesValue,
  mergeTimeseriesRows,
  nextHiddenSeries,
  niceValueDomain,
  plotOverlayFrame,
  plotYAtValue,
  timeAtPlotX,
  timeAxisTickOptions,
  timeseriesAllowsDecimalTicks,
  timeseriesAxisDomain,
  timeseriesDotVisible,
  timeseriesHoverCardAnchor,
} from '../model';
import type { ObservabilityPanelOptions, TimeseriesPanelData, TimeseriesSeries } from '../types';
import { useTimeBrush } from '../useTimeBrush';

const CROSSHAIR_STROKE = 'var(--muted-foreground)';
const SPARSE_SERIES_DOTS = 12;

type PlotPointerEvent = {
  clientX: number;
  clientY: number;
  currentTarget: Element;
};

type CrosshairFrame = {
  x: number;
  y: number;
  width: number;
  height: number;
  left: number;
  top: number;
  hostWidth: number;
};

type HoverState = {
  frame: CrosshairFrame;
  timeMs: number;
};

type TooltipItem = {
  key: string;
  label: ReactNode;
  value: string;
  color: string;
};

type HoverDot = {
  x: number;
  y: number;
  color: string;
};

export function TimeseriesPanel({
  unit,
  options,
  data,
  fillHeight = false,
  start,
  end,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  unit: string;
  options?: ObservabilityPanelOptions;
  data: TimeseriesPanelData;
  fillHeight?: boolean;
  start?: string;
  end?: string;
  onTimeRangeChange?: (start: string, end: string) => void;
  onTimeRangeZoomOut?: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  // 用 useMemo 固定 `?? 兜底值` 的引用，否则每次渲染都会产出新数组/对象，击穿下游 memo。
  const series = useMemo(() => data.series ?? [], [data.series]);
  const stacked = Boolean(options?.stacked);
  const seriesUnits = useMemo(() => options?.series_units ?? {}, [options?.series_units]);
  const merged = useMemo(() => mergeTimeseriesRows(series), [series]);
  const axisDomain = useMemo(() => timeseriesAxisDomain(start, end, merged), [start, end, merged]);
  const chartData = useMemo(
    () =>
      axisDomain && series.length
        ? fillTimeseriesRows(
            merged,
            series.map((item) => item.name),
            axisDomain[0],
            axisDomain[1],
            stacked ? 'zero' : 'gap',
          )
        : merged,
    [axisDomain, merged, series, stacked],
  );
  const axisSpanMs = axisDomain ? axisDomain[1] - axisDomain[0] : 0;
  const tickOptions = useMemo(() => timeAxisTickOptions(axisSpanMs), [axisSpanMs]);
  const config = useMemo(() => chartConfig(series, msg), [msg, series]);
  const percentKeys = useMemo(
    () =>
      new Set(
        Object.entries(seriesUnits)
          .filter(([, seriesUnit]) => seriesUnit === 'percent')
          .map(([name]) => name),
      ),
    [seriesUnits],
  );
  const brushDomain = useMemo(
    () => (axisDomain ? { startMs: axisDomain[0], endMs: axisDomain[1] } : null),
    [axisDomain],
  );
  const brush = useTimeBrush(brushDomain, onTimeRangeChange);
  const hover = usePlotHover(Boolean(brush.selection), axisDomain);
  const [hidden, setHidden] = useState<Set<string>>(() => new Set());
  // 悬停点和 Y 轴共用同一 domain（按可见系列计算），保证悬停点落在曲线上。
  const leftDomain = useMemo(
    () =>
      niceValueDomain(
        chartData,
        series.filter((item) => !hidden.has(item.name) && !percentKeys.has(item.name)).map((item) => item.name),
        stacked,
      ),
    [chartData, hidden, percentKeys, series, stacked],
  );
  const rightDomain = useMemo(
    () =>
      niceValueDomain(
        chartData,
        series.filter((item) => !hidden.has(item.name) && percentKeys.has(item.name)).map((item) => item.name),
        false,
      ),
    [chartData, hidden, percentKeys, series],
  );
  const hoverTimeMs = hover.state?.timeMs;
  const tooltipItems = hoverTooltipItems(series, hidden, chartData, hoverTimeMs, unit, seriesUnits, formatters, msg);
  const hoverDots = hover.state
    ? seriesHoverDots(series, hidden, chartData, hover.state.frame, hover.state.timeMs, stacked, percentKeys, {
        left: leftDomain,
        right: rightDomain,
      })
    : [];
  // 悬停/十字线只更新覆盖层；memo 图表元素本身，避免 mousemove 触发整个 SVG 重渲染。
  const chart = useMemo(() => {
    const Chart = stacked ? AreaChart : LineChart;
    return (
      <ChartContainer config={config} className="aspect-auto h-full min-h-0 w-full">
        <Chart data={chartData} margin={{ left: 8, right: 8, top: 8, bottom: 4 }}>
          <CartesianGrid vertical={false} yAxisId="left" syncWithTicks horizontal={omitPlotTopGridLine} />
          <XAxis
            dataKey="t"
            type="number"
            scale="linear"
            domain={axisDomain ?? ['auto', 'auto']}
            allowDataOverflow
            tickLine={false}
            axisLine={false}
            minTickGap={24}
            tickCount={7}
            tickFormatter={(value: number) => formatters.date(value, tickOptions)}
          />
          <YAxis
            yAxisId="left"
            domain={leftDomain}
            allowDataOverflow
            tickLine={false}
            axisLine={false}
            width={48}
            allowDecimals={timeseriesAllowsDecimalTicks(unit)}
            tickFormatter={(value: number) => formatTimeseriesYTick(value, formatStatValue(value, unit, formatters))}
          />
          {percentKeys.size ? (
            <YAxis
              yAxisId="right"
              orientation="right"
              domain={rightDomain}
              allowDataOverflow
              tickLine={false}
              axisLine={false}
              width={40}
              tickFormatter={(value: number) =>
                formatTimeseriesYTick(value, `${formatters.number(value, { maximumFractionDigits: 0 })}%`)
              }
            />
          ) : null}
          {series.map((item, index) => (
            <TimeseriesSeriesMark
              key={item.name}
              name={item.name}
              color={chartColorAt(index)}
              stacked={stacked}
              yAxisId={percentKeys.has(item.name) ? 'right' : 'left'}
              hidden={hidden.has(item.name)}
              showAllDots={item.points.length <= SPARSE_SERIES_DOTS}
              realTimestamps={new Set(item.points.map((point) => Date.parse(point.timestamp)).filter(Number.isFinite))}
            />
          ))}
          {brush.selection ? (
            <ReferenceArea
              yAxisId="left"
              x1={Math.min(brush.selection.left, brush.selection.right)}
              x2={Math.max(brush.selection.left, brush.selection.right)}
              fill="var(--foreground)"
              fillOpacity={0.08}
            />
          ) : null}
        </Chart>
      </ChartContainer>
    );
  }, [
    axisDomain,
    brush.selection,
    chartData,
    config,
    formatters,
    hidden,
    leftDomain,
    percentKeys,
    rightDomain,
    series,
    stacked,
    tickOptions,
    unit,
  ]);

  return (
    <div className={fillHeight ? 'flex h-full min-h-0 w-full flex-1 flex-col' : 'flex h-48 w-full flex-col'}>
      <div
        className="relative min-h-0 w-full flex-1 select-none"
        onPointerDown={brush.onPointerDown}
        onPointerMove={(event) => {
          brush.onPointerMove(event);
          hover.onPointerMove(event);
        }}
        onPointerUp={brush.onPointerUp}
        onPointerLeave={hover.onPointerLeave}
        onDoubleClick={() => onTimeRangeZoomOut?.()}
        style={{ cursor: 'crosshair', touchAction: 'none' }}
      >
        {chart}
        {hover.state ? (
          <TimeseriesHoverOverlay
            frame={hover.state.frame}
            timestamp={formatChartHoverTimestamp(hover.state.timeMs)}
            items={tooltipItems}
            dots={hoverDots}
          />
        ) : null}
      </div>
      <TimeseriesLegend
        series={series}
        hidden={hidden}
        compact={!fillHeight}
        onToggle={(name, isolate) => {
          setHidden((current) =>
            nextHiddenSeries(
              current,
              name,
              series.map((item) => item.name),
              isolate,
            ),
          );
        }}
      />
    </div>
  );
}

function usePlotHover(disabled: boolean, domain: [number, number] | undefined) {
  const [state, setState] = useState<HoverState | null>(null);
  const onPointerMove = useCallback(
    (event: PlotPointerEvent) => {
      if (disabled) {
        setState(null);
        return;
      }
      const host = event.currentTarget.getBoundingClientRect();
      const grid = event.currentTarget.querySelector('.recharts-cartesian-grid') ?? event.currentTarget;
      const frame = plotOverlayFrame(event.clientX, event.clientY, host, grid.getBoundingClientRect());
      if (!frame || !domain) {
        setState(null);
        return;
      }
      const x = Math.min(frame.width - 1, Math.max(1, frame.x));
      setState({
        frame: { ...frame, x, hostWidth: host.width },
        timeMs: timeAtPlotX(x, frame.width, domain[0], domain[1]),
      });
    },
    [disabled, domain],
  );
  const onPointerLeave = useCallback(() => setState(null), []);
  return { state: disabled ? null : state, onPointerMove, onPointerLeave };
}

function TimeseriesHoverOverlay({
  frame,
  timestamp,
  items,
  dots,
}: {
  frame: CrosshairFrame;
  timestamp?: string;
  items: TooltipItem[];
  dots: HoverDot[];
}) {
  const anchor = timeseriesHoverCardAnchor(frame);
  return (
    <>
      <svg
        className="pointer-events-none absolute"
        style={{ left: frame.left, top: frame.top, width: frame.width, height: frame.height }}
        width={frame.width}
        height={frame.height}
      >
        <line
          x1={frame.x}
          y1={0}
          x2={frame.x}
          y2={frame.height}
          stroke={CROSSHAIR_STROKE}
          strokeDasharray="4 4"
          strokeOpacity={0.55}
          strokeWidth={1}
        />
        {dots.map((dot, index) => (
          <circle
            key={`${dot.color}-${index}`}
            cx={dot.x}
            cy={dot.y}
            r={4}
            fill="var(--background)"
            stroke={dot.color}
            strokeWidth={1.5}
          />
        ))}
      </svg>
      {timestamp || items.length ? (
        <div
          className={cn(
            'pointer-events-none absolute z-10 flex',
            anchor.side === 'left' ? 'justify-end' : 'justify-start',
          )}
          style={{ top: anchor.top, left: anchor.left, right: anchor.right }}
        >
          <ChartHoverCard
            className="min-w-0 max-w-full"
            label={timestamp}
            items={items.map((item) => ({
              key: item.key,
              label: item.label,
              value: item.value,
              color: item.color,
            }))}
          />
        </div>
      ) : null}
    </>
  );
}

function TimeseriesLegend({
  series,
  hidden,
  compact,
  onToggle,
}: {
  series: TimeseriesSeries[];
  hidden: Set<string>;
  compact: boolean;
  onToggle: (name: string, isolate: boolean) => void;
}) {
  const { msg } = useI18n();
  if (!series.length) {
    return null;
  }
  const toggleHint = msg('observability.chart.legend.toggle', 'Click to isolate. Ctrl/Cmd+click to toggle.');
  return (
    <div
      className={cn(
        'subtle-scrollbar grid shrink-0 grid-cols-[repeat(auto-fill,minmax(8.5rem,1fr))] gap-x-3 gap-y-1 overflow-y-auto px-1 pt-2',
        compact ? 'max-h-[2.75rem]' : 'max-h-32',
      )}
    >
      {series.map((item, index) => {
        const isHidden = hidden.has(item.name);
        const label = formatSeriesParts(item.name, msg);
        return (
          <button
            key={item.name}
            type="button"
            className={cn(
              PRESS_SCALE_CLASS,
              'flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm text-left text-xs text-muted-foreground',
              isHidden && 'opacity-40 line-through',
            )}
            aria-pressed={!isHidden}
            aria-label={`${label.full}. ${toggleHint}`}
            title={label.full}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => onToggle(item.name, !event.metaKey && !event.ctrlKey)}
          >
            <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: chartColorAt(index) }} />
            <TimeseriesSeriesName parts={label} />
          </button>
        );
      })}
    </div>
  );
}

function TimeseriesSeriesName({ parts }: { parts: ReturnType<typeof formatSeriesParts> }) {
  if (!parts.suffix) {
    return (
      <span className="min-w-0 truncate" title={parts.full}>
        {parts.base}
      </span>
    );
  }
  return (
    <span className="flex min-w-0 items-center gap-1" title={parts.full}>
      <span className="min-w-0 truncate">{parts.base}</span>
      <span className="shrink-0">· {parts.suffix}</span>
    </span>
  );
}

function TimeseriesSeriesMark({
  name,
  color,
  stacked,
  yAxisId,
  hidden,
  showAllDots,
  realTimestamps,
}: {
  name: string;
  color: string;
  stacked: boolean;
  yAxisId: 'left' | 'right';
  hidden: boolean;
  showAllDots: boolean;
  realTimestamps: Set<number>;
}) {
  const renderDot = showAllDots
    ? (props: { cx?: number; cy?: number; payload?: { t?: number } & Record<string, unknown> }) => {
        if (!timeseriesDotVisible(props.payload?.t, props.payload?.[name], realTimestamps)) {
          return null;
        }
        return <TimeseriesPoint cx={props.cx} cy={props.cy} color={color} />;
      }
    : false;
  if (stacked) {
    return (
      <Area
        yAxisId={yAxisId}
        type="linear"
        dataKey={name}
        stackId="stack"
        stroke={color}
        fill={color}
        fillOpacity={0.18}
        strokeWidth={1.5}
        isAnimationActive={false}
        hide={hidden}
        dot={renderDot}
        activeDot={false}
        connectNulls={false}
      />
    );
  }
  return (
    <Line
      yAxisId={yAxisId}
      type="linear"
      dataKey={name}
      stroke={color}
      strokeWidth={1.5}
      isAnimationActive={false}
      hide={hidden}
      dot={renderDot}
      activeDot={false}
      connectNulls={false}
    />
  );
}

function omitPlotTopGridLine({
  x1,
  y1,
  x2,
  y2,
  key,
  offset,
  stroke,
  strokeWidth,
  strokeOpacity,
  strokeDasharray,
}: {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  key?: string;
  offset: { top: number };
  stroke?: string;
  strokeWidth?: number | string;
  strokeOpacity?: number | string;
  strokeDasharray?: string | number | readonly number[];
}) {
  if (Math.abs(y1 - offset.top) < 1) {
    return <g key={key} />;
  }
  const dasharray = Array.isArray(strokeDasharray) ? strokeDasharray.join(',') : strokeDasharray;
  return (
    <line
      key={key}
      x1={x1}
      y1={y1}
      x2={x2}
      y2={y2}
      fill="none"
      stroke={stroke}
      strokeWidth={strokeWidth}
      strokeOpacity={strokeOpacity}
      strokeDasharray={typeof dasharray === 'string' || typeof dasharray === 'number' ? dasharray : undefined}
    />
  );
}

function TimeseriesPoint({ cx, cy, color }: { cx?: number; cy?: number; color: string }) {
  if (typeof cx !== 'number' || typeof cy !== 'number') {
    return null;
  }
  return <circle cx={cx} cy={cy} r={2.5} fill="var(--background)" stroke={color} strokeWidth={1.5} />;
}

function hoverTooltipItems(
  series: TimeseriesSeries[],
  hidden: Set<string>,
  rows: Array<Record<string, number | null | undefined>>,
  timeMs: number | undefined,
  unit: string,
  seriesUnits: Record<string, string>,
  formatters: ReturnType<typeof useFormatters>,
  msg: (id: string, fallback: string) => string,
): TooltipItem[] {
  if (timeMs == null) {
    return [];
  }
  return series.flatMap((item, index) => {
    if (hidden.has(item.name)) {
      return [];
    }
    const value = interpolateSeriesValue(rows, item.name, timeMs);
    if (value == null) {
      return [];
    }
    return [
      {
        key: item.name,
        label: <TimeseriesSeriesName parts={formatSeriesParts(item.name, msg)} />,
        value: formatStatValue(value, seriesUnits[item.name] ?? unit, formatters),
        color: chartColorAt(index),
      },
    ];
  });
}

function seriesHoverDots(
  series: TimeseriesSeries[],
  hidden: Set<string>,
  rows: Array<Record<string, number | null | undefined>>,
  frame: CrosshairFrame,
  timeMs: number,
  stacked: boolean,
  percentKeys: Set<string>,
  domains: { left: [number, number]; right: [number, number] },
): HoverDot[] {
  const dots: HoverDot[] = [];
  let stackedTotal = 0;
  series.forEach((item, index) => {
    if (hidden.has(item.name)) {
      return;
    }
    const value = interpolateSeriesValue(rows, item.name, timeMs);
    if (value == null) {
      return;
    }
    const percent = percentKeys.has(item.name);
    const plotted = stacked && !percent ? (stackedTotal += value) : value;
    const domain = percent ? domains.right : domains.left;
    dots.push({
      x: frame.x,
      y: Math.min(frame.height - 1, Math.max(1, plotYAtValue(plotted, domain[0], domain[1], frame.height))),
      color: chartColorAt(index),
    });
  });
  return dots;
}

function chartConfig(series: TimeseriesSeries[], msg: (id: string, fallback: string) => string): ChartConfig {
  return Object.fromEntries(
    series.map((item, index) => [
      item.name,
      { label: formatSeriesParts(item.name, msg).full, color: chartColorAt(index) },
    ]),
  );
}
