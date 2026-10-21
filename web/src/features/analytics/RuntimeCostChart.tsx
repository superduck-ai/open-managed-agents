import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '../../shared/ui/chart';
import type { RuntimeCostDay } from './runtimeCost';

const palette = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];

export function RuntimeCostChart({ days }: { days: RuntimeCostDay[] }) {
  const keys = chartKeys(days);
  if (keys.length === 0) {
    return null;
  }
  const config: ChartConfig = {};
  const rows = days.map((day) => {
    const row: Record<string, string | number> = { date: day.date.slice(5) };
    keys.forEach((key, index) => {
      const group = day.groups.find((item) => item.id === key.id);
      row[`group${index}`] = group ? Number(group.amount) : 0;
      config[`group${index}`] = { label: key.label, color: palette[index % palette.length] };
    });
    return row;
  });
  return (
    <ChartContainer config={config} className="h-[360px] w-full px-2 py-4">
      <BarChart data={rows}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="date" tickLine={false} axisLine={false} />
        <YAxis tickLine={false} axisLine={false} width={56} />
        <ChartTooltip content={<ChartTooltipContent />} />
        {keys.map((key, index) => (
          <Bar
            key={key.id || `group${index}`}
            dataKey={`group${index}`}
            stackId="runtime"
            fill={`var(--color-group${index})`}
          />
        ))}
      </BarChart>
    </ChartContainer>
  );
}

function chartKeys(days: RuntimeCostDay[]) {
  const keys = new Map<string, string>();
  days.forEach((day) => {
    day.groups.forEach((group) => {
      if (!keys.has(group.id)) {
        keys.set(group.id, group.label || group.id || 'unknown');
      }
    });
  });
  return [...keys.entries()].map(([id, label]) => ({ id, label }));
}
