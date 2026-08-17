import * as React from 'react';
import * as RechartsPrimitive from 'recharts';

import { cn } from '@/shared/lib/utils';

const THEMES = { light: '', dark: '.dark' } as const;

export type ChartConfig = Record<
  string,
  {
    label?: React.ReactNode;
    icon?: React.ComponentType;
    color?: string;
    theme?: Record<keyof typeof THEMES, string>;
  }
>;

type ChartContextProps = {
  config: ChartConfig;
};

const ChartContext = React.createContext<ChartContextProps | null>(null);

function useChart() {
  const context = React.useContext(ChartContext);
  if (!context) {
    throw new Error('useChart must be used within a ChartContainer');
  }
  return context;
}

function ChartContainer({
  id,
  className,
  children,
  config,
  ...props
}: React.ComponentProps<'div'> & {
  config: ChartConfig;
  children: React.ComponentProps<typeof RechartsPrimitive.ResponsiveContainer>['children'];
}) {
  const uniqueId = React.useId();
  const chartId = `chart-${id || uniqueId.replace(/:/g, '')}`;
  return (
    <ChartContext.Provider value={{ config }}>
      <div
        data-slot="chart"
        data-chart={chartId}
        className={cn(
          "flex aspect-video justify-center text-xs [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground [&_.recharts-cartesian-grid_line[stroke='#ccc']]:stroke-border/50 [&_.recharts-curve.recharts-tooltip-cursor]:stroke-border [&_.recharts-dot[stroke='#fff']]:stroke-transparent [&_.recharts-layer]:outline-none [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-muted [&_.recharts-reference-line_[stroke='#ccc']]:stroke-border [&_.recharts-sector]:outline-none [&_.recharts-surface]:outline-none",
          className,
        )}
        {...props}
      >
        <ChartStyle id={chartId} config={config} />
        <RechartsPrimitive.ResponsiveContainer>{children}</RechartsPrimitive.ResponsiveContainer>
      </div>
    </ChartContext.Provider>
  );
}

function ChartStyle({ id, config }: { id: string; config: ChartConfig }) {
  const colorConfig = Object.entries(config).filter(([, item]) => item.theme || item.color);
  if (!colorConfig.length) {
    return null;
  }
  const css = Object.entries(THEMES)
    .map(([theme, prefix]) => {
      const declarations = colorConfig
        .map(([key, itemConfig]) => {
          const color = itemConfig.theme?.[theme as keyof typeof itemConfig.theme] || itemConfig.color;
          return color ? `  --color-${key}: ${color};` : null;
        })
        .filter(Boolean)
        .join('\n');
      return `${prefix} [data-chart=${id}] {\n${declarations}\n}`;
    })
    .join('\n');
  return <style dangerouslySetInnerHTML={{ __html: css }} />;
}

const ChartTooltip = RechartsPrimitive.Tooltip;

export type ChartHoverItem = {
  key: string;
  label: React.ReactNode;
  value: React.ReactNode;
  color?: string;
};

function ChartHoverCard({
  label,
  items,
  className,
  style,
}: {
  label?: React.ReactNode;
  items: ChartHoverItem[];
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <div
      className={cn(
        'flex w-max max-w-[min(22rem,100%)] min-w-0 flex-col gap-1.5 overflow-hidden rounded-lg border border-border/50 bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md',
        className,
      )}
      style={style}
    >
      {label ? <div className="font-medium tabular-nums">{label}</div> : null}
      {items.length ? (
        <div className="subtle-scrollbar flex max-h-44 min-w-0 flex-col gap-1.5 overflow-y-auto">
          {items.map((item) => (
            <div key={item.key} className="flex min-w-0 items-center gap-3">
              <span className="flex min-w-0 flex-1 items-center gap-1.5 text-muted-foreground">
                {item.color ? (
                  <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
                ) : null}
                {item.label ? <ChartHoverLabel label={item.label} /> : null}
              </span>
              <span className="shrink-0 font-mono font-medium tabular-nums whitespace-nowrap text-foreground">
                {item.value}
              </span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ChartHoverLabel({ label }: { label: React.ReactNode }) {
  if (typeof label === 'string') {
    return (
      <span className="min-w-0 truncate" title={label}>
        {label}
      </span>
    );
  }
  return <span className="min-w-0">{label}</span>;
}

type ChartTooltipPayloadItem = {
  name?: string;
  value?: number | string;
  color?: string;
  fill?: string;
  dataKey?: string;
  payload?: Record<string, unknown>;
};

function ChartTooltipContent({
  active,
  payload,
  className,
  label,
  formatValue,
}: {
  active?: boolean;
  payload?: ReadonlyArray<ChartTooltipPayloadItem>;
  className?: string;
  label?: React.ReactNode;
  formatValue?: (value: number | string) => React.ReactNode;
}) {
  const { config } = useChart();
  if (!active || !payload?.length) {
    return null;
  }
  return (
    <ChartHoverCard
      className={className}
      label={label}
      items={payload.map((item, index) => {
        const key = String(item.dataKey || item.name || 'value');
        const itemLabel = chartTooltipItemLabel(item, config);
        return {
          key: `${key}-${index}`,
          label: itemLabel === label ? '' : itemLabel,
          value: formatTooltipValue(item.value, formatValue),
          color: item.color ?? item.fill,
        };
      })}
    />
  );
}

function chartTooltipItemLabel(item: ChartTooltipPayloadItem, config: ChartConfig): React.ReactNode {
  const key = String(item.dataKey || item.name || 'value');
  const configured = config[key]?.label;
  if (configured && configured !== key && configured !== 'value') {
    return configured;
  }
  const row = item.payload;
  if (typeof row?.label === 'string' && row.label) {
    return row.label;
  }
  if (typeof row?.name === 'string' && row.name && row.name !== key) {
    return row.name;
  }
  if (item.name && item.name !== key && item.name !== 'value') {
    return item.name;
  }
  return '';
}

function formatTooltipValue(
  value: number | string | undefined,
  formatValue?: (value: number | string) => React.ReactNode,
) {
  if (value == null) {
    return '';
  }
  return formatValue ? formatValue(value) : value;
}

const ChartLegend = RechartsPrimitive.Legend;

function ChartLegendContent({
  payload,
  className,
}: {
  payload?: Array<{ value?: string; color?: string; dataKey?: string }>;
  className?: string;
}) {
  const { config } = useChart();
  if (!payload?.length) {
    return null;
  }
  return (
    <div className={cn('flex flex-wrap items-center justify-center gap-4 pt-3', className)}>
      {payload.map((item) => {
        const key = String(item.dataKey || item.value || 'value');
        const itemConfig = config[key];
        return (
          <div key={key} className="flex min-w-0 items-center gap-1.5">
            <div className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
            <span className="min-w-0 truncate text-muted-foreground">{itemConfig?.label || item.value || key}</span>
          </div>
        );
      })}
    </div>
  );
}

export {
  ChartContainer,
  ChartHoverCard,
  ChartLegend,
  ChartLegendContent,
  ChartStyle,
  ChartTooltip,
  ChartTooltipContent,
};
