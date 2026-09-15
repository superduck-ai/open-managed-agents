import { RefreshCw } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import type { ObservabilityFilters } from './model';
import { TimeRangePicker } from './TimeRangePicker';

export function ObservabilityToolbar({
  filters,
  onChange,
  onRefresh,
  refreshing,
  showTimeRange = true,
  children,
}: {
  filters: ObservabilityFilters;
  onChange: (next: ObservabilityFilters) => void;
  onRefresh: () => void;
  refreshing?: boolean;
  showTimeRange?: boolean;
  children?: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2">{children}</div>
      <div className="flex items-center gap-2">
        {showTimeRange ? <TimeRangePicker filters={filters} onChange={onChange} /> : null}
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label={msg('observability.refresh', 'Refresh')}
          disabled={refreshing}
          onClick={onRefresh}
        >
          <RefreshCw className={cn('size-3.5', refreshing && 'animate-spin')} aria-hidden />
        </Button>
      </div>
    </div>
  );
}
