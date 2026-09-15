import { Cell, Label, Pie, PieChart } from 'recharts';
import { ChartContainer, type ChartConfig } from '../../../shared/ui/chart';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { chartColorAt } from '../chartColors';
import { formatStatValue, panelCategoryLabel } from '../format';
import type { CategoricalPanelData } from '../types';

export function DonutPanel({
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
  const items = (data.items ?? []).map((item, index) => ({
    name: item.name || '—',
    label: panelCategoryLabel(item.name, msg),
    value: item.value,
    fill: chartColorAt(index),
  }));
  const total = items.reduce((sum, item) => sum + item.value, 0);
  const sliceCount = items.filter((item) => item.value > 0).length;
  const config = Object.fromEntries(
    items.map((item) => [item.name, { label: item.label, color: item.fill }]),
  ) as ChartConfig;

  return (
    <div className={fillHeight ? 'flex min-h-0 flex-1 items-center gap-4' : 'flex items-center gap-4'}>
      <ChartContainer
        config={config}
        className={fillHeight ? 'aspect-square h-full min-h-32 w-32 flex-none' : 'aspect-square h-32 w-32 flex-none'}
      >
        <PieChart>
          <Pie
            data={items}
            dataKey="value"
            nameKey="label"
            innerRadius={42}
            outerRadius={58}
            stroke="var(--background)"
            strokeWidth={sliceCount > 1 ? 2 : 0}
          >
            {items.map((item, index) => (
              <Cell key={`${item.name}-${index}`} fill={item.fill} />
            ))}
            <Label
              content={({ viewBox }) => {
                if (!viewBox || !('cx' in viewBox) || !('cy' in viewBox)) {
                  return null;
                }
                return (
                  <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                    <tspan className="fill-foreground text-xl font-semibold">
                      {formatStatValue(total, unit, formatters)}
                    </tspan>
                  </text>
                );
              }}
            />
          </Pie>
        </PieChart>
      </ChartContainer>
      <ul
        className={cn(
          'flex min-w-0 flex-1 flex-col gap-2',
          fillHeight ? 'min-h-0 overflow-y-auto' : 'max-h-36 overflow-y-auto',
        )}
      >
        {items.map((item, index) => {
          const percent = total > 0 ? (item.value * 100) / total : 0;
          return (
            <li key={`${item.name}-${index}`} className="flex items-baseline justify-between gap-3 text-sm">
              <span className="flex min-w-0 items-center gap-2 text-foreground">
                <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: item.fill }} />
                <span className="truncate" title={item.label}>
                  {item.label}
                </span>
              </span>
              <span className="shrink-0 tabular-nums">
                <span className="font-medium text-foreground">{formatStatValue(item.value, unit, formatters)}</span>
                <span className="ml-2 text-muted-foreground">
                  {formatters.number(percent, { maximumFractionDigits: 0 })}%
                </span>
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
