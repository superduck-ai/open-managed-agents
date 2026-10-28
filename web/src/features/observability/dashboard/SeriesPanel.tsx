import { useState, type MouseEvent } from 'react';
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, ReferenceArea, XAxis, YAxis } from 'recharts';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { ChartContainer, ChartHoverCard, ChartTooltip, type ChartConfig } from '../../../shared/ui/chart';
import type { PanelUnit, SeriesData } from '../api';
import { timeZoneOption } from '../time/range';
import { useDashboardInteractions } from './context';
import { axisLabel, bucketLabel, formatValue } from './format';
import { usePanelTitle } from './labels';
import { EmptyPanel, PanelFrame } from './PanelFrame';
import { SERIES_TEXT } from './panelText';
import { chartRows, seriesColor, toggleSeries, type Row } from './series';
import { useDragZoom } from './useDragZoom';

type ChartKind = 'bar' | 'line' | 'area';

function seriesMarks(chart: ChartKind, keys: string[], hidden: Set<string>, stacked: boolean) {
  const visible = keys.filter((key) => !hidden.has(key));
  return keys.map((key) => {
    const color = `var(--color-${key})`;
    const hide = hidden.has(key);
    if (chart === 'bar') {
      const top = !stacked || key === visible.at(-1);
      return (
        <Bar
          key={key}
          dataKey={key}
          fill={color}
          hide={hide}
          stackId={stacked ? 'stack' : undefined}
          radius={top ? [4, 4, 0, 0] : [0, 0, 0, 0]}
          maxBarSize={32}
          isAnimationActive={false}
        />
      );
    }
    if (chart === 'area')
      return (
        <Area
          key={key}
          dataKey={key}
          type="monotone"
          stroke={color}
          fill={color}
          fillOpacity={0.15}
          strokeWidth={2}
          hide={hide}
          stackId={stacked ? 'stack' : undefined}
          isAnimationActive={false}
        />
      );
    return (
      <Line
        key={key}
        dataKey={key}
        type="linear"
        stroke={color}
        strokeWidth={2}
        dot={false}
        activeDot={{ r: 3 }}
        hide={hide}
        isAnimationActive={false}
      />
    );
  });
}

function CrosshairCursor(props: {
  points?: { x: number; y: number }[];
  x?: number;
  y?: number;
  width?: number;
  height?: number;
}) {
  const ys = props.points?.map((point) => point.y);
  const x = props.points?.[0]?.x ?? (props.x ?? 0) + (props.width ?? 0) / 2;
  const top = ys ? Math.min(...ys) : (props.y ?? 0);
  const bottom = ys ? Math.max(...ys) : (props.y ?? 0) + (props.height ?? 0);
  return <line x1={x} x2={x} y1={top} y2={bottom} stroke="var(--muted-foreground)" strokeDasharray="3 3" />;
}

function SeriesLegend({
  keys,
  config,
  hidden,
  onToggle,
}: {
  keys: string[];
  config: ChartConfig;
  hidden: Set<string>;
  onToggle: (key: string, additive: boolean) => void;
}) {
  const { msg } = useI18n();
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs"
      role="group"
      aria-label={msg('observability.legend', 'Series')}
    >
      {keys.map((key) => (
        <button
          key={key}
          type="button"
          aria-pressed={!hidden.has(key)}
          title={msg('observability.legendHint', 'Click to show only this series; Ctrl or ⌘ click to toggle it')}
          className={cn(
            'flex min-w-0 items-center gap-1.5 rounded-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50',
            hidden.has(key) && 'opacity-40',
          )}
          onClick={(event: MouseEvent) => onToggle(key, event.ctrlKey || event.metaKey)}
        >
          <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: config[key]?.color }} />
          <span className="truncate">{config[key]?.label}</span>
        </button>
      ))}
    </div>
  );
}

function SeriesTooltip({
  row,
  keys,
  hidden,
  label,
  unit,
  intervalMs,
}: {
  row: Row | undefined;
  keys: string[];
  hidden: Set<string>;
  label: (index: number) => string;
  unit?: PanelUnit;
  intervalMs: number;
}) {
  const { locale } = useI18n();
  const timeZone = timeZoneOption(useDashboardInteractions()?.timeZone ?? 'browser');
  if (!row) return null;
  return (
    <ChartHoverCard
      label={bucketLabel(row.t ?? 0, intervalMs, locale, timeZone)}
      items={keys
        .map((key, index) => ({ key, index }))
        .filter(({ key }) => !hidden.has(key))
        .map(({ key, index }) => ({
          key,
          label: label(index),
          value: formatValue(unit, row[key], locale),
          color: `var(--color-${key})`,
        }))}
    />
  );
}

function SeriesChart({
  id,
  unit,
  chart,
  stacked,
  data,
  intervalMs,
}: {
  id: string;
  unit?: PanelUnit;
  chart: ChartKind;
  stacked: boolean;
  data: SeriesData[];
  intervalMs: number;
}) {
  const { locale, msg } = useI18n();
  const title = usePanelTitle(id);
  const interactions = useDashboardInteractions();
  const [hidden, setHidden] = useState<Set<string>>(() => new Set());
  const [hovered, setHovered] = useState(false);
  const label = (index: number) => {
    const key = data[index].key;
    return SERIES_TEXT[key] ? msg(`observability.series.${key}`, SERIES_TEXT[key]) : key;
  };
  const keys = data.map((_, index) => `s${index}`);
  const config: ChartConfig = Object.fromEntries(
    data.map((series, index) => [keys[index], { label: label(index), color: seriesColor(series.key) }]),
  );
  const rows = chartRows(data);
  const zoom = useDragZoom(rows, intervalMs, interactions?.onZoom);
  const timeZone = timeZoneOption(interactions?.timeZone ?? 'browser');
  const span = rows.length ? (rows.at(-1)!.t ?? 0) - (rows[0].t ?? 0) + intervalMs : 0;
  const tall = interactions?.viewPanel === id;
  const Chart = chart === 'line' ? LineChart : chart === 'area' ? AreaChart : BarChart;
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-2">
      <ChartContainer
        config={config}
        className={cn(
          'aspect-auto w-full cursor-crosshair select-none',
          tall ? 'h-[calc(100vh-16rem)] min-h-80' : 'h-56',
        )}
        role="img"
        aria-label={title}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <Chart
          key={`${rows[0]?.t}-${rows.length}-${intervalMs}`}
          data={rows}
          syncId={interactions?.syncId}
          syncMethod="value"
          margin={{ top: 4, right: 4, bottom: 0, left: 0 }}
          {...zoom.handlers}
        >
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey="t"
            tickLine={false}
            axisLine={false}
            minTickGap={32}
            tickFormatter={(value: number) => axisLabel(value, intervalMs, span, locale, timeZone)}
          />
          <YAxis
            allowDecimals={unit !== 'count' && unit !== 'tokens'}
            tickLine={false}
            axisLine={false}
            width={48}
            tickFormatter={(value: number) => formatValue(unit, value, locale)}
          />
          <ChartTooltip
            cursor={<CrosshairCursor />}
            isAnimationActive={false}
            content={({ active, payload }) =>
              active && hovered && !zoom.area ? (
                <SeriesTooltip
                  row={payload?.[0]?.payload as Row | undefined}
                  keys={keys}
                  hidden={hidden}
                  label={label}
                  unit={unit}
                  intervalMs={intervalMs}
                />
              ) : null
            }
          />
          {seriesMarks(chart, keys, hidden, stacked)}
          {zoom.area ? (
            <ReferenceArea
              {...zoom.area}
              fill="var(--chart-2)"
              fillOpacity={0.18}
              stroke="var(--chart-2)"
              strokeOpacity={0.6}
            />
          ) : null}
        </Chart>
      </ChartContainer>
      {keys.length > 1 ? (
        <SeriesLegend
          keys={keys}
          config={config}
          hidden={hidden}
          onToggle={(key, additive) => setHidden((current) => toggleSeries(current, keys, key, additive))}
        />
      ) : null}
    </div>
  );
}

export function SeriesPanel({
  id,
  unit,
  chart = 'bar',
  stacked = false,
  data,
  intervalMs,
}: {
  id: string;
  unit?: PanelUnit;
  chart?: ChartKind;
  stacked?: boolean;
  data: SeriesData[];
  intervalMs: number;
}) {
  const empty = !data.some((series) => series.points.some((point) => point.v != null));
  return (
    <PanelFrame id={id}>
      {empty ? (
        <EmptyPanel className={cn('flex-none', data.length > 1 ? 'h-62' : 'h-56')} />
      ) : (
        <SeriesChart id={id} unit={unit} chart={chart} stacked={stacked} data={data} intervalMs={intervalMs} />
      )}
    </PanelFrame>
  );
}
