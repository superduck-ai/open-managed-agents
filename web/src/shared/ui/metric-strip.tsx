import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/utils';

type MetricStripItem = {
  id: string;
  label: ReactNode;
  value: string;
  note?: string;
};

export function MetricStrip({
  items,
  columns,
  className,
}: {
  items: MetricStripItem[];
  columns: 3 | 4;
  className?: string;
}) {
  return (
    <dl
      className={cn(
        'grid rounded-lg border-[0.5px] border-border',
        columns === 3 ? 'grid-cols-3 divide-x-[0.5px] divide-border' : 'grid-cols-2 sm:grid-cols-4',
        className,
      )}
    >
      {items.map(({ id, label, value, note }) => (
        <div
          key={id}
          className={cn(
            'min-w-0 px-3 py-4 sm:px-5',
            columns === 4 &&
              'border-border max-sm:nth-[-n+2]:border-b-[0.5px] max-sm:odd:border-r-[0.5px] sm:not-last:border-r-[0.5px]',
          )}
        >
          <dt className="flex min-h-6 flex-wrap items-center gap-1.5 text-sm font-medium">{label}</dt>
          <dd className="mt-4 text-2xl font-medium tabular-nums leading-8">{value}</dd>
          {note && <dd className="mt-1 text-xs leading-5 text-muted-foreground">{note}</dd>}
        </div>
      ))}
    </dl>
  );
}
