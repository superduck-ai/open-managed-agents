import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { ChartContainer, ChartTooltip, type ChartConfig } from '../../../shared/ui/chart';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { formatStatValue, panelCategoryLabel } from '../format';
import { timeseriesAllowsDecimalTicks } from '../model';
import type { CategoricalPanelData } from '../types';
import { ObservabilityChartTooltipContent } from './ObservabilityChartTooltipContent';

export function HistogramPanel({
  unit,
  data,
  fillHeight = false,
}: {
  unit: string;
  data: CategoricalPanelData;
  fillHeight?: boolean;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const items = (data.items ?? []).map((item) => ({
    name: item.name || '—',
    label: panelCategoryLabel(item.name, msg),
    value: item.value,
  }));
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
      <BarChart data={items} margin={{ left: 8, right: 8, top: 8, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} />
        <YAxis
          tickLine={false}
          axisLine={false}
          allowDecimals={timeseriesAllowsDecimalTicks(unit)}
          tickFormatter={(value: number) => formatStatValue(value, unit, formatters)}
        />
        <ChartTooltip content={<ObservabilityChartTooltipContent unit={unit} />} />
        <Bar dataKey="value" fill="var(--chart-1)" radius={4} isAnimationActive={false} />
      </BarChart>
    </ChartContainer>
  );
}
