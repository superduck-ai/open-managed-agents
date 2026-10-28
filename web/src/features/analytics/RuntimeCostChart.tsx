import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '../../shared/ui/chart';
import type { RuntimeCostDay } from './runtimeCost';

export function RuntimeCostChart({ days, label }: { days: RuntimeCostDay[]; label: string }) {
  const config: ChartConfig = { cost: { label, color: 'var(--chart-1)' } };
  const rows = days.map((day) => ({ date: day.date.slice(5), cost: Number(day.amount) }));
  return (
    <ChartContainer config={config} className="h-[360px] w-full px-2 py-4">
      <BarChart data={rows}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="date" tickLine={false} axisLine={false} />
        <YAxis tickLine={false} axisLine={false} width={56} />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Bar dataKey="cost" fill="var(--color-cost)" radius={3} />
      </BarChart>
    </ChartContainer>
  );
}
