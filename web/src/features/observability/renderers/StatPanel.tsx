import { useFormatters, useI18n } from '../../../shared/i18n';
import { formatChangePercent, formatStatValue } from '../format';
import type { StatPanelData } from '../types';

export function StatPanel({ unit, data }: { unit: string; data: StatPanelData }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  return (
    <div className="flex flex-col gap-1">
      <p className="text-2xl font-semibold tracking-tight text-foreground">
        {formatStatValue(data.current, unit, formatters)}
      </p>
      <p className="text-xs text-muted-foreground">
        {msg('observability.stat.previousPeriod', 'vs. previous period')}{' '}
        {formatChangePercent(data.change_percent, formatters)}
      </p>
    </div>
  );
}
