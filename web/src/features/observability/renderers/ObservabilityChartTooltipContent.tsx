import type { ComponentProps } from 'react';
import { ChartTooltipContent } from '../../../shared/ui/chart';
import { useFormatters } from '../../../shared/i18n';
import { formatStatValue } from '../format';

export function ObservabilityChartTooltipContent({
  unit,
  ...props
}: { unit: string } & ComponentProps<typeof ChartTooltipContent>) {
  const formatters = useFormatters();
  return <ChartTooltipContent {...props} formatValue={(value) => formatStatValue(Number(value), unit, formatters)} />;
}
