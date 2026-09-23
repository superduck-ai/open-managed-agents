import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/utils';

export function ResourcePageHeader({
  title,
  description,
  actions,
  titleAdornment,
  contentGap = 'filters',
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  titleAdornment?: ReactNode;
  contentGap?: 'filters' | 'content';
}) {
  return (
    <header className={cn('flex items-start justify-between gap-6', contentGap === 'content' ? 'mb-7' : 'mb-5')}>
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <h1 className="text-[28px] font-semibold leading-tight text-foreground">{title}</h1>
          {titleAdornment}
        </div>
        {description ? (
          <p className="mt-2 max-w-[760px] text-[15px] leading-5 text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}
