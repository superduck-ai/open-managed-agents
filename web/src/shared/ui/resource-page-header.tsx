import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/utils';

export function ResourcePageHeader({
  title,
  description,
  actions,
  titleAdornment,
  contentGap = 'filters',
  compact = false,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  titleAdornment?: ReactNode;
  contentGap?: 'filters' | 'content';
  compact?: boolean;
}) {
  return (
    <header
      className={cn(
        'flex flex-col items-start gap-4 sm:flex-row sm:items-start sm:justify-between sm:gap-6',
        compact ? 'mb-4' : contentGap === 'content' ? 'mb-7' : 'mb-5',
      )}
    >
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <h1 className={cn('font-semibold leading-tight text-foreground', compact ? 'text-xl' : 'text-[28px]')}>
            {title}
          </h1>
          {titleAdornment}
        </div>
        {description ? (
          <p
            className={cn(
              'max-w-[760px] leading-5 text-muted-foreground',
              compact ? 'mt-2 text-sm' : 'mt-2 text-[15px]',
            )}
          >
            {description}
          </p>
        ) : null}
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}
