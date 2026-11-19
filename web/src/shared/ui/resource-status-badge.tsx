import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/utils';

export type ResourceStatusTone = 'success' | 'warning' | 'danger' | 'neutral' | 'info';

const toneClassNames: Record<ResourceStatusTone, string> = {
  success: 'status-success',
  warning: 'status-warning',
  danger: 'status-danger',
  neutral: 'border-[0.5px] border-border bg-secondary text-secondary-foreground',
  info: 'border-[0.5px] border-border bg-secondary text-foreground',
};

export function ResourceStatusBadge({
  tone = 'neutral',
  className,
  children,
}: {
  tone?: ResourceStatusTone;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      data-slot="badge"
      className={cn(
        'inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-md px-2 text-xs font-medium leading-none',
        toneClassNames[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
