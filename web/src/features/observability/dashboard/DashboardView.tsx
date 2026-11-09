import { useI18n } from '../../../shared/i18n';
import { Skeleton } from '../../../shared/ui/skeleton';
import { DashboardGrid } from './DashboardGrid';
import type { useDashboardQuery } from './useDashboardQuery';

const SKELETON_SPANS = [3, 3, 3, 3, 6, 6, 6, 6];

function DashboardSkeleton() {
  const { msg } = useI18n();
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-12" aria-busy>
      <span className="sr-only">{msg('observability.loading', 'Loading dashboard…')}</span>
      {SKELETON_SPANS.map((span, index) => (
        <Skeleton
          key={index}
          className={span === 3 ? 'h-24 md:col-span-6 xl:col-span-3' : 'h-64 md:col-span-12 xl:col-span-6'}
        />
      ))}
    </div>
  );
}

export function DashboardView({
  query,
  viewPanel,
}: {
  query: ReturnType<typeof useDashboardQuery>;
  viewPanel: string;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {query.isError && (
        <div role="alert" className="text-sm text-destructive">
          {msg('observability.loadError', 'Could not load the dashboard. Please try again later.')}
        </div>
      )}
      {query.isPending && !query.isError && <DashboardSkeleton />}
      {query.data && <DashboardGrid dashboard={query.data} viewPanel={viewPanel} />}
    </div>
  );
}
