import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { cn } from '@/shared/lib/utils';

export function resourceListTotalCount(totalCount: number | null | undefined): number | null {
  if (typeof totalCount !== 'number' || !Number.isFinite(totalCount) || totalCount < 0) {
    return null;
  }
  return totalCount;
}

export function resourceListPageCount(totalCount: number | null | undefined, pageSize: number): number | null {
  const count = resourceListTotalCount(totalCount);
  if (count == null || pageSize <= 0) {
    return null;
  }
  if (count === 0) {
    return 1;
  }
  return Math.ceil(count / pageSize);
}

const pagerButtonClassName =
  'border-foreground/30 bg-background text-foreground shadow-none hover:bg-muted disabled:border-foreground/30 disabled:bg-background disabled:text-muted-foreground disabled:opacity-100';

export function ResourceListPagination({
  currentPage,
  totalPages,
  canPrevious,
  canNext,
  onPrevious,
  onNext,
  updatingLabel,
  compact = false,
}: {
  currentPage: number;
  totalPages: number | null;
  canPrevious: boolean;
  canNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
  updatingLabel?: string;
  compact?: boolean;
}) {
  const { msg } = useI18n();
  const statusText = totalPages == null ? String(currentPage) : `${currentPage} / ${totalPages}`;
  const statusLabel =
    totalPages == null
      ? msg('pagination.currentPageLabel', 'Page {current}', { current: currentPage })
      : msg('pagination.pageStatusLabel', 'Page {current} of {total}', { current: currentPage, total: totalPages });

  return (
    <div className={cn('flex items-center gap-2', compact ? 'mt-6' : 'mt-9')}>
      <Button
        type="button"
        variant={compact ? 'secondary' : 'outline'}
        size={compact ? 'icon-sm' : 'icon-lg'}
        className={compact ? 'disabled:opacity-40' : pagerButtonClassName}
        aria-label={msg('pagination.previousPage', 'Previous page')}
        disabled={!canPrevious}
        onClick={onPrevious}
      >
        <ChevronLeft className="size-4" aria-hidden />
      </Button>
      <span
        className={compact ? 'sr-only' : 'min-w-16 text-center text-sm font-medium tabular-nums text-foreground'}
        aria-label={statusLabel}
      >
        {statusText}
      </span>
      <Button
        type="button"
        variant={compact ? 'secondary' : 'outline'}
        size={compact ? 'icon-sm' : 'icon-lg'}
        className={compact ? 'disabled:opacity-40' : pagerButtonClassName}
        aria-label={msg('pagination.nextPage', 'Next page')}
        disabled={!canNext}
        onClick={onNext}
      >
        <ChevronRight className="size-4" aria-hidden />
      </Button>
      {updatingLabel ? <span className="ml-2 text-xs text-muted-foreground/70">{updatingLabel}</span> : null}
    </div>
  );
}
