import { Bar, BarChart, CartesianGrid, XAxis, YAxis, type YAxisTickContentProps } from 'recharts';
import { ChartContainer, ChartTooltip, type ChartConfig } from '../../../shared/ui/chart';
import { useFormatters, useI18n } from '../../../shared/i18n';
import {
  CATEGORY_AXIS_MAX_WIDTH,
  CATEGORY_BAR_MAX_SIZE,
  categoryAxisWidth,
  categoryValueAxisMax,
  formatStatValue,
  panelCategoryLabel,
  truncateCategoryTick,
} from '../format';
import { timeseriesAllowsDecimalTicks } from '../model';
import type { CategoricalPanelData, ObservabilityPanelOptions } from '../types';
import { DonutPanel } from './DonutPanel';
import { HistogramPanel } from './HistogramPanel';
import { ObservabilityChartTooltipContent } from './ObservabilityChartTooltipContent';

export function CategoricalPanel({
  unit,
  data,
  fillHeight = false,
  chart = 'bar',
}: {
  unit: string;
  data: CategoricalPanelData;
  fillHeight?: boolean;
  chart?: ObservabilityPanelOptions['chart'];
}) {
  if (chart === 'pie') {
    return <DonutPanel unit={unit} data={data} fillHeight={fillHeight} />;
  }
  if (chart === 'histogram') {
    return <HistogramPanel unit={unit} data={data} fillHeight={fillHeight} />;
  }
  return <CategoryBarPanel unit={unit} data={data} fillHeight={fillHeight} />;
}

function CategoryBarPanel({
  unit,
  data,
  fillHeight,
}: {
  unit: string;
  data: CategoricalPanelData;
  fillHeight: boolean;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const items = (data.items ?? []).map((item) => ({
    name: panelCategoryLabel(item.name || '—', msg),
    value: item.value,
  }));
  const axisWidth = categoryAxisWidth(items.map((item) => item.name));
  const valueMax = categoryValueAxisMax(
    items.map((item) => item.value),
    !timeseriesAllowsDecimalTicks(unit),
  );
  const config: ChartConfig = { value: { color: 'var(--chart-1)' } };
  return (
    <ChartContainer
      config={config}
      className={
        fillHeight
          ? 'aspect-auto h-full min-h-0 w-full flex-1 overflow-visible'
          : 'aspect-auto h-48 w-full overflow-visible'
      }
    >
      <BarChart data={items} layout="vertical" margin={{ left: 4, right: 12, top: 12, bottom: 0 }} barCategoryGap="24%">
        <CartesianGrid horizontal={false} />
        <YAxis
          dataKey="name"
          type="category"
          tickLine={false}
          axisLine={false}
          width={axisWidth}
          interval={0}
          tickMargin={4}
          tick={CategoryYTick}
        />
        <XAxis
          type="number"
          domain={[0, valueMax]}
          tickLine={false}
          axisLine={false}
          allowDecimals={timeseriesAllowsDecimalTicks(unit)}
          tickFormatter={(value: number) => formatStatValue(value, unit, formatters)}
        />
        <ChartTooltip content={<ObservabilityChartTooltipContent unit={unit} />} />
        <Bar
          dataKey="value"
          fill="var(--chart-1)"
          radius={4}
          maxBarSize={CATEGORY_BAR_MAX_SIZE}
          isAnimationActive={false}
        />
      </BarChart>
    </ChartContainer>
  );
}

function CategoryYTick({ x, y, payload, width }: YAxisTickContentProps) {
  const axisWidth = typeof width === 'number' ? width : CATEGORY_AXIS_MAX_WIDTH;
  return (
    <text x={x} y={y} textAnchor="end" dominantBaseline="central" className="fill-muted-foreground text-xs">
      {truncateCategoryTick(String(payload?.value ?? ''), axisWidth)}
    </text>
  );
}
